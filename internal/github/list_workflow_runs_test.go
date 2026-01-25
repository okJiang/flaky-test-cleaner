package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestListWorkflowRuns_PassesBranchAndEvent(t *testing.T) {
	ctx := context.Background()
	var gotQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"workflow_runs":[]}`))
	}))
	defer srv.Close()

	c := NewClientWithBaseURL("t", 2*time.Second, srv.URL)
	_, err := c.ListWorkflowRuns(ctx, "o", "r", 123, ListWorkflowRunsOptions{Status: "failure", Branch: "main", Event: "push", PerPage: 10})
	if err != nil {
		t.Fatalf("ListWorkflowRuns: %v", err)
	}
	if gotQuery == "" {
		t.Fatalf("expected query")
	}
	if !(strings.Contains(gotQuery, "status=failure") && strings.Contains(gotQuery, "branch=main") && strings.Contains(gotQuery, "event=push") && strings.Contains(gotQuery, "per_page=10")) {
		t.Fatalf("unexpected query: %q", gotQuery)
	}
}
