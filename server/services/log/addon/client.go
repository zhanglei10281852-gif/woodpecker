// Copyright 2025 Woodpecker Authors
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
	"encoding/json"
	"errors"
	"net/rpc"
	"os/exec"

	"github.com/hashicorp/go-plugin"
	"github.com/rs/zerolog/log"

	"go.woodpecker-ci.org/woodpecker/v3/server/model"
	service_log "go.woodpecker-ci.org/woodpecker/v3/server/services/log"
	shared_addon "go.woodpecker-ci.org/woodpecker/v3/shared/addon"
	"go.woodpecker-ci.org/woodpecker/v3/shared/logger"
)

// make sure RPC implements service_log.Service.
var _ service_log.Service = (*RPC)(nil)

// Load starts the log addon subprocess owned by a lifecycle Manager and
// returns a stable log.Service wrapper. If the subprocess exits later, calls
// fail with the original RPC error until a following request restarts it; all
// spawned subprocesses are reaped when the server shuts down.
func Load(file string) (service_log.Service, error) {
	frontend := &RPC{}
	// Register only after the initial start succeeded: a misconfigured addon
	// must fail startup without leaving an ownerless manager behind.
	manager := shared_addon.NewManager(
		"log addon: "+file,
		func() (*rpc.Client, *plugin.Client, error) {
			var conn *rpc.Client
			client := plugin.NewClient(&plugin.ClientConfig{
				HandshakeConfig: HandshakeConfig,
				Plugins: map[string]plugin.Plugin{
					pluginKey: &hostPlugin{bind: func(c *rpc.Client) { conn = c }},
			},
				Cmd:          exec.Command(file),
				StartTimeout: shared_addon.DefaultStartTimeout,
				Logger: &logger.AddonClientLogger{
					Logger: log.With().Str("addon", file).Logger(),
				},
			})

			rpcClient, err := client.Client()
			if err != nil {
				client.Kill()
				return nil, nil, err
			}
			if _, err = rpcClient.Dispense(pluginKey); err != nil {
				client.Kill()
				return nil, nil, err
			}
			if conn == nil {
				client.Kill()
				return nil, nil, errors.New("log addon dispensed without an rpc connection")
			}
			return conn, client, nil
		},
		shared_addon.WithoutGlobalRegistry(),
	)
	frontend.m = manager

	// eagerly fail server startup when the addon can not be started at all
	if _, err := manager.Current(); err != nil {
		return nil, err
	}
	manager.Register()

	return frontend, nil
}

// RPC is the stable, concurrency-safe log.Service frontend. The backing
// subprocess (and therefore its net/rpc client) may be replaced by the
// Manager after a crash; this wrapper itself stays valid.
type RPC struct {
	m *shared_addon.Manager
}

// callRPC invokes an addon method on the current subprocess. A call that
// fails because the subprocess exited returns the original error without any
// replay: the Manager only records the dead connection so the next call can
// restart the addon.
func callRPC[T any](m *shared_addon.Manager, serviceMethod string, args any) (T, error) {
	var zero T

	client, err := m.Current()
	if err != nil {
		return zero, err
	}

	var resp T
	if err = client.Call(serviceMethod, args, &resp); err != nil {
		m.ReportFailure(client, err)
		return zero, err
	}
	return resp, nil
}

func (g *RPC) LogFind(step *model.Step) ([]*model.LogEntry, error) {
	args, err := json.Marshal(step)
	if err != nil {
		return nil, err
	}
	jsonResp, err := callRPC[[]byte](g.m, "Plugin.LogFind", args)
	if err != nil {
		return nil, err
	}

	var resp []*model.LogEntry
	err = json.Unmarshal(jsonResp, &resp)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (g *RPC) LogAppend(step *model.Step, logEntries []*model.LogEntry) error {
	args, err := json.Marshal(&argumentsAppend{
		Step:       step,
		LogEntries: logEntries,
	})
	if err != nil {
		return err
	}
	_, err = callRPC[[]byte](g.m, "Plugin.LogAppend", args)
	return err
}

func (g *RPC) LogDelete(step *model.Step) error {
	args, err := json.Marshal(step)
	if err != nil {
		return err
	}
	_, err = callRPC[[]byte](g.m, "Plugin.LogDelete", args)
	return err
}

func (g *RPC) StepFinished(step *model.Step) {
	args, err := json.Marshal(step)
	if err != nil {
		log.Error().Err(err).Msg("could not marshal json for log addon")
		return
	}
	if _, err = callRPC[[]byte](g.m, "Plugin.StepFinished", args); err != nil {
		log.Error().Err(err).Msg("StepFinished via addon failed")
	}
}
