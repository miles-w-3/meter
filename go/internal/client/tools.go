package client

import (
	"context"

	"pi-workspace.local/workspace/internal/protocol"
)

func (c *Client) Tools(ctx context.Context) (protocol.ToolsStatus, error) {
	res, err := c.api.GetToolsWithResponse(ctx)
	if err != nil {
		return protocol.ToolsStatus{}, err
	}
	if body := res.GetJSON200(); body != nil {
		return *body, nil
	}
	return protocol.ToolsStatus{}, responseError(res.StatusCode(), res.GetBody())
}

func (c *Client) UpdateTools(ctx context.Context, change protocol.ToolSettingsChange) (protocol.ToolsStatus, error) {
	res, err := c.api.UpdateToolsWithResponse(ctx, change)
	if err != nil {
		return protocol.ToolsStatus{}, err
	}
	if body := res.GetJSON200(); body != nil {
		return *body, nil
	}
	return protocol.ToolsStatus{}, responseError(res.StatusCode(), res.GetBody())
}
