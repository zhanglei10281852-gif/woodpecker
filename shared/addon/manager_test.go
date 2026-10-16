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

package addon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/rpc"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testHelperEnv = "WP_ADDON_MANAGER_TEST_HELPER"
	testPluginKey = "test"
	testCrashExit = 17
	shutdownWait  = 20 * time.Second
	helperStartTO = 15 * time.Second
)

var testHandshake = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "WP_ADDON_MANAGER_TEST",
	MagicCookieValue: "test-cookie",
}

// TestHelperProcess is not a real test: it is the re-executed addon
// subprocess driven by go-plugin.
func TestHelperProcess(t *testing.T) {
	if os.Getenv(testHelperEnv) == "" {
		return
	}
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: testHandshake,
		Plugins: map[string]plugin.Plugin{
			testPluginKey: &helperPlugin{},
		},
	})
}

// helperPlugin is the plugin served inside the helper subprocess.
type helperPlugin struct{}

func (*helperPlugin) Server(*plugin.MuxBroker) (any, error) {
	return &helperServer{}, nil
}

func (*helperPlugin) Client(_ *plugin.MuxBroker, _ *rpc.Client) (any, error) {
	return nil, nil
}

type helperServer struct {
	pings atomic.Int32
}

func (s *helperServer) Ping(_ []byte, resp *[]byte) error {
	n := s.pings.Add(1)
	*resp = []byte(fmt.Sprintf("pong:%d", n))
	return nil
}

// Crash terminates the subprocess without answering the RPC call.
func (s *helperServer) Crash(_ []byte, _ *[]byte) error {
	os.Exit(testCrashExit)
	return nil
}

// hostTestPlugin hands the per-subprocess net/rpc client to the test starter.
type hostTestPlugin struct {
	bind func(c *rpc.Client)
}

func (*hostTestPlugin) Server(*plugin.MuxBroker) (any, error) {
	return nil, errors.New("hostTestPlugin must not be served")
}

func (p *hostTestPlugin) Client(_ *plugin.MuxBroker, c *rpc.Client) (any, error) {
	p.bind(c)
	return struct{}{}, nil
}

func helperStarter() (*rpc.Client, *plugin.Client, error) {
	var conn *rpc.Client
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: testHandshake,
		Plugins: map[string]plugin.Plugin{
			testPluginKey: &hostTestPlugin{bind: func(c *rpc.Client) { conn = c }},
		},
		Cmd: func() *exec.Cmd {
			cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
			cmd.Env = append(os.Environ(), testHelperEnv+"=1")
			return cmd
		}(),
		StartTimeout: helperStartTO,
		Logger: hclog.New(&hclog.LoggerOptions{
			Name:   "wp-test-addon",
			Level:  hclog.Off,
			Output: io.Discard,
		}),
	})

	rpcProtocol, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, nil, err
	}
	if _, err = rpcProtocol.Dispense(testPluginKey); err != nil {
		client.Kill()
		return nil, nil, err
	}
	return conn, client, nil
}

func newManagerForTest(t *testing.T, timeout time.Duration, starter Starter) *Manager {
	t.Helper()
	m := NewManager("test-addon", starter)
	m.startTimeout = timeout
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownWait)
		defer cancel()
		_ = m.Shutdown(ctx)
	})
	return m
}

func ping(t *testing.T, cli *rpc.Client) string {
	t.Helper()
	var resp []byte
	require.NoError(t, cli.Call("Plugin.Ping", []byte{}, &resp))
	return string(resp)
}

// crashAddon crashes the subprocess behind cli and reports the resulting error
// to the manager until it invalidated the dead connection (the net/rpc client
// only surfaces ErrShutdown once its read loop noticed the exit).
func crashAddon(t *testing.T, m *Manager, cli *rpc.Client) {
	t.Helper()
	var resp []byte
	err := cli.Call("Plugin.Crash", []byte{}, &resp)
	require.Error(t, err, "call against a crashing addon must return the original error")

	require.Eventually(t, func() bool {
		m.ReportFailure(cli, err)
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.current == nil
	}, 3*time.Second, 10*time.Millisecond, "manager should invalidate the crashed connection")
}

func TestManagerStartsLazilyAndServesCalls(t *testing.T) {
	starts := &atomic.Int32{}
	m := newManagerForTest(t, helperStartTO, func() (*rpc.Client, *plugin.Client, error) {
		starts.Add(1)
		return helperStarter()
	})

	cli, err := m.Current()
	require.NoError(t, err)
	require.Equal(t, "pong:1", ping(t, cli))

	// a second caller reuses the same subprocess without starting anything
	again, err := m.Current()
	require.NoError(t, err)
	assert.Same(t, cli, again)
	assert.Equal(t, int32(1), starts.Load())
}

