package audit_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/gombit-dev/gombit-forge/controlplane/internal/audit"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/dbtest"
)

// TestVocabularyIsForgeScoped is the "runtime deploy/secret events are not
// fabricated here" acceptance criterion (#40, ADR-005), and it needs no
// database so it runs in CI's -short suite. The audit vocabulary must record
// only Forge's own actions; the runtime lifecycle of a build or deployment, and
// secret changes, are Gombit Cloud's audit trail (gombit-cloud RFC §62). If a
// well-meaning change adds `deployment.started` or `secret.changed` back to the
// Forge set, this fails.
func TestVocabularyIsForgeScoped(t *testing.T) {
	for _, a := range audit.Actions {
		s := string(a)
		switch {
		case strings.HasPrefix(s, "deployment."):
			t.Errorf("%q is a Cloud runtime action; Forge records deploy.triggered, not the lifecycle (ADR-005)", a)
		case strings.Contains(s, "secret"):
			t.Errorf("%q is a Cloud action; the secrets store moved to Cloud (ADR-005, #41)", a)
		}
	}
}

// TestNoValueChannelForSecrets guards §23's "no secret values in audit data" at
// the level it is actually enforced: the recordable vocabulary has no
// secret-oriented action, and Event carries only a typed reference, never a
// value. This asserts the vocabulary half; the structural half is that Event
// exposes no free-form value field for a caller to fill (see the Event type).
func TestNoValueChannelForSecrets(t *testing.T) {
	for _, a := range audit.Actions {
		if strings.Contains(strings.ToLower(string(a)), "secret") {
			t.Fatalf("a secret-oriented action %q would invite recording a value; none must exist", a)
		}
	}
}

// TestListReturnsOneOrgNewestFirst: List is scoped to a single organization and
// returns newest first, with id breaking ties so equal timestamps stay
// deterministic. Another org's events must not leak in.
func TestListReturnsOneOrgNewestFirst(t *testing.T) {
	db := dbtest.DB(t)
	ctx := context.Background()
	org1, org2 := uint(1), uint(2)

	for _, target := range []string{"p1", "p2", "p3"} {
		if err := audit.Record(ctx, db, audit.Event{
			OrganizationID: &org1, Action: audit.ActionProjectCreated,
			TargetType: "project", TargetID: target,
		}); err != nil {
			t.Fatalf("record %s: %v", target, err)
		}
	}
	if err := audit.Record(ctx, db, audit.Event{
		OrganizationID: &org2, Action: audit.ActionProjectCreated,
		TargetType: "project", TargetID: "other",
	}); err != nil {
		t.Fatalf("record other-org: %v", err)
	}

	events, err := audit.List(ctx, db, audit.Filter{OrganizationID: org1})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("org1 events = %d, want 3 (org2's must not leak)", len(events))
	}
	// Newest first: p3, p2, p1 (insertion order reversed).
	got := []string{events[0].TargetID, events[1].TargetID, events[2].TargetID}
	want := []string{"p3", "p2", "p1"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("order[%d] = %s, want %s (newest first)", i, got[i], want[i])
		}
	}
}

// TestListFiltersByActionAndActor: the optional narrowing filters select the
// right subset without affecting org scoping.
func TestListFiltersByActionAndActor(t *testing.T) {
	db := dbtest.DB(t)
	ctx := context.Background()
	o, alice, bob := uint(1), uint(7), uint(9)

	if err := audit.Record(ctx, db, audit.Event{OrganizationID: &o, ActorUserID: &alice, Action: audit.ActionProjectCreated, TargetType: "project", TargetID: "1"}); err != nil {
		t.Fatal(err)
	}
	if err := audit.Record(ctx, db, audit.Event{OrganizationID: &o, ActorUserID: &bob, Action: audit.ActionMemberInvited, TargetType: "invitation", TargetID: "2"}); err != nil {
		t.Fatal(err)
	}

	byAction, err := audit.List(ctx, db, audit.Filter{OrganizationID: o, Action: audit.ActionMemberInvited})
	if err != nil {
		t.Fatal(err)
	}
	if len(byAction) != 1 || byAction[0].Action != audit.ActionMemberInvited {
		t.Fatalf("action filter = %+v, want one member.invited", byAction)
	}

	byActor, err := audit.List(ctx, db, audit.Filter{OrganizationID: o, ActorUserID: &alice})
	if err != nil {
		t.Fatal(err)
	}
	if len(byActor) != 1 || byActor[0].Action != audit.ActionProjectCreated {
		t.Fatalf("actor filter = %+v, want alice's project.created", byActor)
	}
}

// TestListPagesAndCaps: limit/offset page through results, and an over-large
// limit is capped rather than trusted.
func TestListPagesAndCaps(t *testing.T) {
	db := dbtest.DB(t)
	ctx := context.Background()
	o := uint(1)
	for i := 0; i < 5; i++ {
		if err := audit.Record(ctx, db, audit.Event{OrganizationID: &o, Action: audit.ActionProjectCreated, TargetType: "project", TargetID: strconv.Itoa(i)}); err != nil {
			t.Fatal(err)
		}
	}

	first, err := audit.List(ctx, db, audit.Filter{OrganizationID: o, Limit: 2})
	if err != nil || len(first) != 2 {
		t.Fatalf("first page = %d (err %v), want 2", len(first), err)
	}
	next, err := audit.List(ctx, db, audit.Filter{OrganizationID: o, Limit: 2, Offset: 2})
	if err != nil || len(next) != 2 {
		t.Fatalf("second page = %d (err %v), want 2", len(next), err)
	}
	if first[0].ID == next[0].ID {
		t.Error("offset paging returned an overlapping row")
	}

	all, err := audit.List(ctx, db, audit.Filter{OrganizationID: o, Limit: 10_000})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 {
		t.Fatalf("capped list = %d, want all 5 (cap does not drop rows below the cap)", len(all))
	}
}
