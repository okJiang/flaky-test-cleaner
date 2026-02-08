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

	"github.com/okJiang/flaky-test-cleaner/internal/domain"
)

type Client struct {
	token   string
	timeout time.Duration
	baseURL string
	http    *http.Client
}

func NewClient(token string, timeout time.Duration) *Client {
	return NewClientWithBaseURL(token, timeout, "https://api.github.com")
}

func NewClientWithBaseURL(token string, timeout time.Duration, baseURL string) *Client {
	return NewClientWithTransport(token, timeout, baseURL, nil)
}

func NewClientWithTransport(token string, timeout time.Duration, baseURL string, transport http.RoundTripper) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	httpClient := &http.Client{Timeout: timeout}
	if transport != nil {
		httpClient.Transport = transport
	}
	return &Client{
		token:   token,
		timeout: timeout,
		baseURL: baseURL,
		http:    httpClient,
	}
}

func (c *Client) FindWorkflowByName(ctx context.Context, owner, repo, name string) (domain.Workflow, error) {
	var res struct {
		Workflows []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"workflows"`
	}
	if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/%s/actions/workflows", owner, repo), nil, nil, &res); err != nil {
		return domain.Workflow{}, err
	}
	for _, wf := range res.Workflows {
		if strings.EqualFold(wf.Name, name) {
			return domain.Workflow{ID: wf.ID, Name: wf.Name}, nil
		}
	}
	return domain.Workflow{}, ErrNotFound
}

func (c *Client) ListWorkflowRuns(ctx context.Context, owner, repo string, workflowID int64, opts domain.ListWorkflowRunsOptions) ([]domain.WorkflowRun, error) {
	query := url.Values{}
	if opts.Status != "" {
		query.Set("status", opts.Status)
	}
	if opts.Branch != "" {
		query.Set("branch", opts.Branch)
	}
	if opts.Event != "" {
		query.Set("event", opts.Event)
	}
	if opts.PerPage > 0 {
		query.Set("per_page", strconv.Itoa(opts.PerPage))
	}
	var res struct {
		Runs []struct {
			ID         int64     `json:"id"`
			HTMLURL    string    `json:"html_url"`
			HeadSHA    string    `json:"head_sha"`
			HeadBranch string    `json:"head_branch"`
			Event      string    `json:"event"`
			CreatedAt  time.Time `json:"created_at"`
		} `json:"workflow_runs"`
	}
	path := fmt.Sprintf("/repos/%s/%s/actions/workflows/%d/runs", owner, repo, workflowID)
	if err := c.doJSON(ctx, http.MethodGet, path, query, nil, &res); err != nil {
		return nil, err
	}
	out := make([]domain.WorkflowRun, 0, len(res.Runs))
	for _, run := range res.Runs {
		out = append(out, domain.WorkflowRun{
			ID:         run.ID,
			HTMLURL:    run.HTMLURL,
			HeadSHA:    run.HeadSHA,
			HeadBranch: run.HeadBranch,
			Event:      run.Event,
			CreatedAt:  run.CreatedAt,
		})
	}
	return out, nil
}

func (c *Client) ListRunJobs(ctx context.Context, owner, repo string, runID int64, opts domain.ListRunJobsOptions) ([]domain.Job, error) {
	query := url.Values{}
	if opts.PerPage > 0 {
		query.Set("per_page", strconv.Itoa(opts.PerPage))
	}
	var res struct {
		Jobs []struct {
			ID         int64    `json:"id"`
			Name       string   `json:"name"`
			Conclusion string   `json:"conclusion"`
			RunnerName string   `json:"runner_name"`
			Labels     []string `json:"labels"`
		} `json:"jobs"`
	}
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/jobs", owner, repo, runID)
	if err := c.doJSON(ctx, http.MethodGet, path, query, nil, &res); err != nil {
		return nil, err
	}
	out := make([]domain.Job, 0, len(res.Jobs))
	for _, job := range res.Jobs {
		out = append(out, domain.Job{
			ID:         job.ID,
			Name:       job.Name,
			Conclusion: job.Conclusion,
			RunnerName: job.RunnerName,
			RunnerOS:   pickRunnerLabel(job.Labels),
			Labels:     job.Labels,
		})
	}
	return out, nil
}

func (c *Client) DownloadJobLogs(ctx context.Context, owner, repo string, jobID int64) ([]byte, error) {
	path := fmt.Sprintf("/repos/%s/%s/actions/jobs/%d/logs", owner, repo, jobID)
	return c.doBytes(ctx, http.MethodGet, path, nil, nil)
}

func (c *Client) GetIssue(ctx context.Context, owner, repo string, number int) (domain.Issue, error) {
	var res struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", owner, repo, number)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, nil, &res); err != nil {
		return domain.Issue{}, err
	}
	labels := make([]domain.IssueLabel, 0, len(res.Labels))
	for _, l := range res.Labels {
		labels = append(labels, domain.IssueLabel{Name: l.Name})
	}
	return domain.Issue{Number: res.Number, Title: res.Title, Body: res.Body, Labels: labels}, nil
}

func (c *Client) CreateIssue(ctx context.Context, owner, repo string, in domain.CreateIssueInput) (domain.Issue, error) {
	payload := map[string]any{"title": in.Title, "body": in.Body, "labels": in.Labels}
	var res struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	path := fmt.Sprintf("/repos/%s/%s/issues", owner, repo)
	if err := c.doJSON(ctx, http.MethodPost, path, nil, payload, &res); err != nil {
		return domain.Issue{}, err
	}
	labels := make([]domain.IssueLabel, 0, len(res.Labels))
	for _, l := range res.Labels {
		labels = append(labels, domain.IssueLabel{Name: l.Name})
	}
	return domain.Issue{Number: res.Number, Title: res.Title, Body: res.Body, Labels: labels}, nil
}

func (c *Client) UpdateIssue(ctx context.Context, owner, repo string, number int, in domain.UpdateIssueInput) (domain.Issue, error) {
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
	if in.State != nil {
		payload["state"] = *in.State
	}
	var res struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", owner, repo, number)
	if err := c.doJSON(ctx, http.MethodPatch, path, nil, payload, &res); err != nil {
		return domain.Issue{}, err
	}
	labels := make([]domain.IssueLabel, 0, len(res.Labels))
	for _, l := range res.Labels {
		labels = append(labels, domain.IssueLabel{Name: l.Name})
	}
	return domain.Issue{Number: res.Number, Title: res.Title, Body: res.Body, Labels: labels}, nil
}

func (c *Client) CreateIssueComment(ctx context.Context, owner, repo string, number int, body string) error {
	payload := map[string]any{"body": body}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, repo, number)
	return c.doJSON(ctx, http.MethodPost, path, nil, payload, nil)
}

func (c *Client) ListIssueComments(ctx context.Context, owner, repo string, number int, opts domain.ListIssueCommentsOptions) ([]domain.IssueComment, error) {
	query := url.Values{}
	if opts.PerPage > 0 {
		query.Set("per_page", strconv.Itoa(opts.PerPage))
	}
	var res []struct {
		ID        int64     `json:"id"`
		Body      string    `json:"body"`
		CreatedAt time.Time `json:"created_at"`
		User      struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, repo, number)
	if err := c.doJSON(ctx, http.MethodGet, path, query, nil, &res); err != nil {
		return nil, err
	}
	out := make([]domain.IssueComment, 0, len(res))
	for _, cmt := range res {
		out = append(out, domain.IssueComment{
			ID:        cmt.ID,
			Body:      cmt.Body,
			CreatedAt: cmt.CreatedAt,
			User:      domain.User{Login: cmt.User.Login},
		})
	}
	return out, nil
}

func (c *Client) ListPullRequestReviews(ctx context.Context, owner, repo string, number int) ([]domain.PullRequestReview, error) {
	var res []struct {
		ID          int64      `json:"id"`
		State       string     `json:"state"`
		Body        string     `json:"body"`
		SubmittedAt *time.Time `json:"submitted_at"`
		User        struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews", owner, repo, number)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, nil, &res); err != nil {
		return nil, err
	}
	out := make([]domain.PullRequestReview, 0, len(res))
	for _, rv := range res {
		out = append(out, domain.PullRequestReview{
			ID:          rv.ID,
			State:       rv.State,
			Body:        rv.Body,
			SubmittedAt: rv.SubmittedAt,
			User:        domain.User{Login: rv.User.Login},
		})
	}
	return out, nil
}

func (c *Client) GetCombinedStatus(ctx context.Context, owner, repo, ref string) (domain.CombinedStatus, error) {
	var res struct {
		State    string `json:"state"`
		Statuses []struct {
			State       string    `json:"state"`
			Context     string    `json:"context"`
			Description string    `json:"description"`
			TargetURL   string    `json:"target_url"`
			UpdatedAt   time.Time `json:"updated_at"`
		} `json:"statuses"`
	}
	path := fmt.Sprintf("/repos/%s/%s/commits/%s/status", owner, repo, ref)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, nil, &res); err != nil {
		return domain.CombinedStatus{}, err
	}
	out := domain.CombinedStatus{State: res.State, Statuses: make([]domain.Status, 0, len(res.Statuses))}
	for _, st := range res.Statuses {
		out.Statuses = append(out.Statuses, domain.Status{
			State:       st.State,
			Context:     st.Context,
			Description: st.Description,
			TargetURL:   st.TargetURL,
			UpdatedAt:   st.UpdatedAt,
		})
	}
	return out, nil
}

func (c *Client) CreatePullRequest(ctx context.Context, owner, repo string, in domain.CreatePullRequestInput) (domain.PullRequest, error) {
	payload := map[string]any{"title": in.Title, "head": in.Head, "base": in.Base, "body": in.Body}
	if in.Draft {
		payload["draft"] = true
	}
	var res struct {
		Number   int        `json:"number"`
		HTMLURL  string     `json:"html_url"`
		State    string     `json:"state"`
		Merged   bool       `json:"merged"`
		MergedAt *time.Time `json:"merged_at"`
		Head     struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"head"`
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls", owner, repo)
	if err := c.doJSON(ctx, http.MethodPost, path, nil, payload, &res); err != nil {
		return domain.PullRequest{}, err
	}
	return domain.PullRequest{
		Number:   res.Number,
		HTMLURL:  res.HTMLURL,
		State:    res.State,
		Merged:   res.Merged,
		MergedAt: res.MergedAt,
		Head:     domain.PRHead{Ref: res.Head.Ref, SHA: res.Head.SHA},
	}, nil
}

func (c *Client) AddIssueLabels(ctx context.Context, owner, repo string, number int, labels []string) error {
	if len(labels) == 0 {
		return nil
	}
	payload := map[string]any{"labels": labels}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/labels", owner, repo, number)
	return c.doJSON(ctx, http.MethodPost, path, nil, payload, nil)
}

func (c *Client) GetPullRequest(ctx context.Context, owner, repo string, number int) (domain.PullRequest, error) {
	var res struct {
		Number   int        `json:"number"`
		HTMLURL  string     `json:"html_url"`
		State    string     `json:"state"`
		Merged   bool       `json:"merged"`
		MergedAt *time.Time `json:"merged_at"`
		Head     struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"head"`
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, number)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, nil, &res); err != nil {
		return domain.PullRequest{}, err
	}
	return domain.PullRequest{
		Number:   res.Number,
		HTMLURL:  res.HTMLURL,
		State:    res.State,
		Merged:   res.Merged,
		MergedAt: res.MergedAt,
		Head:     domain.PRHead{Ref: res.Head.Ref, SHA: res.Head.SHA},
	}, nil
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
	var payloadBytes []byte
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		payloadBytes = b
	}

	respBody, status, err := c.do(ctx, method, path, query, payloadBytes, "application/vnd.github+json")
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
	var payloadBytes []byte
	if payload != nil {
		b, err := io.ReadAll(payload)
		if err != nil {
			return nil, err
		}
		payloadBytes = b
	}
	respBody, status, err := c.do(ctx, method, path, query, payloadBytes, "application/vnd.github+json")
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

func (c *Client) do(ctx context.Context, method, path string, query url.Values, payload []byte, accept string) ([]byte, int, error) {
	urlStr := c.baseURL + path
	if query != nil && len(query) > 0 {
		urlStr = urlStr + "?" + query.Encode()
	}

	for attempt := 0; attempt < 2; attempt++ {
		var body io.Reader
		if payload != nil {
			body = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, urlStr, body)
		if err != nil {
			return nil, 0, err
		}
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}
		req.Header.Set("Accept", accept)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}

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
