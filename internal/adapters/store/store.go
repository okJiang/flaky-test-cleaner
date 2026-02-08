package store

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/okJiang/flaky-test-cleaner/internal/config"
	"github.com/okJiang/flaky-test-cleaner/internal/domain"
)

var errInvalidTransition = errors.New("invalid fingerprint state transition")

var allowedTransitions = map[domain.FingerprintState][]domain.FingerprintState{
	domain.StateUnknown:          {domain.StateDiscovered},
	domain.StateDiscovered:       {domain.StateIssueOpen},
	domain.StateIssueOpen:        {domain.StateTriaged, domain.StateNeedsUpdate},
	domain.StateTriaged:          {domain.StateWaitingForSignal, domain.StateNeedsUpdate},
	domain.StateWaitingForSignal: {domain.StateTriaged, domain.StateApprovedToFix, domain.StateClosedWontFix, domain.StateNeedsUpdate},
	domain.StateNeedsUpdate:      {domain.StateIssueOpen},
	domain.StateApprovedToFix:    {domain.StatePROpen},
	domain.StatePROpen:           {domain.StatePRNeedsChanges, domain.StateMerged, domain.StateClosedWontFix},
	domain.StatePRNeedsChanges:   {domain.StatePRUpdating},
	domain.StatePRUpdating:       {domain.StatePROpen},
	domain.StateMerged:           {},
	domain.StateClosedWontFix:    {},
}

func validateTransition(current, next domain.FingerprintState) error {
	if next == "" {
		return fmt.Errorf("%w: next state empty", errInvalidTransition)
	}
	if current == "" {
		current = domain.StateDiscovered
	}
	if current == next {
		return nil
	}
	for _, candidate := range allowedTransitions[current] {
		if candidate == next {
			return nil
		}
	}
	return fmt.Errorf("%w: %s -> %s", errInvalidTransition, current, next)
}

type Memory struct {
	mu          sync.Mutex
	fps         map[string]domain.FingerprintRecord
	occurrences map[string][]domain.Occurrence
}

func NewMemory() *Memory {
	return &Memory{fps: map[string]domain.FingerprintRecord{}, occurrences: map[string][]domain.Occurrence{}}
}

func (m *Memory) Migrate(ctx context.Context) error { return nil }

func (m *Memory) UpsertOccurrence(ctx context.Context, occ domain.Occurrence) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.occurrences[occ.Fingerprint] = append(m.occurrences[occ.Fingerprint], occ)
	return nil
}

func (m *Memory) UpsertFingerprint(ctx context.Context, rec domain.FingerprintRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	prev, ok := m.fps[rec.Fingerprint]
	if ok {
		if prev.FirstSeenAt.IsZero() || (!rec.FirstSeenAt.IsZero() && rec.FirstSeenAt.Before(prev.FirstSeenAt)) {
			prev.FirstSeenAt = rec.FirstSeenAt
		}
		if rec.LastSeenAt.After(prev.LastSeenAt) {
			prev.LastSeenAt = rec.LastSeenAt
		}
		if rec.Class != "" {
			prev.Class = rec.Class
		}
		if rec.Confidence != 0 {
			prev.Confidence = rec.Confidence
		}
		if rec.TestName != "" {
			prev.TestName = rec.TestName
		}
		if rec.Framework != "" {
			prev.Framework = rec.Framework
		}
		if rec.Repo != "" {
			prev.Repo = rec.Repo
		}
		if rec.LastIssueCommentID > prev.LastIssueCommentID {
			prev.LastIssueCommentID = rec.LastIssueCommentID
		}
		if rec.LastPRCommentID > prev.LastPRCommentID {
			prev.LastPRCommentID = rec.LastPRCommentID
		}
		if rec.FingerprintVersion != "" {
			prev.FingerprintVersion = rec.FingerprintVersion
		}
		prev = ensureStateDefaults(prev)
		m.fps[rec.Fingerprint] = prev
		return nil
	}
	rec = ensureStateDefaults(rec)
	m.fps[rec.Fingerprint] = rec
	return nil
}

func (m *Memory) GetFingerprint(ctx context.Context, fingerprint string) (*domain.FingerprintRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.fps[fingerprint]
	if !ok {
		return nil, nil
	}
	rec = ensureStateDefaults(rec)
	cpy := rec
	return &cpy, nil
}

func (m *Memory) ListRecentOccurrences(ctx context.Context, fingerprint string, limit int) ([]domain.Occurrence, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := m.occurrences[fingerprint]
	if limit <= 0 || len(list) <= limit {
		out := make([]domain.Occurrence, len(list))
		copy(out, list)
		return out, nil
	}
	out := make([]domain.Occurrence, limit)
	copy(out, list[len(list)-limit:])
	return out, nil
}

