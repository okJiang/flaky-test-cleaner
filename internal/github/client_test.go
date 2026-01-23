package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClient_CreateIssue_RetryPreservesBody(t *testing.T) {
	ctx := context.Background()

	var calls atomic.Int64
	var firstBody string
	var secondBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/o/r/issues" {
			w.WriteHeader(500)
			return
		}
		b, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if calls.Add(1) == 1 {
			firstBody = string(b)
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"message":"temporary"}`))
			return
		}
		secondBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number":1,"title":"x","body":"y","labels":[]}`))
	}))
	defer srv.Close()

	c := NewClientWithBaseURL("token", 2*time.Second, srv.URL)
	_, err := c.CreateIssue(ctx, "o", "r", CreateIssueInput{Title: "hello", Body: "world", Labels: []string{"l"}})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected 2 calls, got %d", calls.Load())
	}
	if strings.TrimSpace(firstBody) == "" || strings.TrimSpace(secondBody) == "" {
		t.Fatalf("expected non-empty bodies, got first=%q second=%q", firstBody, secondBody)
	}
	if firstBody != secondBody {
		t.Fatalf("expected bodies equal on retry, got\nfirst: %s\nsecond: %s", firstBody, secondBody)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(firstBody), &parsed); err != nil {
		t.Fatalf("expected json body, got %q: %v", firstBody, err)
	}
	if parsed["title"] != "hello" {
		t.Fatalf("expected title hello, got %v", parsed["title"])
	}
}

func TestNewClientWithBaseURL_TrimsSlash(t *testing.T) {
	ctx := context.Background()
	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"workflows":[]}`))
	}))
	defer srv.Close()

	c := NewClientWithBaseURL("", 2*time.Second, srv.URL+"/")
	_, err := c.FindWorkflowByName(ctx, "o", "r", "x")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if gotPath != "/repos/o/r/actions/workflows" {
		t.Fatalf("unexpected path: %q", gotPath)
	}
}
