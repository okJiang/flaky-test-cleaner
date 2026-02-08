package domain

import "time"

type Workflow struct {
	ID   int64
	Name string
}

type ListWorkflowRunsOptions struct {
	Status  string
	Branch  string
	Event   string
	PerPage int
}

type WorkflowRun struct {
	ID         int64
	HTMLURL    string
	HeadSHA    string
	HeadBranch string
	Event      string
	CreatedAt  time.Time
}

type ListRunJobsOptions struct {
	PerPage int
}

type Job struct {
	ID         int64
	Name       string
	Conclusion string
	RunnerName string
	RunnerOS   string
	Labels     []string
}

type Occurrence struct {
	Repo           string
	Workflow       string
	RunID          int64
	RunURL         string
	HeadSHA        string
	JobID          int64
	JobName        string
	RunnerOS       string
	OccurredAt     time.Time
	Framework      string
	TestName       string
	ErrorSignature string
	Excerpt        string
	Fingerprint    string
}

func (o Occurrence) PlatformBucket() string {
	return o.RunnerOS
}

type Class string

const (
	ClassFlakyTest        Class = "flaky-test"
	ClassInfraFlake       Class = "infra-flake"
	ClassLikelyRegression Class = "likely-regression"
	ClassUnknown          Class = "unknown"
)

type Classification struct {
	Class       Class
	Confidence  float64
	Explanation string
}

type FingerprintState string

const (
	StateUnknown          FingerprintState = ""
	StateDiscovered       FingerprintState = "DISCOVERED"
	StateIssueOpen        FingerprintState = "ISSUE_OPEN"
	StateTriaged          FingerprintState = "TRIAGED"
	StateWaitingForSignal FingerprintState = "WAITING_FOR_SIGNAL"
	StateNeedsUpdate      FingerprintState = "NEEDS_UPDATE"
	StateApprovedToFix    FingerprintState = "APPROVED_TO_FIX"
	StatePROpen           FingerprintState = "PR_OPEN"
	StatePRNeedsChanges   FingerprintState = "PR_NEEDS_CHANGES"
	StatePRUpdating       FingerprintState = "PR_UPDATING"
	StateMerged           FingerprintState = "MERGED"
	StateClosedWontFix    FingerprintState = "CLOSED_WONTFIX"
)

type FingerprintRecord struct {
	Fingerprint        string
	FingerprintVersion string
	Repo               string
	TestName           string
	Framework          string
	Class              string
	Confidence         float64
	IssueNumber        int
	PRNumber           int
	LastIssueCommentID int64
	LastPRCommentID    int64
	State              FingerprintState
	StateChangedAt     time.Time
	FirstSeenAt        time.Time
	LastSeenAt         time.Time
}

type Issue struct {
	Number int
	Title  string
	Body   string
	Labels []IssueLabel
}

type IssueLabel struct {
	Name string
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
	State  *string
}

type User struct {
	Login string
}

type IssueComment struct {
	ID        int64
	Body      string
	User      User
	CreatedAt time.Time
}

type ListIssueCommentsOptions struct {
	PerPage int
}

type PullRequest struct {
	Number   int
	HTMLURL  string
	State    string
	Merged   bool
	MergedAt *time.Time
	Head     PRHead
}

type PRHead struct {
	Ref string
	SHA string
}

type PullRequestReview struct {
	ID          int64
	State       string
	Body        string
	User        User
	SubmittedAt *time.Time
}

type CombinedStatus struct {
	State    string
	Statuses []Status
}

type Status struct {
	State       string
	Context     string
	Description string
	TargetURL   string
	UpdatedAt   time.Time
}

type CreatePullRequestInput struct {
	Title string
	Head  string
	Base  string
	Body  string
	Draft bool
}

type PRFeedback struct {
	PRNumber         int
	PRURL            string
	HeadSHA          string
	ChangesRequested []PullRequestReview
	CombinedStatus   CombinedStatus

	LatestIssueCommentID int64
	NewIssueComments     []IssueComment
}

func (f PRFeedback) NeedsUpdate() bool {
	if len(f.NewIssueComments) > 0 {
		return true
	}
	if len(f.ChangesRequested) > 0 {
		return true
	}
	return f.CombinedStatus.State == "failure" || f.CombinedStatus.State == "error"
}
