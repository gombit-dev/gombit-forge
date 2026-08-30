package deploy_test

// DB-backed checks that the models migrate and their uniqueness constraints
// hold. Docker-gated via dbtest (skips under -short); the state-machine logic is
// covered hermetically in build_test.go and runs in CI.

import (
	"errors"
	"testing"

	"gorm.io/gorm"

	"github.com/gombit-dev/gombit-forge/controlplane/internal/dbtest"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/deploy"
)

func TestEnvironmentUniquePerProjectAndType(t *testing.T) {
	db := dbtest.DB(t)

	if err := db.Create(&deploy.Environment{ProjectID: 1, Type: deploy.EnvironmentPreview}).Error; err != nil {
		t.Fatalf("create preview: %v", err)
	}
	// Same project + same type is a conflict.
	err := db.Create(&deploy.Environment{ProjectID: 1, Type: deploy.EnvironmentPreview}).Error
	if !errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Errorf("duplicate (project, preview) err = %v, want ErrDuplicatedKey", err)
	}
	// Same project, different type is fine (a project has one of each).
	if err := db.Create(&deploy.Environment{ProjectID: 1, Type: deploy.EnvironmentProduction}).Error; err != nil {
		t.Errorf("create production for same project: %v", err)
	}
	// Same type, different project is fine.
	if err := db.Create(&deploy.Environment{ProjectID: 2, Type: deploy.EnvironmentPreview}).Error; err != nil {
		t.Errorf("create preview for another project: %v", err)
	}
}

func TestDomainHostnameUnique(t *testing.T) {
	db := dbtest.DB(t)

	if err := db.Create(&deploy.Domain{EnvironmentID: 1, Hostname: "app.example.test"}).Error; err != nil {
		t.Fatalf("create domain: %v", err)
	}
	err := db.Create(&deploy.Domain{EnvironmentID: 2, Hostname: "app.example.test"}).Error
	if !errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Errorf("duplicate hostname err = %v, want ErrDuplicatedKey", err)
	}
}

// TestBuildAndDeploymentReferences confirms the reference columns round-trip: a
// deployment records the environment, the exact revision, and the build that
// produced it (§12).
func TestBuildAndDeploymentReferences(t *testing.T) {
	db := dbtest.DB(t)

	build := deploy.Build{
		ProjectID: 1, RevisionID: 42,
		ForgeVersion: "v0.0.0", GombitVersion: "v0.1.7",
		State: deploy.BuildQueued,
	}
	if err := db.Create(&build).Error; err != nil {
		t.Fatalf("create build: %v", err)
	}
	env := deploy.Environment{ProjectID: 1, Type: deploy.EnvironmentProduction}
	if err := db.Create(&env).Error; err != nil {
		t.Fatalf("create env: %v", err)
	}

	dep := deploy.Deployment{
		EnvironmentID: env.ID, RevisionID: build.RevisionID, BuildID: build.ID, CreatedBy: 7,
	}
	if err := db.Create(&dep).Error; err != nil {
		t.Fatalf("create deployment: %v", err)
	}

	var got deploy.Deployment
	if err := db.First(&got, dep.ID).Error; err != nil {
		t.Fatalf("reload deployment: %v", err)
	}
	if got.EnvironmentID != env.ID || got.BuildID != build.ID || got.RevisionID != 42 {
		t.Errorf("deployment references = %+v, want env %d build %d revision 42", got, env.ID, build.ID)
	}
}
