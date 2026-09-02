package project_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gombit-dev/gombit-forge/controlplane/internal/project"
	"github.com/gombit-dev/gombit-forge/internal/spec"
)

// TestAddPageEachType is the #50 acceptance criterion: a page of every MVP type
// can be created — the resource types bound to a resource, the dashboard without
// one — and each is serialized into the head spec.
func TestAddPageEachType(t *testing.T) {
	svc, projectID, resID := projectWithResource(t)
	ctx := context.Background()

	cases := []struct {
		label string
		typ   spec.PageType
		res   spec.ID
	}{
		{"Orders", spec.PageResourceTable, resID},
		{"New Order", spec.PageResourceForm, resID},
		{"Order", spec.PageResourceDetail, resID},
		{"Home", spec.PageDashboard, ""},
	}
	for _, c := range cases {
		res, err := svc.AddPage(ctx, projectID, project.PageInput{Label: c.label, Type: c.typ, Resource: c.res}, 7)
		if err != nil {
			t.Fatalf("add %s page: %v", c.typ, err)
		}
		if res.Outcome != project.OutcomeCommitted {
			t.Fatalf("add %s page outcome = %s, want committed", c.typ, res.Outcome)
		}
	}

	pages := headSpec(t, svc, projectID).Pages
	if len(pages) != len(cases) {
		t.Fatalf("want %d pages serialized, got %d", len(cases), len(pages))
	}
	for i, c := range cases {
		p := pages[i]
		if p.Label != c.label || p.Type != c.typ {
			t.Errorf("page[%d] = %s/%s, want %s/%s", i, p.Label, p.Type, c.label, c.typ)
		}
		if p.Resource != c.res {
			t.Errorf("page[%d] resource = %q, want %q", i, p.Resource, c.res)
		}
		if !spec.IsSlug(p.Slug) {
			t.Errorf("page[%d] slug %q is not a valid slug", i, p.Slug)
		}
	}
}

// TestAddPageDerivesUniqueSlugs: two pages with the same label get distinct,
// valid slugs so page URLs never collide.
func TestAddPageDerivesUniqueSlugs(t *testing.T) {
	svc, projectID, resID := projectWithResource(t)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if _, err := svc.AddPage(ctx, projectID, project.PageInput{Label: "Orders", Type: spec.PageResourceTable, Resource: resID}, 7); err != nil {
			t.Fatalf("add page %d: %v", i, err)
		}
	}
	pages := headSpec(t, svc, projectID).Pages
	if len(pages) != 2 {
		t.Fatalf("want 2 pages, got %d", len(pages))
	}
	if pages[0].Slug == pages[1].Slug {
		t.Errorf("page slugs must be unique; both are %q", pages[0].Slug)
	}
}

// TestAddPageRejectsMalformedInput: input the request alone proves wrong is
// rejected with ErrInvalidPageEdit before any spec is built.
func TestAddPageRejectsMalformedInput(t *testing.T) {
	svc, projectID, resID := projectWithResource(t)
	ctx := context.Background()

	cases := map[string]project.PageInput{
		"empty label":         {Label: "  ", Type: spec.PageResourceTable, Resource: resID},
		"unknown type":        {Label: "X", Type: spec.PageType("gallery"), Resource: resID},
		"resource w/o target": {Label: "X", Type: spec.PageResourceTable},
		"dashboard w/ target": {Label: "X", Type: spec.PageDashboard, Resource: resID},
	}
	for name, in := range cases {
		if _, err := svc.AddPage(ctx, projectID, in, 7); !errors.Is(err, project.ErrInvalidPageEdit) {
			t.Errorf("%s: err = %v, want ErrInvalidPageEdit", name, err)
		}
	}
}

// TestAddPageDanglingResourceIsInvalid: a well-formed request binding a resource
// that does not exist is caught by the spec validator as invalid_spec (not a
// committed, unbuildable revision).
func TestAddPageDanglingResourceIsInvalid(t *testing.T) {
	svc, projectID, _ := projectWithResource(t)

	res, err := svc.AddPage(context.Background(), projectID,
		project.PageInput{Label: "Ghosts", Type: spec.PageResourceTable, Resource: spec.MustNewID(spec.KindResource)}, 7)
	if err != nil {
		t.Fatalf("add page: %v", err)
	}
	if res.Outcome != project.OutcomeInvalidSpec {
		t.Fatalf("dangling resource binding outcome = %s, want invalid_spec", res.Outcome)
	}
	if len(res.Diagnostics) == 0 {
		t.Error("invalid_spec must carry diagnostics keyed to the offending page")
	}
}

// TestDeletePageIsNeutral: a page carries no extension ABI, so removing it
// commits as a neutral transition rather than being flagged breaking.
func TestDeletePageIsNeutral(t *testing.T) {
	svc, projectID, resID := projectWithResource(t)
	ctx := context.Background()

	if _, err := svc.AddPage(ctx, projectID, project.PageInput{Label: "Orders", Type: spec.PageResourceTable, Resource: resID}, 7); err != nil {
		t.Fatalf("add: %v", err)
	}
	pageID := headSpec(t, svc, projectID).Pages[0].ID

	res, err := svc.DeletePage(ctx, projectID, pageID, 7)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if res.Outcome != project.OutcomeCommitted || res.Class != "neutral" {
		t.Fatalf("delete page outcome=%s class=%s, want committed/neutral", res.Outcome, res.Class)
	}
	if len(headSpec(t, svc, projectID).Pages) != 0 {
		t.Error("delete must remove the page from the head spec")
	}
}

func TestPageEditErrors(t *testing.T) {
	svc, projectID, _ := projectWithResource(t)
	ctx := context.Background()

	if _, err := svc.DeletePage(ctx, projectID, spec.MustNewID(spec.KindPage), 7); !errors.Is(err, project.ErrPageNotFound) {
		t.Errorf("unknown page = %v, want ErrPageNotFound", err)
	}
}
