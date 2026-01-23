package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/okJiang/flaky-test-cleaner/internal/config"
	"github.com/okJiang/flaky-test-cleaner/internal/store"
)

func TestRunOnce_ApprovalSignal_LabelMovesToApprovedToFix(t *testing.T) {
	ctx := context.Background()
	mem := store.NewMemory()

	owner := "testorg"
	repo := "testrepo"
	issueNumber := 123
	fingerprint := "fp-waiting"

	now := time.Now().UTC()
	if err := mem.UpsertFingerprint(ctx, store.FingerprintRecord{
		Fingerprint:    fingerprint,
		Repo:           owner + "/" + repo,
		IssueNumber:    issueNumber,
		State:          store.StateWaitingForSignal,
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

	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := config.Config{
		GitHubOwner:         owner,
		GitHubRepo:          repo,
		GitHubAPIBaseURL:    srv.URL,
		GitHubReadToken:     "read",
		GitHubIssueToken:    "",
		WorkflowName:        "PD Test",
		MaxRuns:             1,
		MaxJobs:             1,
		DryRun:              true,
		ConfidenceThreshold: 0.75,
		RequestTimeout:      2 * time.Second,
	}

	if err := RunOnceWithDeps(ctx, cfg, RunOnceDeps{Store: mem}); err != nil {
		t.Fatalf("RunOnceWithDeps: %v", err)
	}

	rec, err := mem.GetFingerprint(ctx, fingerprint)
	if err != nil {
		t.Fatalf("GetFingerprint: %v", err)
	}
	if rec == nil {
		t.Fatalf("missing fingerprint")
	}
	if rec.State != store.StateApprovedToFix {
		t.Fatalf("expected state APPROVED_TO_FIX, got %s", rec.State)
	}
}

func TestRunOnce_ApprovalSignal_CommentMovesToApprovedToFix(t *testing.T) {
	ctx := context.Background()
	mem := store.NewMemory()

	owner := "testorg"
	repo := "testrepo"
	issueNumber := 124
	fingerprint := "fp-waiting-2"

	now := time.Now().UTC()
	if err := mem.UpsertFingerprint(ctx, store.FingerprintRecord{
		Fingerprint:    fingerprint,
		Repo:           owner + "/" + repo,
		IssueNumber:    issueNumber,
		State:          store.StateWaitingForSignal,
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
			"labels": []map[string]any{},
		})
	})
	mux.HandleFunc(fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, repo, issueNumber), func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"id":         int64(1),
			"body":       "/ai-fix please",
			"user":       map[string]any{"login": "maintainer"},
			"created_at": time.Now().UTC().Format(time.RFC3339),
		}})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := config.Config{
		GitHubOwner:         owner,
		GitHubRepo:          repo,
		GitHubAPIBaseURL:    srv.URL,
		GitHubReadToken:     "read",
		WorkflowName:        "PD Test",
		MaxRuns:             1,
		MaxJobs:             1,
		DryRun:              true,
		ConfidenceThreshold: 0.75,
		RequestTimeout:      2 * time.Second,
	}

	if err := RunOnceWithDeps(ctx, cfg, RunOnceDeps{Store: mem}); err != nil {
		t.Fatalf("RunOnceWithDeps: %v", err)
	}

	rec, err := mem.GetFingerprint(ctx, fingerprint)
	if err != nil {
		t.Fatalf("GetFingerprint: %v", err)
	}
	if rec == nil {
		t.Fatalf("missing fingerprint")
	}
	if rec.State != store.StateApprovedToFix {
		t.Fatalf("expected state APPROVED_TO_FIX, got %s", rec.State)
	}
}
