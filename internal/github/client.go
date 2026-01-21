package github

import (
	"context"
	"errors"
	"time"
)

type Client struct {
	token   string
	timeout time.Duration
}

func NewClient(token string, timeout time.Duration) *Client {
	return &Client{token: token, timeout: timeout}
}

type Workflow struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type WorkflowRun struct {
	ID      int64  `json:"id"`
	HTMLURL string `json:"html_url"`
	HeadSHA string `json:"head_sha"`
}

type Job struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Conclusion string `json:"conclusion"`
	RunnerOS   string `json:"runner_name"`
}

type ListWorkflowRunsOptions struct {
	Status  string
	PerPage int
}

type ListRunJobsOptions struct {
	PerPage int
}

func (c *Client) FindWorkflowByName(ctx context.Context, owner, repo, name string) (Workflow, error) {
	return Workflow{}, errors.New("github client not implemented")
}

func (c *Client) ListWorkflowRuns(ctx context.Context, owner, repo string, workflowID int64, opts ListWorkflowRunsOptions) ([]WorkflowRun, error) {
	return nil, errors.New("github client not implemented")
}

func (c *Client) ListRunJobs(ctx context.Context, owner, repo string, runID int64, opts ListRunJobsOptions) ([]Job, error) {
	return nil, errors.New("github client not implemented")
}

func (c *Client) DownloadJobLogs(ctx context.Context, owner, repo string, jobID int64) ([]byte, error) {
	return nil, errors.New("github client not implemented")
}

type Issue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

type CreateIssueInput struct {
	Title  string
	Body   string
	Labels []string
}

type UpdateIssueInput struct {
	Title  *string
	Body   *string
	Labels []string
}

func (c *Client) GetIssue(ctx context.Context, owner, repo string, number int) (Issue, error) {
	return Issue{}, errors.New("github client not implemented")
}

func (c *Client) CreateIssue(ctx context.Context, owner, repo string, in CreateIssueInput) (Issue, error) {
	return Issue{}, errors.New("github client not implemented")
}

func (c *Client) UpdateIssue(ctx context.Context, owner, repo string, number int, in UpdateIssueInput) (Issue, error) {
	return Issue{}, errors.New("github client not implemented")
}

func (c *Client) EnsureLabels(ctx context.Context, owner, repo string, labels []string) error {
	return nil
}

var ErrNotFound = errors.New("not found")
