// Copyright 2026 Woodpecker Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package addon owns the subprocess lifecycle of forge and log addons that
// are hosted through hashicorp/go-plugin.
//
// A Manager keeps exactly one healthy addon subprocess at a time:
//
//   - The connection is started lazily (optionally eagerly via [Manager.Current]).
//   - When an RPC call fails because its subprocess has exited, the call
//     returns that original error. It is never replayed: the write may already
//     have been executed inside the addon. [Manager.ReportFailure] only marks
//     the dead connection for replacement.
//   - Concurrent requests that find no healthy connection join a single
//     bounded rebuild instead of each spawning their own subprocess.
//   - A rebuild that fails returns promptly (bounded by [DefaultStartTimeout])
//     and later requests are free to start a new attempt.
//   - After [Manager.Shutdown] (or [ShutdownAll]) no new subprocess is started,
//     an in-flight rebuild is awaited and then terminated, and every subprocess
//     owned by the manager (including every generation replaced at runtime) is
//     reaped.
package addon

import (
	"context"
	"errors"
	"fmt"
	"net/rpc"
	"sync"
	"time"

	"github.com/hashicorp/go-plugin"
	"github.com/rs/zerolog/log"
)

// DefaultStartTimeout bounds a single addon (re)start.
const DefaultStartTimeout = 30 * time.Second

// ErrShuttingDown is returned once the server has started to shut down and
// therefore refuses to start new addon subprocesses.
var ErrShuttingDown = errors.New("addon subprocesses are disabled as the server is shutting down")

// Starter spawns and connects a fresh addon subprocess. It returns the
// net/rpc client used for calls and the go-plugin client that owns the
// subprocess. A failed start must not leak the process: callers should invoke
// pluginClient.Kill() on failure; the Manager does so again defensively.
type Starter func() (rpcClient *rpc.Client, pluginClient *plugin.Client, err error)

type instance struct {
	rpc    *rpc.Client
	plugin *plugin.Client
}

type rebuildOp struct {
	done chan struct{}
	cli  *rpc.Client
	err  error
}

type spawnResult struct {
	cli    *rpc.Client
	plugin *plugin.Client
	err    error
}

// Manager owns the lifecycle of a single addon executable and is safe for
// concurrent use.
type Manager struct {
	name         string
	start        Starter
	startTimeout time.Duration

	mu           sync.Mutex
	current      *instance
	rebuilding   *rebuildOp
	live         map[*plugin.Client]struct{}
	shuttingDown bool

	// spawnWg covers complete rebuild goroutines (including a start that
	// already exceeded startTimeout but has not returned yet).
	spawnWg sync.WaitGroup
	// killWg tracks goroutines that terminate owned subprocesses.
	killWg sync.WaitGroup
}

// Option customizes a Manager at construction time.
type Option func(*options)

type options struct {
	register bool
}

// WithoutGlobalRegistry creates a Manager that is not reaped by
// [ShutdownAll]. Callers can add it later via [Manager.Register], e.g. after a
// successful initial start.
func WithoutGlobalRegistry() Option {
	return func(o *options) {
		o.register = false
	}
}

// NewManager creates a Manager for one addon executable. By default it is
// registered for process-wide shutdown via [ShutdownAll].
func NewManager(name string, start Starter, opts ...Option) *Manager {
	cfg := options{register: true}
	for _, opt := range opts {
		opt(&cfg)
	}

	m := &Manager{
		name:         name,
		start:        start,
		startTimeout: DefaultStartTimeout,
		live:         make(map[*plugin.Client]struct{}),
	}
	if cfg.register {
		register(m)
	}
	return m
}

// Register adds the manager to the registry used by [ShutdownAll]. It is
// idempotent.
func (m *Manager) Register() {
	register(m)
}

// Current returns the net/rpc client of the healthy addon subprocess. If no
// healthy subprocess exists it is (re)started: all concurrent callers join
// the same bounded rebuild attempt.
func (m *Manager) Current() (*rpc.Client, error) {
	m.mu.Lock()
	if m.shuttingDown {
		m.mu.Unlock()
		return nil, ErrShuttingDown
	}
	if m.current != nil {
		cli := m.current.rpc
		m.mu.Unlock()
		return cli, nil
	}
	op := m.rebuilding
	if op == nil {
		op = &rebuildOp{done: make(chan struct{})}
		m.rebuilding = op
		m.spawnWg.Add(1)
		go m.rebuild(op)
	}
	m.mu.Unlock()

	<-op.done
	return op.cli, op.err
}

// ReportFailure must be called with the client and error of a failed RPC call.
// The failing call itself is expected to return err to its caller: this method
// never replays it. When the failure is caused by the backing subprocess
// having exited (or by an already shut down rpc connection), the connection is
// discarded so that the next request triggers a restart. Errors returned by a
// healthy subprocess (e.g. business errors) leave the connection untouched.
func (m *Manager) ReportFailure(cli *rpc.Client, err error) {
	if err == nil || cli == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.shuttingDown || m.current == nil || m.current.rpc != cli {
		return
	}
	if !errors.Is(err, rpc.ErrShutdown) && !m.current.plugin.Exited() {
		return
	}

	log.Warn().
		Str("addon", m.name).
		Err(err).
		Msg("addon subprocess connection lost; it will be restarted on the next request")

	old := m.current
	m.current = nil
	m.killLocked(old.plugin)
}