func (m *Memory) LinkIssue(ctx context.Context, fingerprint string, issueNumber int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.fps[fingerprint]
	if !ok {
		return errors.New("fingerprint not found")
	}
	rec.IssueNumber = issueNumber
	m.fps[fingerprint] = rec
	return nil
}

func (m *Memory) UpdateFingerprintState(ctx context.Context, fingerprint string, next domain.FingerprintState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.fps[fingerprint]
	if !ok {
		return errors.New("fingerprint not found")
	}
	rec = ensureStateDefaults(rec)
	if err := validateTransition(rec.State, next); err != nil {
		return err
	}
	if rec.State == next {
		return nil
	}
	rec.State = next
	rec.StateChangedAt = time.Now().UTC()
	m.fps[fingerprint] = rec
	return nil
}

func (m *Memory) Close() error { return nil }

func (m *Memory) RecordAudit(ctx context.Context, action, target, result, errorMessage string) error {
	return nil
}

func (m *Memory) ListFingerprintsByState(ctx context.Context, state domain.FingerprintState, limit int) ([]domain.FingerprintRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.FingerprintRecord
	if limit <= 0 {
		limit = len(m.fps)
	}
	for _, rec := range m.fps {
		rec = ensureStateDefaults(rec)
		if rec.State != state {
			continue
		}
		out = append(out, rec)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func ensureStateDefaults(rec domain.FingerprintRecord) domain.FingerprintRecord {
	if rec.State == "" {
		rec.State = domain.StateDiscovered
	}
	if rec.StateChangedAt.IsZero() {
		if !rec.LastSeenAt.IsZero() {
			rec.StateChangedAt = rec.LastSeenAt
		} else if !rec.FirstSeenAt.IsZero() {
			rec.StateChangedAt = rec.FirstSeenAt
		} else {
			rec.StateChangedAt = time.Now().UTC()
		}
	}
	if strings.TrimSpace(rec.FingerprintVersion) == "" {
		rec.FingerprintVersion = "v1"
	}
	return rec
}

type TiDBStore struct {
	cfg config.Config
	db  *sql.DB
}

func NewTiDBStore(cfg config.Config) (*TiDBStore, error) {
	if err := registerTLS(cfg.TiDBCACertPath); err != nil {
		return nil, err
	}
	dsn := mysqlDSN(cfg, cfg.TiDBDatabase)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetMaxIdleConns(5)
	db.SetMaxOpenConns(10)
	return &TiDBStore{cfg: cfg, db: db}, nil
}

func (t *TiDBStore) Migrate(ctx context.Context) error {
	if err := t.ensureDatabase(ctx); err != nil {
		return err
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS occurrences (
			fingerprint VARCHAR(64) NOT NULL,
			repo VARCHAR(200) NOT NULL,
			workflow VARCHAR(200) NOT NULL,
			run_id BIGINT NOT NULL,
			run_url TEXT NOT NULL,
			head_sha VARCHAR(64) NOT NULL,
			job_id BIGINT NOT NULL,
			job_name VARCHAR(200) NOT NULL,
			runner_os VARCHAR(100) NOT NULL,
			occurred_at TIMESTAMP NOT NULL,
			framework VARCHAR(50) NOT NULL,
			test_name VARCHAR(300) NOT NULL,
			error_signature TEXT NOT NULL,
			excerpt MEDIUMTEXT NOT NULL,
			PRIMARY KEY (fingerprint, run_id, job_id, test_name(128))
		)`,
		`CREATE TABLE IF NOT EXISTS fingerprints (
			fingerprint VARCHAR(64) NOT NULL PRIMARY KEY,
			fingerprint_version VARCHAR(16) NOT NULL DEFAULT 'v1',
			repo VARCHAR(200) NOT NULL,
			test_name VARCHAR(300) NOT NULL,
			framework VARCHAR(50) NOT NULL,
			class VARCHAR(50) NOT NULL,
			confidence DOUBLE NOT NULL,
			issue_number INT NOT NULL DEFAULT 0,
			pr_number INT NOT NULL DEFAULT 0,
			last_issue_comment_id BIGINT NOT NULL DEFAULT 0,
			last_pr_comment_id BIGINT NOT NULL DEFAULT 0,
			first_seen_at TIMESTAMP NOT NULL,
			last_seen_at TIMESTAMP NOT NULL,
			state VARCHAR(50) NOT NULL DEFAULT 'DISCOVERED',
			state_changed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`ALTER TABLE fingerprints ADD COLUMN IF NOT EXISTS fingerprint_version VARCHAR(16) NOT NULL DEFAULT 'v1'`,
		`ALTER TABLE fingerprints ADD COLUMN IF NOT EXISTS state VARCHAR(50) NOT NULL DEFAULT 'DISCOVERED'`,
		`ALTER TABLE fingerprints ADD COLUMN IF NOT EXISTS state_changed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP`,
		`ALTER TABLE fingerprints ADD COLUMN IF NOT EXISTS last_issue_comment_id BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE fingerprints ADD COLUMN IF NOT EXISTS last_pr_comment_id BIGINT NOT NULL DEFAULT 0`,
		`CREATE TABLE IF NOT EXISTS audit_log (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			action VARCHAR(100) NOT NULL,
			target VARCHAR(200) NOT NULL,
			result VARCHAR(50) NOT NULL,
			error_message TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS costs (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			provider VARCHAR(50) NOT NULL,
			model VARCHAR(100) NOT NULL,
			tokens BIGINT NOT NULL,
			cost_usd DOUBLE NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := t.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (t *TiDBStore) UpsertOccurrence(ctx context.Context, occ domain.Occurrence) error {
	query := `INSERT INTO occurrences (
		fingerprint, repo, workflow, run_id, run_url, head_sha, job_id, job_name, runner_os,
		occurred_at, framework, test_name, error_signature, excerpt
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	ON DUPLICATE KEY UPDATE
		occurred_at = VALUES(occurred_at),
		excerpt = VALUES(excerpt)`
	_, err := t.db.ExecContext(ctx, query,
		occ.Fingerprint, occ.Repo, occ.Workflow, occ.RunID, occ.RunURL, occ.HeadSHA, occ.JobID, occ.JobName, occ.RunnerOS,
		occ.OccurredAt, occ.Framework, occ.TestName, occ.ErrorSignature, occ.Excerpt,
	)
	return err
}

func (t *TiDBStore) UpsertFingerprint(ctx context.Context, rec domain.FingerprintRecord) error {
	rec = ensureStateDefaults(rec)
	query := `INSERT INTO fingerprints (
		fingerprint, fingerprint_version, repo, test_name, framework, class, confidence, issue_number, pr_number, last_issue_comment_id, last_pr_comment_id, first_seen_at, last_seen_at, state, state_changed_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	ON DUPLICATE KEY UPDATE
		fingerprint_version = VALUES(fingerprint_version),
		repo = VALUES(repo),
		test_name = VALUES(test_name),
		framework = VALUES(framework),
		class = IF(VALUES(class)='', class, VALUES(class)),
		confidence = IF(VALUES(confidence)=0, confidence, VALUES(confidence)),
		issue_number = IF(VALUES(issue_number)=0, issue_number, VALUES(issue_number)),
		pr_number = IF(VALUES(pr_number)=0, pr_number, VALUES(pr_number)),
		last_issue_comment_id = GREATEST(last_issue_comment_id, VALUES(last_issue_comment_id)),
		last_pr_comment_id = GREATEST(last_pr_comment_id, VALUES(last_pr_comment_id)),
		first_seen_at = LEAST(first_seen_at, VALUES(first_seen_at)),
		last_seen_at = GREATEST(last_seen_at, VALUES(last_seen_at))`
	_, err := t.db.ExecContext(ctx, query,
		rec.Fingerprint, rec.FingerprintVersion, rec.Repo, rec.TestName, rec.Framework, rec.Class, rec.Confidence, rec.IssueNumber, rec.PRNumber, rec.LastIssueCommentID, rec.LastPRCommentID, rec.FirstSeenAt, rec.LastSeenAt, rec.State, rec.StateChangedAt,
	)
	return err
}

func (t *TiDBStore) GetFingerprint(ctx context.Context, fingerprint string) (*domain.FingerprintRecord, error) {
	query := `SELECT fingerprint, fingerprint_version, repo, test_name, framework, class, confidence, issue_number, pr_number, last_issue_comment_id, last_pr_comment_id, first_seen_at, last_seen_at, state, state_changed_at
		FROM fingerprints WHERE fingerprint = ?`
	row := t.db.QueryRowContext(ctx, query, fingerprint)
	var rec domain.FingerprintRecord
	if err := row.Scan(&rec.Fingerprint, &rec.FingerprintVersion, &rec.Repo, &rec.TestName, &rec.Framework, &rec.Class, &rec.Confidence, &rec.IssueNumber, &rec.PRNumber, &rec.LastIssueCommentID, &rec.LastPRCommentID, &rec.FirstSeenAt, &rec.LastSeenAt, &rec.State, &rec.StateChangedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	rec = ensureStateDefaults(rec)
	return &rec, nil
}

func (t *TiDBStore) ListRecentOccurrences(ctx context.Context, fingerprint string, limit int) ([]domain.Occurrence, error) {
	if limit <= 0 {
		limit = 5
	}
	query := `SELECT repo, workflow, run_id, run_url, head_sha, job_id, job_name, runner_os,
		occurred_at, framework, test_name, error_signature, excerpt, fingerprint
		FROM occurrences WHERE fingerprint = ? ORDER BY occurred_at DESC LIMIT ?`
	rows, err := t.db.QueryContext(ctx, query, fingerprint, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Occurrence
	for rows.Next() {
		var occ domain.Occurrence
		if err := rows.Scan(&occ.Repo, &occ.Workflow, &occ.RunID, &occ.RunURL, &occ.HeadSHA, &occ.JobID, &occ.JobName, &occ.RunnerOS,
			&occ.OccurredAt, &occ.Framework, &occ.TestName, &occ.ErrorSignature, &occ.Excerpt, &occ.Fingerprint); err != nil {
			return nil, err
		}
		out = append(out, occ)
	}
	return out, rows.Err()
}

func (t *TiDBStore) LinkIssue(ctx context.Context, fingerprint string, issueNumber int) error {
	_, err := t.db.ExecContext(ctx, `UPDATE fingerprints SET issue_number = ? WHERE fingerprint = ?`, issueNumber, fingerprint)
	return err
}

func (t *TiDBStore) UpdateFingerprintState(ctx context.Context, fingerprint string, next domain.FingerprintState) error {
	rec, err := t.GetFingerprint(ctx, fingerprint)
	if err != nil {
		return err
	}
	if rec == nil {
		return errors.New("fingerprint not found")
	}
	if err := validateTransition(rec.State, next); err != nil {
		return err
	}
	if rec.State == next {
		return nil
	}
	_, err = t.db.ExecContext(ctx, `UPDATE fingerprints SET state = ?, state_changed_at = ? WHERE fingerprint = ?`, next, time.Now().UTC(), fingerprint)
	return err
}

func (t *TiDBStore) Close() error { return t.db.Close() }

func (t *TiDBStore) RecordAudit(ctx context.Context, action, target, result, errorMessage string) error {
	_, err := t.db.ExecContext(ctx, `INSERT INTO audit_log (action, target, result, error_message) VALUES (?,?,?,?)`,
		action, target, result, errorMessage)
	return err
}

func (t *TiDBStore) ListFingerprintsByState(ctx context.Context, state domain.FingerprintState, limit int) ([]domain.FingerprintRecord, error) {
	if limit <= 0 {
		limit = 20
	}
	query := `SELECT fingerprint, fingerprint_version, repo, test_name, framework, class, confidence, issue_number, pr_number, last_issue_comment_id, last_pr_comment_id, first_seen_at, last_seen_at, state, state_changed_at
		FROM fingerprints WHERE state = ? ORDER BY last_seen_at DESC LIMIT ?`
	rows, err := t.db.QueryContext(ctx, query, state, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.FingerprintRecord
	for rows.Next() {
		var rec domain.FingerprintRecord
		if err := rows.Scan(&rec.Fingerprint, &rec.FingerprintVersion, &rec.Repo, &rec.TestName, &rec.Framework, &rec.Class, &rec.Confidence, &rec.IssueNumber, &rec.PRNumber, &rec.LastIssueCommentID, &rec.LastPRCommentID, &rec.FirstSeenAt, &rec.LastSeenAt, &rec.State, &rec.StateChangedAt); err != nil {
			return nil, err
		}
		rec = ensureStateDefaults(rec)
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (t *TiDBStore) ensureDatabase(ctx context.Context) error {
	adminDSN := mysqlDSN(t.cfg, "")
	admin, err := sql.Open("mysql", adminDSN)
	if err != nil {
		return err
	}
	defer admin.Close()
	_, err = admin.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", t.cfg.TiDBDatabase))
	return err
}

func registerTLS(caPath string) error {
	if strings.TrimSpace(caPath) == "" {
		return nil
	}
	certPool := x509.NewCertPool()
	pem, err := os.ReadFile(caPath)
	if err != nil {
		return err
	}
	if !certPool.AppendCertsFromPEM(pem) {
		return errors.New("failed to append CA cert")
	}
	return mysql.RegisterTLSConfig("tidb", &tls.Config{RootCAs: certPool})
}

func mysqlDSN(cfg config.Config, database string) string {
	params := "parseTime=true"
	if strings.TrimSpace(cfg.TiDBCACertPath) != "" {
		params += "&tls=tidb"
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?%s",
		cfg.TiDBUser,
		cfg.TiDBPassword,
		cfg.TiDBHost,
		cfg.TiDBPort,
		database,
		params,
	)
}
