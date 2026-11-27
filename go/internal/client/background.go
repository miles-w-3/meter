package client

import (
	"context"
)

type BackgroundJob struct {
	ID             string
	ConversationID string
	Label          string
	Command        string
	Cwd            string
	Mode           string
	Status         string
	StartedAt      int64
	DeadlineAt     *int64
	EndedAt        *int64
	ExitCode       *int
	Signal         *string
	Events         int
	DroppedEvents  int
	Output         string
}

func (c *Client) Background(ctx context.Context, conversation string) ([]BackgroundJob, error) {
	res, err := c.api.GetBackgroundJobsWithResponse(ctx, conversation)
	if err != nil {
		return nil, err
	}
	body := res.GetJSON200()
	if body == nil {
		return nil, responseError(res.StatusCode(), res.GetBody())
	}
	jobs := make([]BackgroundJob, 0, len(body.Jobs))
	for _, j := range body.Jobs {
		var endedAt, deadlineAt *int64
		if j.EndedAt != nil {
			value := int64(*j.EndedAt)
			endedAt = &value
		}
		if j.DeadlineAt != nil {
			value := int64(*j.DeadlineAt)
			deadlineAt = &value
		}
		jobs = append(jobs, BackgroundJob{ID: j.Id.String(), ConversationID: j.ConversationId, Label: j.Label, Command: j.Command, Cwd: j.Cwd, Mode: string(j.Mode), Status: string(j.Status), StartedAt: int64(j.StartedAt), DeadlineAt: deadlineAt, EndedAt: endedAt, ExitCode: j.ExitCode, Signal: j.Signal, Events: j.Events, DroppedEvents: j.DroppedEvents, Output: j.Output})
	}
	return jobs, nil
}

func (c *Client) StopBackground(ctx context.Context, conversation, id string) error {
	res, err := c.api.StopBackgroundJobWithResponse(ctx, conversation, id)
	if err != nil {
		return err
	}
	if res.GetJSON200() == nil {
		return responseError(res.StatusCode(), res.GetBody())
	}
	return nil
}
