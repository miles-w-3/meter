package client

import (
	"context"

	"pi-workspace.local/workspace/internal/protocol"
)

func (c *Client) Catalog(ctx context.Context) (protocol.Catalog, error) {
	res, err := c.api.GetCatalogWithResponse(ctx)
	if err != nil {
		return protocol.Catalog{}, err
	}
	if body := res.GetJSON200(); body != nil {
		return *body, nil
	}
	return protocol.Catalog{}, responseError(res.StatusCode(), res.GetBody())
}

func (c *Client) RegisterProject(ctx context.Context, cwd, name string) (protocol.Project, error) {
	res, err := c.api.RegisterProjectWithResponse(ctx, protocol.ProjectInput{Cwd: cwd, Name: name})
	if err != nil {
		return protocol.Project{}, err
	}
	if body := res.GetJSON201(); body != nil {
		return *body, nil
	}
	return protocol.Project{}, responseError(res.StatusCode(), res.GetBody())
}

func (c *Client) RenameProject(ctx context.Context, id, name string) (protocol.Project, error) {
	res, err := c.api.RenameProjectWithResponse(ctx, id, protocol.ProjectRenameInput{Name: name})
	if err != nil {
		return protocol.Project{}, err
	}
	if body := res.GetJSON200(); body != nil {
		return *body, nil
	}
	return protocol.Project{}, responseError(res.StatusCode(), res.GetBody())
}

func (c *Client) Layout(ctx context.Context, id string) (protocol.Layout, error) {
	res, err := c.api.GetLayoutWithResponse(ctx, id)
	if err != nil {
		return protocol.Layout{}, err
	}
	if body := res.GetJSON200(); body != nil {
		return *body, nil
	}
	return protocol.Layout{}, responseError(res.StatusCode(), res.GetBody())
}

func (c *Client) SaveLayout(ctx context.Context, id string, layout protocol.Layout) (protocol.Layout, error) {
	res, err := c.api.SaveLayoutWithResponse(ctx, id, layout)
	if err != nil {
		return protocol.Layout{}, err
	}
	if body := res.GetJSON200(); body != nil {
		return *body, nil
	}
	return protocol.Layout{}, responseError(res.StatusCode(), res.GetBody())
}

func (c *Client) SetConversationCwd(ctx context.Context, id, cwd string) (protocol.Conversation, error) {
	res, err := c.api.SetConversationCwdWithResponse(ctx, id, protocol.CwdInput{Cwd: cwd})
	if err != nil {
		return protocol.Conversation{}, err
	}
	if body := res.GetJSON200(); body != nil {
		return *body, nil
	}
	return protocol.Conversation{}, responseError(res.StatusCode(), res.GetBody())
}

func (c *Client) ResolveCwd(ctx context.Context, cwd string) (string, error) {
	res, err := c.api.ResolveCwdWithResponse(ctx, protocol.CwdInput{Cwd: cwd})
	if err != nil {
		return "", err
	}
	if body := res.GetJSON200(); body != nil {
		return body.Cwd, nil
	}
	return "", responseError(res.StatusCode(), res.GetBody())
}
