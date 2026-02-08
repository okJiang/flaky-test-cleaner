package ports

import (
	"context"

	"github.com/okJiang/flaky-test-cleaner/internal/domain"
)

type CIProvider interface {
	FindWorkflowByName(ctx context.Context, owner, repo, name string) (domain.Workflow, error)
	ListWorkflowRuns(ctx context.Context, owner, repo string, workflowID int64, opts domain.ListWorkflowRunsOptions) ([]domain.WorkflowRun, error)
	ListRunJobs(ctx context.Context, owner, repo string, runID int64, opts domain.ListRunJobsOptions) ([]domain.Job, error)
	DownloadJobLogs(ctx context.Context, owner, repo string, jobID int64) ([]byte, error)
}

type IssueService interface {
	EnsureLabels(ctx context.Context, owner, repo string, labels []string) error
	CreateIssue(ctx context.Context, owner, repo string, in domain.CreateIssueInput) (domain.Issue, error)
	UpdateIssue(ctx context.Context, owner, repo string, number int, in domain.UpdateIssueInput) (domain.Issue, error)
	GetIssue(ctx context.Context, owner, repo string, number int) (domain.Issue, error)
	CreateIssueComment(ctx context.Context, owner, repo string, number int, body string) error
	ListIssueComments(ctx context.Context, owner, repo string, number int, opts domain.ListIssueCommentsOptions) ([]domain.IssueComment, error)
	ListPullRequestReviews(ctx context.Context, owner, repo string, number int) ([]domain.PullRequestReview, error)
	GetCombinedStatus(ctx context.Context, owner, repo, ref string) (domain.CombinedStatus, error)
	GetPullRequest(ctx context.Context, owner, repo string, number int) (domain.PullRequest, error)
	CreatePullRequest(ctx context.Context, owner, repo string, in domain.CreatePullRequestInput) (domain.PullRequest, error)
	AddIssueLabels(ctx context.Context, owner, repo string, number int, labels []string) error
}

type Store interface {
	Migrate(ctx context.Context) error
	UpsertOccurrence(ctx context.Context, occ domain.Occurrence) error
	UpsertFingerprint(ctx context.Context, rec domain.FingerprintRecord) error
	GetFingerprint(ctx context.Context, fingerprint string) (*domain.FingerprintRecord, error)
	ListRecentOccurrences(ctx context.Context, fingerprint string, limit int) ([]domain.Occurrence, error)
	LinkIssue(ctx context.Context, fingerprint string, issueNumber int) error
	UpdateFingerprintState(ctx context.Context, fingerprint string, next domain.FingerprintState) error
	RecordAudit(ctx context.Context, action, target, result, errorMessage string) error
	ListFingerprintsByState(ctx context.Context, state domain.FingerprintState, limit int) ([]domain.FingerprintRecord, error)
	Close() error
}

type WorkLease interface {
	Pathname() string
	Release(ctx context.Context) error
}

type Workspace interface {
	Ensure(ctx context.Context) error
	Acquire(ctx context.Context, name, sha string) (WorkLease, error)
	CatFile(ctx context.Context, sha, path string) ([]byte, error)
	ListTree(ctx context.Context, sha, prefix string) ([]string, error)
	Grep(ctx context.Context, sha, pattern string, scopes ...string) ([]string, error)
	HasPath(ctx context.Context, sha, path string) (bool, error)
}

type AnalysisModel interface {
	GenerateIssueAgentComment(ctx context.Context, systemMsg, prompt string) (string, error)
}

type DiscoveryUseCase interface {
	DiscoveryOnce(ctx context.Context) error
}

type InteractionUseCase interface {
	InteractionOnce(ctx context.Context) error
}
