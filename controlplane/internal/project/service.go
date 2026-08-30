package project

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/gombit-dev/gombit-forge/internal/spec"
)

// Sentinel errors the HTTP layer (issue #39) maps to status codes.
var (
	// ErrProjectNotFound means no project has the given id.
	ErrProjectNotFound = errors.New("project: not found")
	// ErrInvalidSpec means the candidate spec failed semantic validation; the
	// wrapped error carries the diagnostics.
	ErrInvalidSpec = errors.New("project: invalid spec")
	// ErrCorruptLineage means a project's HeadRevisionID points at a revision
	// that does not exist — an impossible state, distinct from "no revisions
	// yet". The API layer maps it to a 500, not a 404.
	ErrCorruptLineage = errors.New("project: head revision missing")
)

// Service holds project and revision operations.
//
// now is a seam for a future timestamp-ordering test to replace; today it is
// always the system clock — there is no option or setter yet, so the comment
// promises nothing the package cannot do.
type Service struct {
	db  *gorm.DB
	now func() time.Time
}

// NewService builds a Service over db with the system clock.
func NewService(db *gorm.DB) *Service {
	return &Service{db: db, now: time.Now}
}

// CreateProject creates an empty project in an organization. It has no revisions
// until CreateRevision is called, so HeadRevisionID is nil.
//
// Authorization (the caller's org role) is applied by the API layer (#39); this
// is the data operation.
func (s *Service) CreateProject(ctx context.Context, orgID uint, name, slug string, createdBy uint) (Project, error) {
	p := Project{OrganizationID: orgID, Name: name, Slug: slug, CreatedBy: createdBy}
	if err := s.db.WithContext(ctx).Create(&p).Error; err != nil {
		return Project{}, fmt.Errorf("create project: %w", err)
	}
	return p, nil
}

// CreateRevision records an accepted candidate spec as a new immutable revision
// of the project and advances the project's head to it.
//
// The spec is validated (an invalid candidate never becomes a revision),
// canonicalized and hashed through the compiler's internal/spec, so the stored
// bytes and hash are the compiler's own. Lineage is set by pointing the new
// revision's parent at the project's current head, then moving head forward —
// all within one transaction that locks the project row, so concurrent
// revisions of the same project serialize into a linear chain rather than
// forking off a shared parent.
func (s *Service) CreateRevision(ctx context.Context, projectID uint, sp *spec.ProjectSpec, createdBy uint) (Revision, error) {
	if d := spec.Validate(sp); d != nil {
		return Revision{}, fmt.Errorf("%w: %s", ErrInvalidSpec, d.Error())
	}
	canonical, err := spec.Marshal(sp)
	if err != nil {
		return Revision{}, fmt.Errorf("canonicalize spec: %w", err)
	}
	hash, err := spec.Hash(sp)
	if err != nil {
		return Revision{}, fmt.Errorf("hash spec: %w", err)
	}

	var rev Revision
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Lock the project row so two concurrent revisions cannot both read the
		// same head and fork the chain.
		var p Project
		lock := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&p, projectID)
		if errors.Is(lock.Error, gorm.ErrRecordNotFound) {
			return ErrProjectNotFound
		}
		if lock.Error != nil {
			return lock.Error
		}

		rev = Revision{
			ProjectID:        projectID,
			SpecVersion:      sp.SpecVersion,
			SpecJSON:         string(canonical),
			SpecHash:         hash,
			ParentRevisionID: p.HeadRevisionID, // nil for the first revision
			CreatedBy:        createdBy,
			CreatedAt:        s.now(),
		}
		if err := tx.Create(&rev).Error; err != nil {
			return err
		}
		// Advance head. Update the column explicitly rather than saving the
		// struct so nothing else on the project row is touched.
		return tx.Model(&Project{}).Where("id = ?", projectID).
			Update("head_revision_id", rev.ID).Error
	})
	if err != nil {
		if errors.Is(err, ErrProjectNotFound) {
			return Revision{}, err
		}
		return Revision{}, fmt.Errorf("create revision: %w", err)
	}
	return rev, nil
}

// Head returns a project's current revision. It returns ErrProjectNotFound if
// the project does not exist, (Revision{}, false, nil) if the project exists but
// has no revisions yet, and ErrCorruptLineage if the project's head points at a
// revision that is gone.
func (s *Service) Head(ctx context.Context, projectID uint) (Revision, bool, error) {
	var p Project
	err := s.db.WithContext(ctx).First(&p, projectID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Revision{}, false, ErrProjectNotFound
	}
	if err != nil {
		return Revision{}, false, fmt.Errorf("load project: %w", err)
	}
	if p.HeadRevisionID == nil {
		return Revision{}, false, nil
	}
	rev, ok, err := s.Revision(ctx, *p.HeadRevisionID)
	if err != nil {
		return Revision{}, false, err
	}
	if !ok {
		// The project claims a head, but that revision is gone: the chain is
		// corrupt. Surface it loudly rather than disguising it as a fresh
		// project with no revisions.
		return Revision{}, false, fmt.Errorf("%w: project %d head %d",
			ErrCorruptLineage, projectID, *p.HeadRevisionID)
	}
	return rev, true, nil
}

// Revision returns a single revision by id.
func (s *Service) Revision(ctx context.Context, id uint) (Revision, bool, error) {
	var rev Revision
	err := s.db.WithContext(ctx).First(&rev, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Revision{}, false, nil
	}
	if err != nil {
		return Revision{}, false, fmt.Errorf("load revision: %w", err)
	}
	return rev, true, nil
}
