package gomuksruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go.mau.fi/gomuks/pkg/hicli"
	"go.mau.fi/gomuks/pkg/hicli/jsoncmd"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/id"
)

func submitJSONCommand(ctx context.Context, cli *hicli.HiClient, cmd jsoncmd.Name, params any, out any) error {
	var payload json.RawMessage
	if params == nil {
		payload = []byte(`{}`)
	} else {
		raw, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("failed to encode %s params: %w", cmd, err)
		}
		payload = raw
	}

	resp := cli.SubmitJSONCommand(ctx, &hicli.JSONCommand{
		Command: cmd,
		Data:    payload,
	})
	if resp == nil {
		return fmt.Errorf("gomuks returned empty response for %s", cmd)
	}
	if resp.Command == jsoncmd.RespError {
		var message string
		if err := json.Unmarshal(resp.Data, &message); err != nil || strings.TrimSpace(message) == "" {
			message = string(resp.Data)
		}
		message = strings.TrimSpace(message)
		if message == "" {
			message = "unknown error"
		}
		return fmt.Errorf("gomuks %s failed: %s", cmd, message)
	}
	if resp.Command != jsoncmd.RespSuccess {
		return fmt.Errorf("gomuks returned unexpected response type %s for %s", resp.Command, cmd)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(resp.Data, out); err != nil {
		return fmt.Errorf("failed to decode %s response: %w", cmd, err)
	}
	return nil
}

func (r *Runtime) requireClient() (*hicli.HiClient, error) {
	cli := r.Client()
	if cli == nil || cli.Client == nil {
		return nil, fmt.Errorf("gomuks runtime is not initialized")
	}
	return cli, nil
}

func (r *Runtime) ClientState() (*jsoncmd.ClientState, error) {
	cli, err := r.requireClient()
	if err != nil {
		return nil, err
	}
	return cli.State(), nil
}

func (r *Runtime) DiscoverHomeserver(ctx context.Context, userID id.UserID) (*mautrix.ClientWellKnown, error) {
	cli, err := r.requireClient()
	if err != nil {
		return nil, err
	}
	var discovery mautrix.ClientWellKnown
	if err = submitJSONCommand(ctx, cli, jsoncmd.ReqDiscoverHomeserver, &jsoncmd.DiscoverHomeserverParams{
		UserID: userID,
	}, &discovery); err != nil {
		return nil, err
	}
	return &discovery, nil
}

func (r *Runtime) GetLoginFlows(ctx context.Context, homeserverURL string) (*mautrix.RespLoginFlows, error) {
	cli, err := r.requireClient()
	if err != nil {
		return nil, err
	}
	var flows mautrix.RespLoginFlows
	if err = submitJSONCommand(ctx, cli, jsoncmd.ReqGetLoginFlows, &jsoncmd.GetLoginFlowsParams{
		HomeserverURL: homeserverURL,
	}, &flows); err != nil {
		return nil, err
	}
	return &flows, nil
}

func (r *Runtime) Login(ctx context.Context, params *jsoncmd.LoginParams) error {
	cli, err := r.requireClient()
	if err != nil {
		return err
	}
	return submitJSONCommand(ctx, cli, jsoncmd.ReqLogin, params, nil)
}

func (r *Runtime) LoginCustom(ctx context.Context, params *jsoncmd.LoginCustomParams) error {
	cli, err := r.requireClient()
	if err != nil {
		return err
	}
	return submitJSONCommand(ctx, cli, jsoncmd.ReqLoginCustom, params, nil)
}

func (r *Runtime) Verify(ctx context.Context, params *jsoncmd.VerifyParams) error {
	cli, err := r.requireClient()
	if err != nil {
		return err
	}
	return submitJSONCommand(ctx, cli, jsoncmd.ReqVerify, params, nil)
}
