package project_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gombit-dev/gombit/auth"

	"github.com/gombit-dev/gombit-forge/controlplane/internal/dbtest"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/org"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/project"
)

// The tests in this file exercise the integrity rules the initial migration
// (#101) adds and that GORM's AutoMigrate cannot express, so they are only
// meaningful against the real migrated schema dbtest.DB now applies: the
// ON DELETE behaviour of the foreign keys and the project_revisions append-only
// trigger. They assert behaviour (a delete cascades, a delete is refused, an
// update is rejected), not catalog metadata — a schema-only check would pass
// against constraints that were declared wrong.

// TestOrgDeleteCascadesToMembersAndProjects: ON DELETE CASCADE on the org side.
// Dropping an organization removes its members and its projects in one step.
func TestOrgDeleteCascadesToMembersAndProjects(t *testing.T) {
	db := dbtest.DB(t)
	seedFKDeps(t, db)

	if err := db.Create(&org.Member{OrganizationID: 1, UserID: 7, Role: org.RoleOwner}).Error; err != nil {
		t.Fatalf("seed member: %v", err)
	}
	if _, err := project.NewService(db).CreateProject(context.Background(), 1, "P", "p", 7); err != nil {
		t.Fatalf("create project: %v", err)
	}

	if err := db.Delete(&org.Organization{}, uint(1)).Error; err != nil {
		t.Fatalf("delete org: %v", err)
	}

	var members, projects int64
	db.Model(&org.Member{}).Count(&members)
	db.Model(&project.Project{}).Count(&projects)
	if members != 0 {
		t.Errorf("members after org delete = %d, want 0 (CASCADE)", members)
	}
	if projects != 0 {
		t.Errorf("projects after org delete = %d, want 0 (CASCADE)", projects)
	}
}

// TestUserDeleteRestrictedByMembership: ON DELETE RESTRICT on the user side. A
// Gombit user cannot be deleted out from under a live membership.
func TestUserDeleteRestrictedByMembership(t *testing.T) {
	db := dbtest.DB(t)
	seedFKDeps(t, db)

	if err := db.Create(&org.Member{OrganizationID: 1, UserID: 7, Role: org.RoleOwner}).Error; err != nil {
		t.Fatalf("seed member: %v", err)
	}

	if err := db.Delete(&auth.User{}, uint(7)).Error; err == nil {
		t.Fatal("deleting a user with a live membership must be refused (RESTRICT)")
	}
	// The user is still there.
	var users int64
	db.Model(&auth.User{}).Where("id = ?", 7).Count(&users)
	if users != 1 {
		t.Errorf("user count after refused delete = %d, want 1", users)
	}
}

// TestProjectDeleteCascadesRevisions: ON DELETE CASCADE from project_revisions
// to projects. A project's revisions die with it.
func TestProjectDeleteCascadesRevisions(t *testing.T) {
	db := dbtest.DB(t)
	seedFKDeps(t, db)
	svc := project.NewService(db)
	ctx := context.Background()

	p, err := svc.CreateProject(ctx, 1, "P", "p", 7)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := svc.CreateRevision(ctx, p.ID, validSpec(t, "V1", "v1"), 7); err != nil {
		t.Fatalf("create revision: %v", err)
	}

	// The project's head references the revision (SET NULL); the revision
	// references the project (CASCADE). Deleting the project must remove its
	// revisions rather than fail on the head reference.
	if err := db.Delete(&project.Project{}, p.ID).Error; err != nil {
		t.Fatalf("delete project: %v", err)
	}
	var revs int64
	db.Model(&project.Revision{}).Where("project_id = ?", p.ID).Count(&revs)
	if revs != 0 {
		t.Errorf("revisions after project delete = %d, want 0 (CASCADE)", revs)
	}
}

// TestRevisionTriggerBlocksRawUpdate is the trigger's reason to exist: a raw
// UPDATE that bypasses the Go BeforeUpdate hook (exec SQL directly, as
// Session{SkipHooks:true} or a hand-written statement would) must still be
// rejected at the database. Asserting only that the service leaves the row
// alone would pass against no enforcement at all.
func TestRevisionTriggerBlocksRawUpdate(t *testing.T) {
	db := dbtest.DB(t)
	seedFKDeps(t, db)
	svc := project.NewService(db)
	ctx := context.Background()

	p, err := svc.CreateProject(ctx, 1, "P", "p", 7)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	rev, err := svc.CreateRevision(ctx, p.ID, validSpec(t, "V1", "v1"), 7)
	if err != nil {
		t.Fatalf("create revision: %v", err)
	}

	err = db.Exec("UPDATE project_revisions SET spec_hash = ? WHERE id = ?", "tampered", rev.ID).Error
	if err == nil {
		t.Fatal("a raw UPDATE of a committed revision must be rejected by the trigger")
	}
	if !strings.Contains(err.Error(), "append-only") {
		t.Errorf("trigger error = %v, want it to mention append-only", err)
	}

	// The bytes are unchanged.
	reloaded, ok, err := svc.Revision(ctx, rev.ID)
	if err != nil || !ok {
		t.Fatalf("reload: ok=%v err=%v", ok, err)
	}
	if reloaded.SpecHash != rev.SpecHash {
		t.Error("revision hash changed despite the append-only trigger")
	}
}

// TestHeadGuardCatchesDanglingPointerWhenFKAbsent covers the defensive
// ErrCorruptLineage path in Head. The projects.head_revision_id SET NULL FK
// makes a dangling head unreachable in the real schema, so to reach the guard
// at all the test first drops that one constraint, then deletes the head
// revision to create the dangling pointer the FK would otherwise have nulled.
// This keeps the guard — belt to the FK's braces — under test.
func TestHeadGuardCatchesDanglingPointerWhenFKAbsent(t *testing.T) {
	db := dbtest.DB(t)
	seedFKDeps(t, db)
	svc := project.NewService(db)
	ctx := context.Background()

	p, err := svc.CreateProject(ctx, 1, "P", "p", 7)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	rev, err := svc.CreateRevision(ctx, p.ID, validSpec(t, "V1", "v1"), 7)
	if err != nil {
		t.Fatalf("create revision: %v", err)
	}

	if err := db.Exec(`ALTER TABLE projects DROP CONSTRAINT fk_projects_head`).Error; err != nil {
		t.Fatalf("drop head FK: %v", err)
	}
	if err := db.Delete(&project.Revision{}, rev.ID).Error; err != nil {
		t.Fatalf("delete revision: %v", err)
	}

	if _, ok, err := svc.Head(ctx, p.ID); !errors.Is(err, project.ErrCorruptLineage) || ok {
		t.Errorf("Head over dangling pointer = (ok=%v, err=%v), want ErrCorruptLineage", ok, err)
	}
}
