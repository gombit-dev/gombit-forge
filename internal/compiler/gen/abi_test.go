package gen

import (
	"strings"
	"testing"

	"github.com/gombit-dev/gombit-forge/internal/spec"
)

func TestExtensionABINilGraph(t *testing.T) {
	if _, err := ExtensionABI(nil); err == nil {
		t.Error("ExtensionABI(nil) must error")
	}
}

// TestExtensionABICapturesSurface checks the ABI records the frozen accessor
// symbol and Go type for each field (independent of label/storage) and the
// enabled hook set.
func TestExtensionABICapturesSurface(t *testing.T) {
	g, ids := buildGraph(t)
	g.Resources[0].Spec.Hooks = []*spec.Hook{
		{ID: spec.MustNewID(spec.KindHook), Event: spec.HookAfterCreate},
	}

	abi, err := ExtensionABI(g)
	if err != nil {
		t.Fatalf("ExtensionABI: %v", err)
	}

	// Resources are sorted by stable ID.
	for i := 1; i < len(abi.Resources); i++ {
		if abi.Resources[i-1].ID > abi.Resources[i].ID {
			t.Fatalf("resources not sorted by id: %v", abi.Resources)
		}
	}

	cust := resourceABIByID(t, abi, string(ids["customer"]))
	if cust.Type != "Customer" {
		t.Errorf("resource Type: got %q want Customer", cust.Type)
	}
	// Email's code symbol is Email though its storage is contact_email: the ABI
	// records the symbol, not the column.
	email := cust.Fields[string(ids["email"])]
	if email.Symbol != "Email" || email.Type != "string" {
		t.Errorf("email field ABI: got %+v want {Email string}", email)
	}
	if _, ok := cust.Hooks["after_create"]; !ok {
		t.Errorf("expected an after_create hook in the ABI; got %v", cust.Hooks)
	}

	inv := resourceABIByID(t, abi, string(ids["invoice"]))
	// belongs_to appears under its foreign-key symbol and type.
	fk := inv.Fields[string(ids["custFK"])]
	if fk.Symbol != "CustomerID" || fk.Type != "uint" {
		t.Errorf("belongs_to field ABI: got %+v want {CustomerID uint}", fk)
	}
}

func TestABISumStableAndSensitive(t *testing.T) {
	g, ids := buildGraph(t)
	abi, _ := ExtensionABI(g)
	sum, err := abi.Sum()
	if err != nil {
		t.Fatalf("Sum: %v", err)
	}
	if len(sum) != 64 {
		t.Errorf("sum must be 64 hex chars; got %d", len(sum))
	}
	if again, _ := abi.Sum(); again != sum {
		t.Errorf("Sum not deterministic: %q vs %q", sum, again)
	}

	// A label change on the graph does not change the sum...
	g.Resources[0].Spec.Label = "Clients"
	relabeled, _ := ExtensionABI(g)
	if s, _ := relabeled.Sum(); s != sum {
		t.Errorf("relabel changed the fingerprint: %q -> %q", sum, s)
	}

	// ...but a code-symbol change does.
	g.Resources[0].Fields[0].Spec.CodeName = "ContactEmail"
	_ = ids
	renamed, _ := ExtensionABI(g)
	if s, _ := renamed.Sum(); s == sum {
		t.Error("a code-symbol rename must change the fingerprint")
	}
}

func TestClassifyTransitionNeutralOnIdenticalABI(t *testing.T) {
	g, _ := buildGraph(t)
	abi, _ := ExtensionABI(g)
	tr, err := ClassifyTransition(abi, abi)
	if err != nil {
		t.Fatalf("ClassifyTransition: %v", err)
	}
	if tr.Class != ClassNeutral {
		t.Errorf("identical ABIs must be neutral; got %s", tr.Class)
	}
	if tr.Fingerprint == "" {
		t.Error("neutral transition must still carry the fingerprint")
	}
}

func resourceABIByID(t *testing.T, abi ABI, id string) ResourceABI {
	t.Helper()
	for _, r := range abi.Resources {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("resource %s not in ABI", id)
	return ResourceABI{}
}

// TestOptionalTimeIsNullable: an optional date or datetime is a *time.Time in
// the model and the extension ABI, so a blank one is NULL rather than the zero
// time Gombit refuses (and which was never the user's value); a required one
// stays a time.Time. Because the Go type is ABI, toggling required on such a
// field is a breaking transition.
func TestOptionalTimeIsNullable(t *testing.T) {
	g, ids := buildGraph(t)
	before, err := ExtensionABI(g)
	if err != nil {
		t.Fatal(err)
	}
	joined := resourceABIByID(t, before, string(ids["customer"])).Fields[string(ids["joined"])]
	if joined.Type != "*time.Time" {
		t.Fatalf("optional datetime ABI type = %q, want *time.Time", joined.Type)
	}
	files, err := Models(g)
	if err != nil {
		t.Fatal(err)
	}
	if src := modelSource(t, files, "customer"); !strings.Contains(src, "Joined *time.Time") {
		t.Fatalf("customer model lacks `Joined *time.Time`:\n%s", src)
	}

	for _, f := range g.Resources[0].Fields {
		if f.Spec.ID == ids["joined"] {
			f.Spec.Required = true
		}
	}
	after, err := ExtensionABI(g)
	if err != nil {
		t.Fatal(err)
	}
	if got := resourceABIByID(t, after, string(ids["customer"])).Fields[string(ids["joined"])].Type; got != "time.Time" {
		t.Fatalf("required datetime ABI type = %q, want time.Time", got)
	}
	tr, err := ClassifyTransition(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Class != ClassBreaking {
		t.Fatalf("making an optional datetime required = %s, want breaking (its Go type changes)", tr.Class)
	}
}
