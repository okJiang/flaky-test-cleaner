package copilotsdk

import (
	"context"
	"fmt"
	"strings"
	"time"

	copilot "github.com/github/copilot-sdk/go"
)

type Options struct {
	Enabled  bool
	Model    string
	Timeout  time.Duration
	LogLevel string
}

type Client struct {
	opts    Options
	client  *copilot.Client
	started bool
}

func New(opts Options) *Client { return &Client{opts: opts} }

func (c *Client) Start() error {
	if !c.opts.Enabled {
		return nil
	}
	if c.started {
		return nil
	}
	c.client = copilot.NewClient(&copilot.ClientOptions{LogLevel: c.opts.LogLevel})
	if err := c.client.Start(); err != nil {
		return err
	}
	c.started = true
	return nil
}

func (c *Client) Stop() {
	if !c.started || c.client == nil {
		return
	}
	_ = c.client.Stop()
	c.started = false
}

func (c *Client) GenerateIssueAgentComment(ctx context.Context, systemMsg, prompt string) (string, error) {
	if !c.opts.Enabled {
		return "", fmt.Errorf("copilot sdk disabled")
	}
	if !c.started {
		return "", fmt.Errorf("copilot sdk not started")
	}

	timeout := c.opts.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > 0 && remaining < timeout {
			timeout = remaining
		}
	}

	s, err := c.client.CreateSession(&copilot.SessionConfig{
		Model:         c.opts.Model,
		SystemMessage: &copilot.SystemMessageConfig{Mode: "append", Content: systemMsg},
		OnPermissionRequest: func(req copilot.PermissionRequest, inv copilot.PermissionInvocation) (copilot.PermissionRequestResult, error) {
			return copilot.PermissionRequestResult{Kind: "denied-no-approval-rule-and-could-not-request-from-user"}, nil
		},
	})
	if err != nil {
		return "", err
	}
	defer s.Destroy()

	res, err := s.SendAndWait(copilot.MessageOptions{Prompt: prompt}, timeout)
	if err != nil {
		return "", err
	}
	if res == nil || res.Data.Content == nil {
		return "", fmt.Errorf("empty assistant response")
	}
	out := strings.TrimSpace(*res.Data.Content)
	if out == "" {
		return "", fmt.Errorf("empty assistant content")
	}
	return out, nil
}
