package exportjob

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/gombit-dev/gombit-forge/controlplane/internal/joblease"
)

// ErrBlankRepoName is returned by Enqueue when the repository name is blank. It
// is a caller (validation) error, distinct from a storage failure, so the HTTP
// layer can map it to 422 while treating any other Enqueue error as a 500.
var ErrBlankRepoName = errors.New("exportjob: a repository name is required")

// Service is the export-job store: enqueue, claim (for the worker), report
// terminal outcomes, and read status (for polling).
type Service struct {
	db    *gorm.DB
	lease time.Duration
	now   func() time.Time
}

// Option configures a Service.
type Option func(*Service)

// WithLease sets how long a claim or renewal holds a job; the default is
// joblease.DefaultLease.
func WithLease(d time.Duration) Option { return func(s *Service) { s.lease = d } }

// WithClock replaces the clock leases are stamped and checked with (tests).
func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

// NewService builds the service over db.
func NewService(db *gorm.DB, opts ...Option) *Service {
	s := &Service{db: db, lease: joblease.DefaultLease, now: time.Now}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Service) leaseEnd() time.Time { return s.now().UTC().Add(s.lease) }

// Enqueue creates a queued job that freezes revisionID for projectID, initiated
// by userID. The caller (the HTTP handler) resolves and authorizes the project
// and its head revision first, then hands the exact revision id here — so the
// job is pinned to what the caller saw, not to whatever head becomes by the time
// a worker runs it.
func (s *Service) Enqueue(ctx context.Context, projectID, revisionID, userID uint, repoName string, private bool) (ExportJob, error) {
	if strings.TrimSpace(repoName) == "" {
		return ExportJob{}, ErrBlankRepoName
	}
	job := ExportJob{
		ProjectID:  projectID,
		RevisionID: revisionID,
		UserID:     userID,
		RepoName:   repoName,
		Private:    private,
		Status:     StatusQueued,
	}
	if err := s.db.WithContext(ctx).Create(&job).Error; err != nil {
		return ExportJob{}, err
	}
	return job, nil
}

// Get returns the job by id, or ok=false if there is none.
func (s *Service) Get(ctx context.Context, jobID uint) (ExportJob, bool, error) {
	var job ExportJob
	err := s.db.WithContext(ctx).First(&job, jobID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ExportJob{}, false, nil
	}
	if err != nil {
		return ExportJob{}, false, err
	}
	return job, true, nil
}

// Claim atomically takes the oldest queued job and marks it running, returning
// ok=false when the queue is empty. It locks the row FOR UPDATE SKIP LOCKED so
// concurrent workers never claim the same job and never block each other on a
// job another worker already holds.
func (s *Service) Claim(ctx context.Context) (ExportJob, bool, error) {
	var job ExportJob
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status = ?", StatusQueued).
			Order("id").
			First(&job)
		if res.Error != nil {
			return res.Error
		}
		end := s.leaseEnd()
		job.Status, job.LeaseExpiresAt = StatusRunning, &end
		return tx.Model(&job).Updates(map[string]any{"status": StatusRunning, "lease_expires_at": end}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ExportJob{}, false, nil
	}
	if err != nil {
		return ExportJob{}, false, err
	}
	return job, true, nil
}

// MarkSucceeded records a completed export and its repository URL. It is valid
// only on a running job (see finish).
func (s *Service) MarkSucceeded(ctx context.Context, jobID uint, repoURL string) error {
	return s.finish(ctx, jobID, StatusSucceeded, map[string]any{
		"status":   StatusSucceeded,
		"repo_url": repoURL,
	})
}

// MarkFailed records a failed export with a sanitized reason. It is valid only
// on a running job (see finish). The caller is responsible for sanitizing — a
// raw toolchain error can leak filesystem paths or token material.
func (s *Service) MarkFailed(ctx context.Context, jobID uint, sanitized string) error {
	return s.finish(ctx, jobID, StatusFailed, map[string]any{
		"status": StatusFailed,
		"error":  sanitized,
	})
}

// finish applies a terminal update, enforcing the running → terminal transition
// the package documents: the update matches only a row that is still running,
// so a job that was never claimed (queued → terminal), an already-terminal job
// re-marked, or a missing row all fall through as RowsAffected == 0 and error
// rather than silently clobbering a terminal outcome. This is what keeps a late
// or duplicate worker from overwriting a job's recorded result once a requeue or
// stuck-job path exists.
func (s *Service) finish(ctx context.Context, jobID uint, status Status, fields map[string]any) error {
	fields["lease_expires_at"] = nil
	res := s.db.WithContext(ctx).Model(&ExportJob{}).
		Where("id = ? AND status = ?", jobID, StatusRunning).
		Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("exportjob: cannot mark job %d as %s: not running (missing or already terminal)", jobID, status)
	}
	return nil
}

// Renew extends a running job's lease (internal/joblease.Keep calls it while
// the worker works the job). It fails once the job is no longer running, e.g.
// because its lease lapsed and ExpireAbandoned failed it.
func (s *Service) Renew(ctx context.Context, jobID uint) error {
	res := s.db.WithContext(ctx).Model(&ExportJob{}).
		Where("id = ? AND status = ?", jobID, StatusRunning).
		Update("lease_expires_at", s.leaseEnd())
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("exportjob: cannot renew job %d: not running", jobID)
	}
	return nil
}

// ExpireAbandoned fails every running job whose lease lapsed: its worker died
// or was stopped mid-job, so nothing will finish it. It reports how many it
// failed. A running job with no lease was claimed before leases existed, by a
// worker that is gone, and is failed too. The job is not re-run (see
// internal/joblease).
func (s *Service) ExpireAbandoned(ctx context.Context) (int64, error) {
	res := s.db.WithContext(ctx).Model(&ExportJob{}).
		Where("status = ? AND (lease_expires_at IS NULL OR lease_expires_at < ?)", StatusRunning, s.now().UTC()).
		Updates(map[string]any{"status": StatusFailed, "error": AbandonedReason, "lease_expires_at": nil})
	return res.RowsAffected, res.Error
}

// AbandonedReason is the stored failure of a job ExpireAbandoned failed.
const AbandonedReason = "the worker stopped before the export finished; export again"
