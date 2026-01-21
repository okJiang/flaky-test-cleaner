package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	token   string
	timeout time.Duration
	baseURL string
	http    *http.Client
}

func NewClient(token string, timeout time.Duration) *Client {
	return &Client{
		token:   token,
		timeout: timeout,
		baseURL: "https://api.github.com",
		http: &http.Client{
			Timeout: timeout,
		},
	}
}

type Workflow struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type WorkflowRun struct {
	ID      int64  `json:"id"`
	HTMLURL string `json:"html_url"`
	HeadSHA string `json:"head_sha"`
	CreatedAt time.Time `json:"created_at"`
}

type Job struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	Conclusion string   `json:"conclusion"`
	RunnerName string   `json:"runner_name"`
	RunnerOS   string   `json:"-"`
	Labels     []string `json:"labels"`
}

type ListWorkflowRunsOptions struct {
	Status  string
	PerPage int
}

type ListRunJobsOptions struct {
	PerPage int
}

func (c *Client) FindWorkflowByName(ctx context.Context, owner, repo, name string) (Workflow, error) {
	var res struct {
		Workflows []Workflow `json:"workflows"`
	}
	if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/%s/actions/workflows", owner, repo), nil, nil, &res); err != nil {
		return Workflow{}, err
	}
	for _, wf := range res.Workflows {
		if strings.EqualFold(wf.Name, name) {
			return wf, nil
		}
	}
	return Workflow{}, ErrNotFound
}

func (c *Client) ListWorkflowRuns(ctx context.Context, owner, repo string, workflowID int64, opts ListWorkflowRunsOptions) ([]WorkflowRun, error) {
	query := url.Values{}
	if opts.Status != "" {
		query.Set("status", opts.Status)
	}
	if opts.PerPage > 0 {
		query.Set("per_page", strconv.Itoa(opts.PerPage))
	}
	var res struct {
		Runs []WorkflowRun `json:"workflow_runs"`
	}
	path := fmt.Sprintf("/repos/%s/%s/actions/workflows/%d/runs", owner, repo, workflowID)
	if err := c.doJSON(ctx, http.MethodGet, path, query, nil, &res); err != nil {
		return nil, err
	}
	return res.Runs, nil
}

func (c *Client) ListRunJobs(ctx context.Context, owner, repo string, runID int64, opts ListRunJobsOptions) ([]Job, error) {
	query := url.Values{}
	if opts.PerPage > 0 {
		query.Set("per_page", strconv.Itoa(opts.PerPage))
	}
	var res struct {
		Jobs []Job `json:"jobs"`
	}
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/jobs", owner, repo, runID)
	if err := c.doJSON(ctx, http.MethodGet, path, query, nil, &res); err != nil {
		return nil, err
	}
	for i := range res.Jobs {
		if res.Jobs[i].RunnerName != "" {
			res.Jobs[i].RunnerOS = res.Jobs[i].RunnerName
			continue
		}
		res.Jobs[i].RunnerOS = pickRunnerLabel(res.Jobs[i].Labels)
	}
	return res.Jobs, nil
}

func (c *Client) DownloadJobLogs(ctx context.Context, owner, repo string, jobID int64) ([]byte, error) {
	path := fmt.Sprintf("/repos/%s/%s/actions/jobs/%d/logs", owner, repo, jobID)
	return c.doBytes(ctx, http.MethodGet, path, nil, nil)
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
	var res Issue
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", owner, repo, number)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, nil, &res); err != nil {
		return Issue{}, err
	}
	return res, nil
}

func (c *Client) CreateIssue(ctx context.Context, owner, repo string, in CreateIssueInput) (Issue, error) {
	payload := map[string]any{
		"title":  in.Title,
		"body":   in.Body,
		"labels": in.Labels,
	}
	var res Issue
	path := fmt.Sprintf("/repos/%s/%s/issues", owner, repo)
	if err := c.doJSON(ctx, http.MethodPost, path, nil, payload, &res); err != nil {
		return Issue{}, err
	}
	return res, nil
}

func (c *Client) UpdateIssue(ctx context.Context, owner, repo string, number int, in UpdateIssueInput) (Issue, error) {
	payload := map[string]any{}
	if in.Title != nil {
		payload["title"] = *in.Title
	}
	if in.Body != nil {
		payload["body"] = *in.Body
	}
	if in.Labels != nil {
		payload["labels"] = in.Labels
	}
	var res Issue
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", owner, repo, number)
	if err := c.doJSON(ctx, http.MethodPatch, path, nil, payload, &res); err != nil {
		return Issue{}, err
	}
	return res, nil
}

func (c *Client) EnsureLabels(ctx context.Context, owner, repo string, labels []string) error {
	for _, label := range labels {
		if strings.TrimSpace(label) == "" {
			continue
		}
		if err := c.createLabel(ctx, owner, repo, label); err != nil {
			var apiErr *apiError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnprocessableEntity {
				continue
			}
			return err
		}
	}
	return nil
}

var ErrNotFound = errors.New("not found")

type apiError struct {
	StatusCode int
	Message    string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("github api error: %d %s", e.StatusCode, e.Message)
}

func (c *Client) doJSON(ctx context.Context, method, path string, query url.Values, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}

	respBody, status, err := c.do(ctx, method, path, query, body, "application/vnd.github+json")
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		return ErrNotFound
	}
	if status < 200 || status >= 300 {
		return &apiError{StatusCode: status, Message: string(respBody)}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(respBody, out)
}

func (c *Client) doBytes(ctx context.Context, method, path string, query url.Values, payload io.Reader) ([]byte, error) {
	respBody, status, err := c.do(ctx, method, path, query, payload, "application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if status < 200 || status >= 300 {
		return nil, &apiError{StatusCode: status, Message: string(respBody)}
	}
	return respBody, nil
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body io.Reader, accept string) ([]byte, int, error) {
	urlStr := c.baseURL + path
	if query != nil && len(query) > 0 {
		urlStr = urlStr + "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, urlStr, body)
	if err != nil {
		return nil, 0, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", accept)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	for attempt := 0; attempt < 2; attempt++ {
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, 0, err
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusGatewayTimeout {
			if attempt == 0 {
				if wait := retryAfter(resp); wait > 0 {
					time.Sleep(wait)
				}
				continue
			}
		}
		return b, resp.StatusCode, nil
	}
	return nil, 0, errors.New("github request failed after retries")
}

func retryAfter(resp *http.Response) time.Duration {
	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil {
			return time.Duration(secs) * time.Second
		}
	}
	return 2 * time.Second
}

func pickRunnerLabel(labels []string) string {
	for _, label := range labels {
		lower := strings.ToLower(label)
		if strings.Contains(lower, "ubuntu") || strings.Contains(lower, "macos") || strings.Contains(lower, "windows") {
			return label
		}
	}
	if len(labels) > 0 {
		return labels[0]
	}
	return ""
}

func (c *Client) createLabel(ctx context.Context, owner, repo, name string) error {
	payload := map[string]any{
		"name":        name,
		"color":       "ededed",
		"description": "managed by flaky-test-cleaner",
	}
	path := fmt.Sprintf("/repos/%s/%s/labels", owner, repo)
	return c.doJSON(ctx, http.MethodPost, path, nil, payload, nil)
}
