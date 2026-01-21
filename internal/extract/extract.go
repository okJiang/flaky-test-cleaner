package extract

import "time"

type Input struct {
	Repo       string
	Workflow   string
	RunID      int64
	RunURL     string
	HeadSHA    string
	JobID      int64
	JobName    string
	RunnerOS   string
	OccurredAt time.Time

	RawLogText string

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
	if o.RunnerOS == "" {
		return ""
	}
	return o.RunnerOS
}

type Extractor interface {
	Extract(in Input) []Occurrence
}

type GoTestExtractor struct{}

func NewGoTestExtractor() *GoTestExtractor { return &GoTestExtractor{} }

func (e *GoTestExtractor) Extract(in Input) []Occurrence { return nil }