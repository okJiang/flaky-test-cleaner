package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	githubadapter "github.com/okJiang/flaky-test-cleaner/internal/adapters/github"
	storeadapter "github.com/okJiang/flaky-test-cleaner/internal/adapters/store"
	"github.com/okJiang/flaky-test-cleaner/internal/config"
	"github.com/okJiang/flaky-test-cleaner/internal/domain"
	"github.com/okJiang/flaky-test-cleaner/internal/runtime"
)

func TestService_ApprovalSignal_LabelMovesToApprovedToFix(t *testing.T) {
	ctx := context.Background()
	mem := storeadapter.NewMemory()

	owner := "testorg"
	repo := "testrepo"
	issueNumber := 123
	fingerprint := "fp-waiting"

	now := time.Now().UTC()
	if err := mem.UpsertFingerprint(ctx, domain.FingerprintRecord{
		Fingerprint:    fingerprint,
		Repo:           owner + "/" + repo,
		IssueNumber:    issueNumber,
		State:          domain.StateWaitingForSignal,
		StateChangedAt: now,
		FirstSeenAt:    now.Add(-time.Hour),
		LastSeenAt:     now,
	}); err != nil {
		t.Fatalf("UpsertFingerprint: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/"+owner+"/"+repo+"/actions/workflows", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"workflows": []map[string]any{{"id": int64(1), "name": "PD Test"}}})
	})
	mux.HandleFunc("/repos/"+owner+"/"+repo+"/actions/workflows/1/runs", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []map[string]any{}})
	})
	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/issues/%d", owner, repo, issueNumber), func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number": issueNumber,
			"title":  "stub",
			"body":   "stub",
			"labels": []map[string]any{{"name": "flaky-test-cleaner/ai-fix-approved"}},
		})
	})
	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, repo, issueNumber), func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	})

	cfg := config.Config{
		GitHubOwner:         owner,
		GitHubRepo:          repo,
		GitHubWriteOwner:    owner,
		GitHubWriteRepo:     repo,
		GitHubAPIBaseURL:    "http://stub",
		GitHubReadToken:     "read",
		WorkflowName:        "PD Test",
		MaxRuns:             1,
		MaxJobs:             1,
		DryRun:              true,
		ConfidenceThreshold: 0.75,
		RequestTimeout:      2 * time.Second,
		RunOnce:             true,
		DiscoveryInterval:   time.Hour,
		InteractionInterval: time.Minute,
		CopilotModel:        "",
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

	rec, err := mem.GetFingerprint(ctx, fingerprint)
	if err != nil {
		t.Fatalf("GetFingerprint: %v", err)
	}
	if rec == nil {
		t.Fatalf("missing fingerprint")
	}
	if rec.State != domain.StateApprovedToFix {
		t.Fatalf("expected state APPROVED_TO_FIX, got %s", rec.State)
	}
}

func TestService_ApprovalSignal_CommentMovesToApprovedToFix(t *testing.T) {
	ctx := context.Background()
	mem := storeadapter.NewMemory()

	owner := "testorg"
	repo := "testrepo"
	issueNumber := 124
	fingerprint := "fp-waiting-2"

	now := time.Now().UTC()
	if err := mem.UpsertFingerprint(ctx, domain.FingerprintRecord{
		Fingerprint:    fingerprint,
		Repo:           owner + "/" + repo,
		IssueNumber:    issueNumber,
		State:          domain.StateWaitingForSignal,
		StateChangedAt: now,
		FirstSeenAt:    now.Add(-time.Hour),
		LastSeenAt:     now,
	}); err != nil {
		t.Fatalf("UpsertFingerprint: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/"+owner+"/"+repo+"/actions/workflows", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"workflows": []map[string]any{{"id": int64(1), "name": "PD Test"}}})
	})
	mux.HandleFunc("/repos/"+owner+"/"+repo+"/actions/workflows/1/runs", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []map[string]any{}})
	})
	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/issues/%d", owner, repo, issueNumber), func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"number": issueNumber, "title": "stub", "body": "stub", "labels": []map[string]any{}})
	})
	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, repo, issueNumber), func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"id":         int64(1),
			"body":       "/ai-fix please",
			"user":       map[string]any{"login": "maintainer"},
			"created_at": time.Now().UTC().Format(time.RFC3339),
		}})
	})

	cfg := config.Config{
		GitHubOwner:         owner,
		GitHubRepo:          repo,
		GitHubWriteOwner:    owner,
		GitHubWriteRepo:     repo,
		GitHubAPIBaseURL:    "http://stub",
		GitHubReadToken:     "read",
		WorkflowName:        "PD Test",
		MaxRuns:             1,
		MaxJobs:             1,
		DryRun:              true,
		ConfidenceThreshold: 0.75,
		RequestTimeout:      2 * time.Second,
		RunOnce:             true,
		DiscoveryInterval:   time.Hour,
		InteractionInterval: time.Minute,
		CopilotModel:        "",
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

	rec, err := mem.GetFingerprint(ctx, fingerprint)
	if err != nil {
		t.Fatalf("GetFingerprint: %v", err)
	}
	if rec == nil {
		t.Fatalf("missing fingerprint")
	}
	if rec.State != domain.StateApprovedToFix {
		t.Fatalf("expected state APPROVED_TO_FIX, got %s", rec.State)
	}
}
