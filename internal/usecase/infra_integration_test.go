package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	githubadapter "github.com/okJiang/flaky-test-cleaner/internal/adapters/github"
	storeadapter "github.com/okJiang/flaky-test-cleaner/internal/adapters/store"
	"github.com/okJiang/flaky-test-cleaner/internal/config"
	"github.com/okJiang/flaky-test-cleaner/internal/domain"
	"github.com/okJiang/flaky-test-cleaner/internal/runtime"
)

func TestService_InfraFlake_DoesNotCreateIssue(t *testing.T) {
	ctx := context.Background()
	mem := storeadapter.NewMemory()

	owner := "testorg"
	repo := "testrepo"
	workflowID := int64(1)
	runID := int64(101)
	jobID := int64(202)

	var issuesCreated atomic.Int64
	var labelsCreated atomic.Int64

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON := func(status int, v any) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(v)
		}

		switch {
		case r.Method == http.MethodGet && r.URL.Path == fmt.Sprintf("/repos/%s/%s/actions/workflows", owner, repo):
			writeJSON(200, map[string]any{"workflows": []map[string]any{{"id": workflowID, "name": "PD Test"}}})
			return
		case r.Method == http.MethodGet && r.URL.Path == fmt.Sprintf("/repos/%s/%s/actions/workflows/%d/runs", owner, repo, workflowID):
			writeJSON(200, map[string]any{"workflow_runs": []map[string]any{{
				"id":         runID,
				"html_url":   "https://example.com/run/101",
				"head_sha":   "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
				"created_at": time.Now().UTC().Format(time.RFC3339),
			}}})
			return
		case r.Method == http.MethodGet && r.URL.Path == fmt.Sprintf("/repos/%s/%s/actions/runs/%d/jobs", owner, repo, runID):
			writeJSON(200, map[string]any{"jobs": []map[string]any{{
				"id":         jobID,
				"name":       "PD Test (unit)",
				"conclusion": "failure",
				"labels":     []string{"ubuntu-latest"},
			}}})
			return
		case r.Method == http.MethodGet && r.URL.Path == fmt.Sprintf("/repos/%s/%s/actions/jobs/%d/logs", owner, repo, jobID):
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(200)
			_, _ = w.Write([]byte(strings.Join([]string{
				"=== RUN   TestFoo",
				"--- FAIL: TestFoo (0.02s)",
				"dial tcp 10.0.0.1:443: i/o timeout",
				"FAIL",
				"exit status 1",
			}, "\n")))
			return
		case r.Method == http.MethodPost && r.URL.Path == fmt.Sprintf("/repos/%s/%s/labels", owner, repo):
			labelsCreated.Add(1)
			writeJSON(200, map[string]any{})
			return
		case r.Method == http.MethodPost && r.URL.Path == fmt.Sprintf("/repos/%s/%s/issues", owner, repo):
			issuesCreated.Add(1)
			writeJSON(201, map[string]any{"number": 1})
			return
		}
		w.WriteHeader(500)
		_, _ = w.Write([]byte("unexpected"))
	})

	cfg := config.Config{
		GitHubOwner:           owner,
		GitHubRepo:            repo,
		GitHubWriteOwner:      owner,
		GitHubWriteRepo:       repo,
		GitHubAPIBaseURL:      "http://stub",
		GitHubReadToken:       "read",
		GitHubIssueToken:      "issue",
		WorkflowName:          "PD Test",
		MaxRuns:               1,
		MaxJobs:               1,
		DryRun:                false,
		ConfidenceThreshold:   0.75,
		RequestTimeout:        2 * time.Second,
		WorkspaceMirrorDir:    t.TempDir(),
		WorkspaceWorktreesDir: t.TempDir(),
		WorkspaceMaxWorktrees: 1,
		RunOnce:               true,
		DiscoveryInterval:     time.Hour,
		InteractionInterval:   time.Minute,
		CopilotModel:          "",
	}

	gh := githubadapter.NewClientWithTransport("token", 2*time.Second, "http://stub", newHandlerTransport(mux))
	svc, cleanup, err := NewService(ctx, cfg, ServiceDeps{Store: mem, GitHubRead: gh, GitHubIssue: gh})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	defer func() { _ = cleanup() }()

	rt, err := runtime.New(cfg, svc, svc)
	if err != nil {
		t.Fatalf("runtime.New: %v", err)
	}
	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if issuesCreated.Load() != 0 {
		t.Fatalf("expected no issue created for infra-flake, got %d", issuesCreated.Load())
	}
	if labelsCreated.Load() != 0 {
		t.Fatalf("expected no label creation for infra-flake, got %d", labelsCreated.Load())
	}

	fps, err := mem.ListFingerprintsByState(ctx, domain.StateDiscovered, 10)
	if err != nil {
		t.Fatalf("ListFingerprintsByState: %v", err)
	}
	if len(fps) == 0 {
		t.Fatalf("expected at least 1 fingerprint, got 0")
	}
	for _, fp := range fps {
		if fp.Class != "infra-flake" {
			t.Fatalf("expected class infra-flake, got %q", fp.Class)
		}
		if fp.IssueNumber != 0 {
			t.Fatalf("expected no issue linked, got %d", fp.IssueNumber)
		}
	}
}
