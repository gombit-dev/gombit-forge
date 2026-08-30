package project_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gombit-dev/gombit-forge/controlplane/internal/dbtest"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/project"
	"github.com/gombit-dev/gombit-forge/internal/spec"
)

// validSpec builds a minimal valid one-resource ProjectSpec. name/slug let a
// test vary the content so hashes differ when they should.
func validSpec(t *testing.T, name, slug string) *spec.ProjectSpec {
	t.Helper()
	customer := spec.MustNewID(spec.KindResource)
	s := &spec.ProjectSpec{
		SpecVersion: spec.SpecVersion,
		Project:     spec.Project{ID: spec.MustNewID(spec.KindProject), Name: name, Slug: slug},
		Database:    spec.Database{Driver: spec.DriverPostgres},
		Auth:        spec.Auth{Mode: spec.AuthCookie},
		Resources: []*spec.Resource{
			{
				ID: customer, Label: "Customer", LabelPlural: "Customers",
				CodeName: "Customer", StorageName: "customers",
				Behavior: spec.ResourceBehavior{CreateEnabled: true, AdminVisible: true},
				Fields: []*spec.Field{
					{ID: spec.MustNewID(spec.KindField), Label: "Email", Type: spec.TypeString,
						CodeName: "Email", StorageName: "email", Required: true},
				},
			},
		},
	}
	if d := spec.Validate(s); d != nil {
		t.Fatalf("test spec invalid: %s", d.Error())
	}
	return s
}

func TestCreateProjectHasNoHead(t *testing.T) {
	db := dbtest.DB(t)
	svc := project.NewService(db)

	p, err := svc.CreateProject(context.Background(), 1, "Acme CRM", "acme-crm", 7)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if p.HeadRevisionID != nil {
		t.Errorf("new project head = %v, want nil", p.HeadRevisionID)
	}

	_, ok, err := svc.Head(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if ok {
		t.Error("a project with no revisions must report no head")
	}
}

func TestFirstRevisionAnchorsSpecAndAdvancesHead(t *testing.T) {
	db := dbtest.DB(t)
	svc := project.NewService(db)
	ctx := context.Background()

	p, err := svc.CreateProject(ctx, 1, "Acme", "acme", 7)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	sp := validSpec(t, "Acme CRM", "acme-crm")

	rev, err := svc.CreateRevision(ctx, p.ID, sp, 7)
	if err != nil {
		t.Fatalf("create revision: %v", err)
	}

	// The stored bytes and hash are the compiler's own.
	wantJSON, _ := spec.Marshal(sp)
	wantHash, _ := spec.Hash(sp)
	if rev.SpecJSON != string(wantJSON) {
		t.Error("revision spec_json is not the canonical encoding")
	}
	if rev.SpecHash != wantHash {
		t.Errorf("revision spec_hash = %s, want %s", rev.SpecHash, wantHash)
	}
	if rev.SpecVersion != spec.SpecVersion {
		t.Errorf("revision spec_version = %d, want %d", rev.SpecVersion, spec.SpecVersion)
	}
	// First revision has no parent.
	if rev.ParentRevisionID != nil {
		t.Errorf("first revision parent = %v, want nil", rev.ParentRevisionID)
	}

	// Head advanced to this revision.
	head, ok, err := svc.Head(ctx, p.ID)
	if err != nil || !ok {
		t.Fatalf("head after first revision: ok=%v err=%v", ok, err)
	}
	if head.ID != rev.ID {
		t.Errorf("head = %d, want %d", head.ID, rev.ID)
	}
}

func TestSecondRevisionLinksToFirstAndFirstIsImmutable(t *testing.T) {
	db := dbtest.DB(t)
	svc := project.NewService(db)
	ctx := context.Background()

	p, err := svc.CreateProject(ctx, 1, "Acme", "acme", 7)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	first, err := svc.CreateRevision(ctx, p.ID, validSpec(t, "V1", "v1"), 7)
	if err != nil {
		t.Fatalf("first revision: %v", err)
	}
	second, err := svc.CreateRevision(ctx, p.ID, validSpec(t, "V2", "v2"), 9)
	if err != nil {
		t.Fatalf("second revision: %v", err)
	}

	// Lineage: second descends from first; head is second.
	if second.ParentRevisionID == nil || *second.ParentRevisionID != first.ID {
		t.Errorf("second parent = %v, want %d", second.ParentRevisionID, first.ID)
	}
	head, _, err := svc.Head(ctx, p.ID)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if head.ID != second.ID {
		t.Errorf("head = %d, want %d (second)", head.ID, second.ID)
	}

	// Immutability: the first revision is untouched by later writes.
	reloaded, ok, err := svc.Revision(ctx, first.ID)
	if err != nil || !ok {
		t.Fatalf("reload first: ok=%v err=%v", ok, err)
	}
	if reloaded.SpecHash != first.SpecHash || reloaded.SpecJSON != first.SpecJSON ||
		reloaded.ParentRevisionID != nil || reloaded.ProjectID != first.ProjectID {
		t.Error("first revision changed after a later revision was created; revisions must be immutable")
	}
}

// TestSameSpecSameHash is the determinism contract crossing the module boundary:
// the same spec produces the same canonical bytes and hash in two projects.
func TestSameSpecSameHash(t *testing.T) {
	db := dbtest.DB(t)
	svc := project.NewService(db)
	ctx := context.Background()

	p1, _ := svc.CreateProject(ctx, 1, "One", "one", 7)
	p2, _ := svc.CreateProject(ctx, 1, "Two", "two", 7)

	// The *same* spec (same stable IDs), stored into two projects. Identity is
	// the stable ID, so reusing the value is what "the same spec" means.
	sp := validSpec(t, "Same", "same")
	r1, err := svc.CreateRevision(ctx, p1.ID, sp, 7)
	if err != nil {
		t.Fatalf("revision 1: %v", err)
	}
	r2, err := svc.CreateRevision(ctx, p2.ID, sp, 7)
	if err != nil {
		t.Fatalf("revision 2: %v", err)
	}
	if r1.SpecHash != r2.SpecHash {
		t.Errorf("same spec, different hash: %s vs %s", r1.SpecHash, r2.SpecHash)
	}
	if r1.SpecJSON != r2.SpecJSON {
		t.Error("same spec, different canonical bytes")
	}
}

func TestCreateRevisionRejectsInvalidSpec(t *testing.T) {
	db := dbtest.DB(t)
	svc := project.NewService(db)
	ctx := context.Background()

	p, err := svc.CreateProject(ctx, 1, "Acme", "acme", 7)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	bad := validSpec(t, "Acme", "acme")
	bad.Project.Slug = "Not A Slug" // fails slug validation

	if _, err := svc.CreateRevision(ctx, p.ID, bad, 7); !errors.Is(err, project.ErrInvalidSpec) {
		t.Errorf("invalid spec err = %v, want ErrInvalidSpec", err)
	}
	// No revision was created, and head is still unset.
	if _, ok, _ := svc.Head(ctx, p.ID); ok {
		t.Error("an invalid candidate must not create a revision or advance head")
	}
}

func TestCreateRevisionUnknownProject(t *testing.T) {
	db := dbtest.DB(t)
	svc := project.NewService(db)

	if _, err := svc.CreateRevision(context.Background(), 999999, validSpec(t, "X", "x"), 7); !errors.Is(err, project.ErrProjectNotFound) {
		t.Errorf("unknown project err = %v, want ErrProjectNotFound", err)
	}
}
