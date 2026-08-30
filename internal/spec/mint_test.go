package spec

import "testing"

func TestMintFreshSymbol(t *testing.T) {
	l := NewLedger()
	ns := FieldNamespace("res_A")

	sym, err := Mint(l, ns, "Email", "fld_A", nil)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if sym != "Email" {
		t.Errorf("mint = %q, want Email", sym)
	}
	// Frozen against the stable ID (AC: minted symbol frozen against the ID).
	if owner, ok := l.OwnerOf(ns, "Email"); !ok || owner != "fld_A" {
		t.Errorf("OwnerOf(Email) = (%q,%v), want (fld_A,true)", owner, ok)
	}
}

// TestMintDisambiguatesTakenSymbol is the AC: a new entity whose natural symbol
// is taken gets the deterministic next suffix — Email, Email2, Email3.
func TestMintDisambiguatesTakenSymbol(t *testing.T) {
	l := NewLedger()
	ns := FieldNamespace("res_A")

	want := []string{"Email", "Email2", "Email3"}
	ids := []ID{"fld_A", "fld_B", "fld_C"}
	for i, id := range ids {
		sym, err := Mint(l, ns, "Email", id, nil)
		if err != nil {
			t.Fatalf("mint %d: %v", i, err)
		}
		if sym != want[i] {
			t.Errorf("mint %d = %q, want %q", i, sym, want[i])
		}
		if owner, _ := l.OwnerOf(ns, sym); owner != id {
			t.Errorf("mint %d owner = %q, want %q", i, owner, id)
		}
	}
}

// TestMintScenarioB is ADR-001 §50 verbatim: fld_A holds code Email (its label
// was later changed to "Primary contact", but a relabel does not re-mint — the
// ledger entry is untouched). A new field labeled "Email" then mints Email2, so
// no collision enters the project.
func TestMintScenarioB(t *testing.T) {
	l := NewLedger()
	ns := FieldNamespace("res_A")

	a, err := Mint(l, ns, "Email", "fld_A", nil)
	if err != nil || a != "Email" {
		t.Fatalf("fld_A mint = (%q,%v), want Email", a, err)
	}
	// fld_A is relabeled to "Primary contact" — no Mint call, ledger unchanged.
	// A second field is created with the now-duplicate visible label "Email".
	b, err := Mint(l, ns, "Email", "fld_B", nil)
	if err != nil {
		t.Fatalf("fld_B mint: %v", err)
	}
	if b != "Email2" {
		t.Errorf("fld_B mint = %q, want Email2 (§50)", b)
	}
}

// TestMintSkipsTombstoned is §10: a tombstoned symbol is reserved forever, so a
// new entity does not reuse it even though it is not live.
func TestMintSkipsTombstoned(t *testing.T) {
	l := NewLedger()
	ns := FieldNamespace("res_A")

	if _, err := Mint(l, ns, "Email", "fld_A", nil); err != nil {
		t.Fatal(err)
	}
	if err := l.Tombstone(ns, "Email"); err != nil {
		t.Fatal(err)
	}
	// Email is tombstoned (not live); a fresh mint must still not reuse it.
	sym, err := Mint(l, ns, "Email", "fld_B", nil)
	if err != nil {
		t.Fatal(err)
	}
	if sym != "Email2" {
		t.Errorf("mint over a tombstone = %q, want Email2 (tombstones are never reused, §10)", sym)
	}
}

// TestMintSkipsReserved: a candidate that collides with a generated/framework
// symbol is disambiguated past, not minted. "table name" normalizes to
// TableName, which is reserved, so the symbol is TableName2.
func TestMintSkipsReserved(t *testing.T) {
	if _, reserved := ReservedModelSymbol("TableName"); !reserved {
		t.Fatal("precondition: TableName must be reserved")
	}
	l := NewLedger()
	ns := FieldNamespace("res_A")

	sym, err := Mint(l, ns, "table name", "fld_A", IsReservedCodeName)
	if err != nil {
		t.Fatal(err)
	}
	if sym != "TableName2" {
		t.Errorf("mint of a reserved candidate = %q, want TableName2", sym)
	}
}

// TestMintFallsBackForUnrepresentableLabel: a label Normalize cannot represent
// (fails closed) mints a kind-derived symbol rather than erroring or producing a
// fragment, and the fallback disambiguates like any other base.
func TestMintFallsBackForUnrepresentableLabel(t *testing.T) {
	l := NewLedger()
	ns := FieldNamespace("res_A")

	first, err := Mint(l, ns, "żółć", "fld_A", nil) // Latin Extended-A: Normalize ok=false
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if first != "Field" {
		t.Errorf("fallback mint = %q, want Field (kind-derived)", first)
	}
	second, err := Mint(l, ns, "😀", "fld_B", nil)
	if err != nil {
		t.Fatal(err)
	}
	if second != "Field2" {
		t.Errorf("second fallback mint = %q, want Field2", second)
	}
}

// TestMintNamespaceIndependence: the same label in two resources' field
// namespaces both mint Email, because uniqueness is per namespace (§7).
func TestMintNamespaceIndependence(t *testing.T) {
	l := NewLedger()
	a := FieldNamespace("res_A")
	b := FieldNamespace("res_B")

	symA, err := Mint(l, a, "Email", "fld_A", nil)
	if err != nil {
		t.Fatal(err)
	}
	symB, err := Mint(l, b, "Email", "fld_B", nil)
	if err != nil {
		t.Fatal(err)
	}
	if symA != "Email" || symB != "Email" {
		t.Errorf("mints = (%q,%q), want both Email (independent namespaces)", symA, symB)
	}
}

func TestMintNilLedger(t *testing.T) {
	if _, err := Mint(nil, FieldNamespace("res_A"), "Email", "fld_A", nil); err == nil {
		t.Error("Mint with a nil ledger must error")
	}
}
