// Copyright 2024 Woodpecker Authors
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
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/rpc"
	"os/exec"

	"github.com/hashicorp/go-plugin"
	"github.com/rs/zerolog/log"

	"go.woodpecker-ci.org/woodpecker/v3/server/forge"
	"go.woodpecker-ci.org/woodpecker/v3/server/forge/types"
	"go.woodpecker-ci.org/woodpecker/v3/server/model"
	shared_addon "go.woodpecker-ci.org/woodpecker/v3/shared/addon"
	"go.woodpecker-ci.org/woodpecker/v3/shared/logger"
)

// make sure RPC implements forge.Forge.
var _ forge.Forge = (*RPC)(nil)

// Load starts the forge addon subprocess owned by a lifecycle Manager and
// returns a stable forge.Forge wrapper. If the subprocess exits later, calls
// fail with the original RPC error until a following request restarts it; all
// spawned subprocesses are reaped when the server shuts down.
func Load(file string) (forge.Forge, error) {
	frontend := &RPC{}
	// Register only after the initial start succeeded: a misconfigured addon
	// must fail startup without leaving an ownerless manager behind.
	manager := shared_addon.NewManager(
		"forge addon: "+file,
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
				return nil, nil, errors.New("forge addon dispensed without an rpc connection")
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

// RPC is the stable, concurrency-safe forge.Forge frontend. The backing
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

func (g *RPC) Name() string {
	resp, err := callRPC[string](g.m, "Plugin.Name", []byte{})
	if err != nil {
		log.Error().Err(err).Msg("addon Plugin.Name call failed")
	}
	return resp
}

func (g *RPC) URL() string {
	resp, err := callRPC[string](g.m, "Plugin.URL", []byte{})
	if err != nil {
		log.Error().Err(err).Msg("addon Plugin.URL call failed")
	}
	return resp
}

func (g *RPC) Login(_ context.Context, r *types.OAuthRequest) (*model.User, string, error) {
	args, err := json.Marshal(r)
	if err != nil {
		return nil, "", err
	}
	jsonResp, err := callRPC[[]byte](g.m, "Plugin.Login", args)
	if err != nil {
		return nil, "", err
	}

	var resp responseLogin
	err = json.Unmarshal(jsonResp, &resp)
	if err != nil {
		return nil, "", err
	}

	return resp.User.asModel(), resp.RedirectURL, nil
}

func (g *RPC) Teams(_ context.Context, u *model.User, p *model.ListOptions) ([]*model.Team, error) {
	args, err := json.Marshal(&argumentsTeams{
		U: modelUserFromModel(u),
		P: p,
	})
	if err != nil {
		return nil, err
	}
	jsonResp, err := callRPC[[]byte](g.m, "Plugin.Teams", args)
	if err != nil {
		return nil, err
	}

	var resp []*model.Team
	return resp, json.Unmarshal(jsonResp, &resp)
}

func (g *RPC) Repo(_ context.Context, u *model.User, remoteID model.ForgeRemoteID, owner, name string) (*model.Repo, error) {
	args, err := json.Marshal(&argumentsRepo{
		U:        modelUserFromModel(u),
		RemoteID: remoteID,
		Owner:    owner,
		Name:     name,
	})
	if err != nil {
		return nil, err
	}
	jsonResp, err := callRPC[[]byte](g.m, "Plugin.Repo", args)
	if err != nil {
		return nil, err
	}

	var resp modelRepo
	err = json.Unmarshal(jsonResp, &resp)
	if err != nil {
		return nil, err
	}
	return resp.asModel(), nil
}

func (g *RPC) Repos(_ context.Context, u *model.User, p *model.ListOptions) ([]*model.Repo, error) {
	args, err := json.Marshal(&argumentsRepos{
		U: modelUserFromModel(u),
		P: p,
	})
	if err != nil {
		return nil, err
	}
	jsonResp, err := callRPC[[]byte](g.m, "Plugin.Repos", args)
	if err != nil {
		return nil, err
	}

	var resp []*modelRepo
	err = json.Unmarshal(jsonResp, &resp)
	if err != nil {
		return nil, err
	}
	var modelRepos []*model.Repo
	for _, repo := range resp {
		modelRepos = append(modelRepos, repo.asModel())
	}
	return modelRepos, nil
}

func (g *RPC) File(_ context.Context, u *model.User, r *model.Repo, b *model.Pipeline, f string) ([]byte, error) {
	args, err := json.Marshal(&argumentsFileDir{
		U: modelUserFromModel(u),
		R: modelRepoFromModel(r),
		B: b,
		F: f,
	})
	if err != nil {
		return nil, err
	}
	return callRPC[[]byte](g.m, "Plugin.File", args)
}

func (g *RPC) Dir(_ context.Context, u *model.User, r *model.Repo, b *model.Pipeline, f string) ([]*types.FileMeta, error) {
	args, err := json.Marshal(&argumentsFileDir{
		U: modelUserFromModel(u),
		R: modelRepoFromModel(r),
		B: b,
		F: f,
	})
	if err != nil {
		return nil, err
	}
	jsonResp, err := callRPC[[]byte](g.m, "Plugin.Dir", args)
	if err != nil {
		return nil, err
	}
	var resp []*types.FileMeta
	return resp, json.Unmarshal(jsonResp, &resp)
}

func (g *RPC) Status(_ context.Context, u *model.User, r *model.Repo, b *model.Pipeline, p *model.Workflow) error {
	args, err := json.Marshal(&argumentsStatus{
		U: modelUserFromModel(u),
		R: modelRepoFromModel(r),
		B: b,
		P: p,
	})
	if err != nil {
		return err
	}
	_, err = callRPC[[]byte](g.m, "Plugin.Status", args)
	return err
}

func (g *RPC) Netrc(u *model.User, r *model.Repo) (*model.Netrc, error) {
	args, err := json.Marshal(&argumentsNetrc{
		U: modelUserFromModel(u),
		R: modelRepoFromModel(r),
	})
	if err != nil {
		return nil, err
	}
	jsonResp, err := callRPC[[]byte](g.m, "Plugin.Netrc", args)
	if err != nil {
		return nil, err
	}
	var resp *model.Netrc
	return resp, json.Unmarshal(jsonResp, &resp)
}

func (g *RPC) Activate(_ context.Context, u *model.User, r *model.Repo, link string) error {
	args, err := json.Marshal(&argumentsActivateDeactivate{
		U:    modelUserFromModel(u),
		R:    modelRepoFromModel(r),
		Link: link,
	})
	if err != nil {
		return err
	}
	_, err = callRPC[[]byte](g.m, "Plugin.Activate", args)
	return err
}

func (g *RPC) Deactivate(_ context.Context, u *model.User, r *model.Repo, link string) error {
	args, err := json.Marshal(&argumentsActivateDeactivate{
		U:    modelUserFromModel(u),
		R:    modelRepoFromModel(r),
		Link: link,
	})
	if err != nil {
		return err
	}
	_, err = callRPC[[]byte](g.m, "Plugin.Deactivate", args)
	return err
}

func (g *RPC) Branches(_ context.Context, u *model.User, r *model.Repo, p *model.ListOptions) ([]string, error) {
	args, err := json.Marshal(&argumentsBranchesPullRequests{
		U: modelUserFromModel(u),
		R: modelRepoFromModel(r),
		P: p,
	})
	if err != nil {
		return nil, err
	}
	jsonResp, err := callRPC[[]byte](g.m, "Plugin.Branches", args)
	if err != nil {
		return nil, err
	}
	var resp []string
	return resp, json.Unmarshal(jsonResp, &resp)
}

func (g *RPC) BranchHead(_ context.Context, u *model.User, r *model.Repo, branch string) (*model.Commit, error) {
	args, err := json.Marshal(&argumentsBranchHead{
		U:      modelUserFromModel(u),
		R:      modelRepoFromModel(r),
		Branch: branch,
	})
	if err != nil {
		return nil, err
	}
	jsonResp, err := callRPC[[]byte](g.m, "Plugin.BranchHead", args)
	if err != nil {
		return nil, err
	}
	var resp *model.Commit
	return resp, json.Unmarshal(jsonResp, &resp)
}

func (g *RPC) PullRequests(_ context.Context, u *model.User, r *model.Repo, p *model.ListOptions) ([]*model.PullRequest, error) {
	args, err := json.Marshal(&argumentsBranchesPullRequests{
		U: modelUserFromModel(u),
		R: modelRepoFromModel(r),
		P: p,
	})
	if err != nil {
		return nil, err
	}
	jsonResp, err := callRPC[[]byte](g.m, "Plugin.PullRequests", args)
	if err != nil {
		return nil, err
	}
	var resp []*model.PullRequest
	return resp, json.Unmarshal(jsonResp, &resp)
}

func (g *RPC) Hook(_ context.Context, r *http.Request) (*model.Repo, *model.Pipeline, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, nil, err
	}
	args, err := json.Marshal(&httpRequest{
		Method: r.Method,
		URL:    r.URL.String(),
		Header: r.Header,
		Form:   r.Form,
		Body:   body,
	})
	if err != nil {
		return nil, nil, err
	}
	jsonResp, err := callRPC[[]byte](g.m, "Plugin.Hook", args)
	if err != nil {
		return nil, nil, err
	}
	var resp responseHook
	err = json.Unmarshal(jsonResp, &resp)
	if err != nil {
		return nil, nil, err
	}
	return resp.Repo.asModel(), resp.Pipeline, nil
}

func (g *RPC) OrgMembership(_ context.Context, u *model.User, org string) (*model.OrgPerm, error) {
	args, err := json.Marshal(&argumentsOrgMembershipOrg{
		U:   modelUserFromModel(u),
		Org: org,
	})
	if err != nil {
		return nil, err
	}
	jsonResp, err := callRPC[[]byte](g.m, "Plugin.OrgMembership", args)
	if err != nil {
		return nil, err
	}
	var resp *model.OrgPerm
	return resp, json.Unmarshal(jsonResp, &resp)
}

func (g *RPC) Org(_ context.Context, u *model.User, org string) (*model.Org, error) {
	args, err := json.Marshal(&argumentsOrgMembershipOrg{
		U:   modelUserFromModel(u),
		Org: org,
	})
	if err != nil {
		return nil, err
	}
	jsonResp, err := callRPC[[]byte](g.m, "Plugin.Org", args)
	if err != nil {
		return nil, err
	}
	var resp *model.Org
	return resp, json.Unmarshal(jsonResp, &resp)
}