func TestManagerFailedCallReturnsErrorAndNextCallRestarts(t *testing.T) {
	m := newManagerForTest(t, helperStartTO, helperStarter)

	cli, err := m.Current()
	require.NoError(t, err)
	require.Equal(t, "pong:1", ping(t, cli))

	crashAddon(t, m, cli)

	// the addon is restarted for the following request and serves again
	newCli, err := m.Current()
	require.NoError(t, err)
	assert.NotSame(t, cli, newCli)
	// fresh process: its ping counter starts at one, proving a new subprocess
	assert.Equal(t, "pong:1", ping(t, newCli))
}

func TestManagerConcurrentRequestsTriggerSingleRebuild(t *testing.T) {
	starts := &atomic.Int32{}
	m := newManagerForTest(t, helperStartTO, func() (*rpc.Client, *plugin.Client, error) {
		starts.Add(1)
		return helperStarter()
	})

	cli, err := m.Current()
	require.NoError(t, err)
	crashAddon(t, m, cli)

	const concurrency = 30
	var wg sync.WaitGroup
	clients := make(chan *rpc.Client, concurrency)
	errs := make(chan error, concurrency)
	start := make(chan struct{})
	wg.Add(concurrency)
	for range concurrency {
		go func() {
			defer wg.Done()
			<-start
			c, err := m.Current()
			if err != nil {
				errs <- err
				return
			}
			clients <- c
		}()
	}
	close(start)
	wg.Wait()
	close(clients)
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	restarted := <-clients
	expectedStarts := int32(2) // initial + one shared rebuild
	for c := range clients {
		assert.Same(t, restarted, c)
	}
	require.Equal(t, expectedStarts, starts.Load(), "concurrent requests must share a single rebuild")
	assert.Equal(t, "pong:1", ping(t, restarted))
}

func TestManagerStartFailureIsPromptAndRetriable(t *testing.T) {
	attempt := &atomic.Int32{}
	starter := func() (*rpc.Client, *plugin.Client, error) {
		if attempt.Add(1) < 3 {
			return nil, nil, errors.New("addon binary refused to start")
		}
		return helperStarter()
	}
	m := newManagerForTest(t, helperStartTO, starter)

	for range 2 {
		start := time.Now()
		_, err := m.Current()
		require.Error(t, err)
		assert.Less(t, time.Since(start), time.Second, "failed starts must return promptly")
	}

	// a later request is allowed to try again and succeeds
	cli, err := m.Current()
	require.NoError(t, err)
	assert.Equal(t, "pong:1", ping(t, cli))
}

func TestManagerRebuildIsBoundedByTimeout(t *testing.T) {
	release := make(chan struct{})
	starter := func() (*rpc.Client, *plugin.Client, error) {
		<-release
		return nil, nil, errors.New("released after timeout")
	}
	m := newManagerForTest(t, 200*time.Millisecond, starter)

	start := time.Now()
	_, err := m.Current()
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 2*time.Second, "waiters must be released at the rebuild timeout")

	// later requests may start a fresh attempt: let the lingering start
	// return and verify shutdown still completes without leaking it
	close(release)
}

func TestManagerShutdownReapsAllGenerationsAndBlocksNewStarts(t *testing.T) {
	owned := make(chan *plugin.Client, 8)
	starter := func() (*rpc.Client, *plugin.Client, error) {
		cli, pc, err := helperStarter()
		if err == nil {
			owned <- pc
		}
		return cli, pc, err
	}
	m := newManagerForTest(t, helperStartTO, starter)
	// skip automatic cleanup: this test asserts Shutdown behavior itself.

	first, err := m.Current()
	require.NoError(t, err)
	crashAddon(t, m, first)
	second, err := m.Current()
	require.NoError(t, err)
	require.NotSame(t, first, second)
	close(owned)

	var clients []*plugin.Client
	for pc := range owned {
		clients = append(clients, pc)
	}
	require.Len(t, clients, 2, "both addon generations must be tracked")

	ctx, cancel := context.WithTimeout(context.Background(), shutdownWait)
	defer cancel()
	require.NoError(t, m.Shutdown(ctx))

	for _, pc := range clients {
		assert.True(t, pc.Exited(), "addon subprocesses of every generation must be reaped")
	}

	_, err = m.Current()
	assert.ErrorIs(t, err, ErrShuttingDown)

	// shutdown is idempotent
	assert.NoError(t, m.Shutdown(ctx))
}

func TestShutdownAllReapsRegisteredManagers(t *testing.T) {
	m1 := NewManager("shutdown-all-a", helperStarter)
	m2 := NewManager("shutdown-all-b", helperStarter)
	_, err := m1.Current()
	require.NoError(t, err)
	_, err = m2.Current()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), shutdownWait)
	defer cancel()
	require.NoError(t, ShutdownAll(ctx))

	_, err = m1.Current()
	assert.ErrorIs(t, err, ErrShuttingDown)
	_, err = m2.Current()
	assert.ErrorIs(t, err, ErrShuttingDown)
}
