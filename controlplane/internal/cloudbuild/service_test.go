package cloudbuild_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gombit-dev/gombit-forge/controlplane/internal/cloudbuild"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/dbtest"
)

// TestServiceLifecycle drives one job from enqueue through claim, Cloud-state
// reflection, and a terminal outcome, plus the queue ordering and the
// running-only transition guard. The store has no FKs (a job is an audit-style
// record), so no project/revision rows need seeding.
func TestServiceLifecycle(t *testing.T) {
	db := dbtest.DB(t)
	svc := cloudbuild.NewService(db)
	ctx := context.Background()

	// A project with no Cloud link can't enqueue.
	if _, err := svc.Enqueue(ctx, 1, 10, 100, ""); !errors.Is(err, cloudbuild.ErrNoCloudProject) {
		t.Fatalf("enqueue without a cloud project = %v, want ErrNoCloudProject", err)
	}

	job, err := svc.Enqueue(ctx, 1, 10, 100, "prj_cloud")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != cloudbuild.StatusQueued || job.CloudProjectID != "prj_cloud" {
		t.Fatalf("enqueued job = %+v", job)
	}

	if got, ok, err := svc.Get(ctx, job.ID); err != nil || !ok || got.ID != job.ID {
		t.Fatalf("get = %+v, %v, %v", got, ok, err)
	}

	// A second job for the same project; listed newest-first.
	job2, err := svc.Enqueue(ctx, 1, 11, 100, "prj_cloud")
	if err != nil {
		t.Fatal(err)
	}
	list, err := svc.ListForProject(ctx, 1)
	if err != nil || len(list) != 2 || list[0].ID != job2.ID {
		t.Fatalf("list = %+v, %v (want newest first)", list, err)
	}

	// Guard: a queued (never-claimed) job can't be marked terminal.
	if err := svc.MarkSucceeded(ctx, job.ID, "succeeded", "sha256:x"); err == nil {
		t.Fatal("marking a queued job succeeded should fail (not running)")
	}

	// Claim takes the oldest queued job and runs it.
	claimed, ok, err := svc.Claim(ctx)
	if err != nil || !ok || claimed.ID != job.ID || claimed.Status != cloudbuild.StatusRunning {
		t.Fatalf("claim = %+v, %v, %v", claimed, ok, err)
	}

	// Reflect the Cloud build id + state on the running job, then settle it.
	if err := svc.SetCloudBuild(ctx, claimed.ID, "bld_1"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReflectCloudStatus(ctx, claimed.ID, "building"); err != nil {
		t.Fatal(err)
	}
	if err := svc.MarkSucceeded(ctx, claimed.ID, "succeeded", "sha256:cafe"); err != nil {
		t.Fatal(err)
	}

	final, _, _ := svc.Get(ctx, claimed.ID)
	if final.Status != cloudbuild.StatusSucceeded || final.CloudBuildID != "bld_1" ||
		final.CloudStatus != "succeeded" || final.ArtifactDigest != "sha256:cafe" {
		t.Fatalf("final job = %+v", final)
	}

	// Guard: an already-terminal job can't be re-marked (no late/duplicate clobber).
	if err := svc.MarkFailed(ctx, claimed.ID, "failed", "boom"); err == nil {
		t.Fatal("re-marking a terminal job should fail")
	}

	// The second job is claimed next; then the queue is empty.
	if _, ok, _ := svc.Claim(ctx); !ok {
		t.Fatal("expected to claim the second queued job")
	}
	if _, ok, err := svc.Claim(ctx); err != nil || ok {
		t.Fatalf("expected an empty queue, got ok=%v err=%v", ok, err)
	}
}

// TestLeaseExpiresAbandonedBuildJobs: a build job whose worker stopped renewing
// its lease is failed with AbandonedReason; one still renewed is left alone.
func TestLeaseExpiresAbandonedBuildJobs(t *testing.T) {
	db := dbtest.DB(t)
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	svc := cloudbuild.NewService(db, cloudbuild.WithLease(2*time.Minute), cloudbuild.WithClock(func() time.Time { return now }))
	ctx := context.Background()

	abandoned, _ := svc.Enqueue(ctx, 1, 10, 100, "prj_cloud")
	live, _ := svc.Enqueue(ctx, 1, 11, 100, "prj_cloud")
	if _, _, err := svc.Claim(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Claim(ctx); err != nil {
		t.Fatal(err)
	}
	now = now.Add(3 * time.Minute)
	if err := svc.Renew(ctx, live.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := svc.ExpireAbandoned(ctx); err != nil || n != 1 {
		t.Fatalf("ExpireAbandoned = %d, %v; want 1", n, err)
	}
	if got, _, _ := svc.Get(ctx, abandoned.ID); got.Status != cloudbuild.StatusFailed || got.Error != cloudbuild.AbandonedReason {
		t.Fatalf("abandoned job = %+v, want failed with the abandoned reason", got)
	}
	if got, _, _ := svc.Get(ctx, live.ID); got.Status != cloudbuild.StatusRunning {
		t.Fatalf("renewed job status = %q, want running", got.Status)
	}
	if err := svc.SetCloudBuild(ctx, abandoned.ID, "bld_late"); err == nil {
		t.Error("a late worker updated the abandoned job")
	}
}
