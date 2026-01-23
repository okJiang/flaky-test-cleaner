package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/okJiang/flaky-test-cleaner/internal/config"
	"github.com/okJiang/flaky-test-cleaner/internal/store"
)

func TestRunOnce_EndToEnd_WithStubGitHubAPI(t *testing.T) {
	ctx := context.Background()
	mem := store.NewMemory()

	owner := "testorg"
	repo := "testrepo"
	workflowID := int64(3933317)
	runID := int64(101)
	jobID := int64(202)
	issueNumber := 123

	var labelsCreated atomic.Int64
	var issuesCreated atomic.Int64
	var commentsCreated atomic.Int64
	unexpected := make(chan string, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON := func(status int, v any) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(v)
		}

		switch {
		case r.Method == http.MethodGet && r.URL.Path == fmt.Sprintf("/repos/%s/%s/actions/workflows", owner, repo):
			writeJSON(200, map[string]any{
				"workflows": []map[string]any{{"id": workflowID, "name": "PD Test"}},
			})
			return

		case r.Method == http.MethodGet && r.URL.Path == fmt.Sprintf("/repos/%s/%s/actions/workflows/%d/runs", owner, repo, workflowID):
			writeJSON(200, map[string]any{
				"workflow_runs": []map[string]any{{
					"id":         runID,
					"html_url":   "https://example.com/run/101",
					"head_sha":   "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
					"created_at": time.Now().UTC().Format(time.RFC3339),
				}},
			})
			return

		case r.Method == http.MethodGet && r.URL.Path == fmt.Sprintf("/repos/%s/%s/actions/runs/%d/jobs", owner, repo, runID):
			writeJSON(200, map[string]any{
				"jobs": []map[string]any{{
					"id":         jobID,
					"name":       "PD Test (unit)",
					"conclusion": "failure",
					"labels":     []string{"ubuntu-latest"},
				}},
			})
			return

		case r.Method == http.MethodGet && r.URL.Path == fmt.Sprintf("/repos/%s/%s/actions/jobs/%d/logs", owner, repo, jobID):
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(200)
			_, _ = w.Write([]byte(strings.Join([]string{
				"=== RUN   TestFoo",
				"--- FAIL: TestFoo (0.02s)",
				"    foo_test.go:12: expected true, got false",
				"FAIL",
				"exit status 1",
			}, "\n")))
			return

		case r.Method == http.MethodPost && r.URL.Path == fmt.Sprintf("/repos/%s/%s/labels", owner, repo):
			labelsCreated.Add(1)
			// Pretend label already exists.
			writeJSON(422, map[string]any{"message": "Validation Failed"})
			return

		case r.Method == http.MethodPost && r.URL.Path == fmt.Sprintf("/repos/%s/%s/issues", owner, repo):
			issuesCreated.Add(1)
			writeJSON(201, map[string]any{
				"number": issueNumber,
				"title":  "stub",
				"body":   "stub",
				"labels": []map[string]any{},
			})
			return

		case r.Method == http.MethodPost && r.URL.Path == fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, repo, issueNumber):
			commentsCreated.Add(1)
			writeJSON(201, map[string]any{"id": 1})
			return

		case r.Method == http.MethodGet && r.URL.Path == fmt.Sprintf("/repos/%s/%s/issues/%d", owner, repo, issueNumber):
			writeJSON(200, map[string]any{
				"number": issueNumber,
				"title":  "stub",
				"body":   "stub",
				"labels": []map[string]any{},
			})
			return

		case r.Method == http.MethodGet && r.URL.Path == fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, repo, issueNumber):
			writeJSON(200, []map[string]any{})
			return
		}

		select {
		case unexpected <- fmt.Sprintf("%s %s", r.Method, r.URL.Path):
		default:
		}
		w.WriteHeader(500)
		_, _ = w.Write([]byte("unexpected request"))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := config.Config{
		GitHubOwner:           owner,
		GitHubRepo:            repo,
		GitHubBaseBranch:      "main",
		GitHubAPIBaseURL:      srv.URL,
		GitHubReadToken:       "read-token",
		GitHubIssueToken:      "issue-token",
		WorkflowName:          "PD Test",
		MaxRuns:               1,
		MaxJobs:               1,
		DryRun:                false,
		ConfidenceThreshold:   0.75,
		TiDBEnabled:           false,
		WorkspaceMirrorDir:    t.TempDir(),
		WorkspaceWorktreesDir: t.TempDir(),
		WorkspaceMaxWorktrees: 1,
		RequestTimeout:        2 * time.Second,
		RunInterval:           0,
	}

	if err := RunOnceWithDeps(ctx, cfg, RunOnceDeps{Store: mem}); err != nil {
		t.Fatalf("RunOnceWithDeps: %v", err)
	}

	select {
	case got := <-unexpected:
		t.Fatalf("unexpected GitHub API request: %s", got)
	default:
	}

	if issuesCreated.Load() != 1 {
		t.Fatalf("expected 1 issue created, got %d", issuesCreated.Load())
	}
	if commentsCreated.Load() != 1 {
		t.Fatalf("expected 1 issue comment created, got %d", commentsCreated.Load())
	}
	if labelsCreated.Load() == 0 {
		t.Fatalf("expected labels to be ensured")
	}

	fps, err := mem.ListFingerprintsByState(ctx, store.StateWaitingForSignal, 10)
	if err != nil {
		t.Fatalf("ListFingerprintsByState: %v", err)
	}
	if len(fps) != 1 {
		t.Fatalf("expected 1 fingerprint in WAITING_FOR_SIGNAL, got %d", len(fps))
	}
	if fps[0].TestName != "TestFoo" {
		t.Fatalf("expected test name TestFoo, got %q", fps[0].TestName)
	}
	if fps[0].IssueNumber != issueNumber {
		t.Fatalf("expected issue #%d, got %d", issueNumber, fps[0].IssueNumber)
	}
}