func (m *Manager) rebuild(op *rebuildOp) {
	defer m.spawnWg.Done()

	resultCh := make(chan spawnResult, 1)
	go func() {
		cli, pluginClient, err := m.start()
		resultCh <- spawnResult{cli: cli, plugin: pluginClient, err: err}
	}()

	timer := time.NewTimer(m.startTimeout)
	defer timer.Stop()

	var result spawnResult
	timedOut := false
	select {
	case result = <-resultCh:
	case <-timer.C:
		// Release all waiting callers promptly. They must not retry on the
		// same attempt; the next request starts a fresh one.
		timedOut = true
		err := fmt.Errorf("addon %q did not become ready within %s: %w",
			m.name, m.startTimeout, context.DeadlineExceeded)
		log.Error().Str("addon", m.name).Err(err).Msg("addon restart timed out")
		m.publishError(op, err)
		result = <-resultCh
	}
	m.complete(op, result, timedOut)
}

func (m *Manager) publishError(op *rebuildOp, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rebuilding == op {
		m.rebuilding = nil
	}
	op.err = err
	close(op.done)
}

func (m *Manager) complete(op *rebuildOp, res spawnResult, timedOut bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.rebuilding == op {
		m.rebuilding = nil
	}

	// A start that only returned after the attempt was already reported as
	// timed out can never become current; reap whatever it spawned.
	if timedOut {
		if res.plugin != nil {
			m.live[res.plugin] = struct{}{}
			m.killLocked(res.plugin)
		}
		return
	}

	if res.err != nil {
		op.err = res.err
		if res.plugin != nil {
			// The Starter is expected to clean up failed starts itself;
			// never rely on that to avoid an orphaned process.
			m.live[res.plugin] = struct{}{}
			m.killLocked(res.plugin)
		}
		close(op.done)
		return
	}

	m.live[res.plugin] = struct{}{}

	if m.shuttingDown {
		m.killLocked(res.plugin)
		op.err = ErrShuttingDown
		close(op.done)
		return
	}

	old := m.current
	m.current = &instance{rpc: res.cli, plugin: res.plugin}
	op.cli = res.cli
	if old != nil {
		log.Debug().Str("addon", m.name).Msg("terminating addon subprocess replaced by a new instance")
		m.killLocked(old.plugin)
	}
	log.Info().Str("addon", m.name).Msg("addon subprocess started")
	close(op.done)
}

// killLocked terminates the subprocess in the background. The caller must hold
// m.mu (so the WaitGroup bookkeeping is synchronized with Shutdown).
func (m *Manager) killLocked(pc *plugin.Client) {
	if pc == nil {
		return
	}
	delete(m.live, pc)
	m.killWg.Go(pc.Kill)
}

// Shutdown prevents any further subprocess starts, waits for an in-flight
// rebuild to settle, terminates the switching instance and the current one and
// reaps every subprocess replaced during the whole lifetime of the manager.
// It blocks until all subprocesses have exited or ctx expires. Shutdown is
// idempotent.
func (m *Manager) Shutdown(ctx context.Context) error {
	unregister(m)

	m.mu.Lock()
	m.shuttingDown = true
	// Terminate everything currently owned right away, including the current
	// instance instead of waiting for an in-flight rebuild to replace it.
	for pc := range m.live {
		m.killLocked(pc)
	}
	m.current = nil
	m.mu.Unlock()

	done := make(chan struct{})
	go func() {
		// Wait for a rebuild that is still starting a subprocess: it observes
		// the shuttingDown flag and kills the switching instance itself.
		m.spawnWg.Wait()

		// Guard against a rebuild that scheduled its kill just after the
		// snapshot above was taken.
		m.mu.Lock()
		for pc := range m.live {
			m.killLocked(pc)
		}
		m.mu.Unlock()

		m.killWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Debug().Str("addon", m.name).Msg("all addon subprocesses reaped")
		return nil
	case <-ctx.Done():
		return fmt.Errorf("shutting down addon %q: %w", m.name, ctx.Err())
	}
}

// Registry tracks every live manager so the server can reap all addon
// subprocesses (including ones replaced while running) on shutdown.
var (
	registryMu sync.Mutex
	registry   = make(map[*Manager]struct{})
)

func register(m *Manager) {
	registryMu.Lock()
	registry[m] = struct{}{}
	registryMu.Unlock()
}

func unregister(m *Manager) {
	registryMu.Lock()
	delete(registry, m)
	registryMu.Unlock()
}

// ShutdownAll shuts down all registered managers concurrently. Each manager
// obeys ctx; the first context expiration (if any) is returned joined with
// any other errors after every manager has been asked to terminate its
// subprocesses.
func ShutdownAll(ctx context.Context) error {
	registryMu.Lock()
	managers := make([]*Manager, 0, len(registry))
	for m := range registry {
		managers = append(managers, m)
	}
	registryMu.Unlock()

	var (
		wg    sync.WaitGroup
		errMu sync.Mutex
		errs  []error
	)
	for _, m := range managers {
		wg.Add(1)
		go func(m *Manager) {
			defer wg.Done()
			if err := m.Shutdown(ctx); err != nil {
				errMu.Lock()
				errs = append(errs, err)
				errMu.Unlock()
			}
		}(m)
	}
	wg.Wait()
	return errors.Join(errs...)
}
