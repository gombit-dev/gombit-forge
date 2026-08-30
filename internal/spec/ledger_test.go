package spec

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestLedgerRecordAndQuery(t *testing.T) {
	l := NewLedger()
	ns := FieldNamespace("res_A")

	if !l.IsFree(ns, "Email") {
		t.Fatal("a symbol never recorded must be free")
	}
	if _, recorded := l.Status(ns, "Email"); recorded {
		t.Fatal("Status of an unrecorded symbol must report recorded=false")
	}

	if err := l.Record(ns, "Email", "fld_A"); err != nil {
		t.Fatalf("record: %v", err)
	}
	if l.IsFree(ns, "Email") {
		t.Error("a recorded symbol is not free")
	}
	if !l.IsLive(ns, "Email") {
		t.Error("a freshly recorded symbol is live")
	}
	if owner, ok := l.OwnerOf(ns, "Email"); !ok || owner != "fld_A" {
		t.Errorf("OwnerOf = (%q,%v), want (fld_A,true)", owner, ok)
	}
}

// TestLedgerNeverRecycles is the §20 invariant: a symbol, once recorded, cannot
// be minted again in that namespace — not while live (collision), and not after
// it is tombstoned (recycling a retired name would silently repoint a frozen
// ABI at a different entity).
func TestLedgerNeverRecycles(t *testing.T) {
	l := NewLedger()
	ns := NamespaceResource

	if err := l.Record(ns, "Customer", "res_A"); err != nil {
		t.Fatalf("first record: %v", err)
	}
	if err := l.Record(ns, "Customer", "res_B"); !errors.Is(err, ErrSymbolTaken) {
		t.Errorf("re-recording a live symbol = %v, want ErrSymbolTaken", err)
	}

	if err := l.Tombstone(ns, "Customer"); err != nil {
		t.Fatalf("tombstone: %v", err)
	}
	if err := l.Record(ns, "Customer", "res_B"); !errors.Is(err, ErrSymbolTaken) {
		t.Errorf("re-recording a tombstoned symbol = %v, want ErrSymbolTaken (no recycle)", err)
	}
	// The tombstoned symbol still names its original owner (lineage).
	if owner, ok := l.OwnerOf(ns, "Customer"); !ok || owner != "res_A" {
		t.Errorf("tombstoned OwnerOf = (%q,%v), want (res_A,true)", owner, ok)
	}
}

func TestLedgerTombstone(t *testing.T) {
	l := NewLedger()
	ns := FieldNamespace("res_A")

	if err := l.Tombstone(ns, "Email"); !errors.Is(err, ErrSymbolNotLive) {
		t.Errorf("tombstone of a free symbol = %v, want ErrSymbolNotLive", err)
	}

	if err := l.Record(ns, "Email", "fld_A"); err != nil {
		t.Fatal(err)
	}
	if err := l.Tombstone(ns, "Email"); err != nil {
		t.Fatalf("tombstone live: %v", err)
	}
	if !l.IsTombstoned(ns, "Email") || l.IsLive(ns, "Email") || l.IsFree(ns, "Email") {
		t.Error("after tombstone the symbol must be tombstoned, not live, not free")
	}
	if err := l.Tombstone(ns, "Email"); !errors.Is(err, ErrSymbolNotLive) {
		t.Errorf("re-tombstoning = %v, want ErrSymbolNotLive", err)
	}
}

// TestLedgerNamespacesAreIndependent: uniqueness is per namespace (§7). The same
// symbol in two resources' field namespaces, and in the resource namespace, are
// three unrelated records.
func TestLedgerNamespacesAreIndependent(t *testing.T) {
	l := NewLedger()
	a := FieldNamespace("res_A")
	b := FieldNamespace("res_B")

	if err := l.Record(a, "Email", "fld_A"); err != nil {
		t.Fatal(err)
	}
	// Same symbol, different resource namespace: must be allowed and independent.
	if err := l.Record(b, "Email", "fld_B"); err != nil {
		t.Errorf("Email in a second resource namespace must be free: %v", err)
	}
	if err := l.Record(NamespaceResource, "Email", "res_C"); err != nil {
		t.Errorf("Email as a resource type name is a third namespace: %v", err)
	}

	if o, _ := l.OwnerOf(a, "Email"); o != "fld_A" {
		t.Errorf("namespace a owner = %q, want fld_A", o)
	}
	if o, _ := l.OwnerOf(b, "Email"); o != "fld_B" {
		t.Errorf("namespace b owner = %q, want fld_B", o)
	}
	// Tombstoning in one namespace does not touch the others.
	if err := l.Tombstone(a, "Email"); err != nil {
		t.Fatal(err)
	}
	if !l.IsLive(b, "Email") {
		t.Error("tombstoning Email in resource A must not affect resource B")
	}
}

// TestLedgerRoundTrips: the persisted form reconstructs the same history,
// including tombstones (§70 "reconstructible symbol history").
func TestLedgerRoundTrips(t *testing.T) {
	l := NewLedger()
	_ = l.Record(NamespaceResource, "Customer", "res_A")
	_ = l.Record(FieldNamespace("res_A"), "Email", "fld_A")
	_ = l.Tombstone(FieldNamespace("res_A"), "Email")
	_ = l.Record(FieldNamespace("res_A"), "Email2", "fld_B")

	data, err := json.Marshal(l)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Ledger
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !got.IsLive(NamespaceResource, "Customer") {
		t.Error("reconstructed: Customer should be live")
	}
	if !got.IsTombstoned(FieldNamespace("res_A"), "Email") {
		t.Error("reconstructed: Email should be tombstoned")
	}
	if !got.IsLive(FieldNamespace("res_A"), "Email2") {
		t.Error("reconstructed: Email2 should be live")
	}
	// A reconstructed tombstone is still not recyclable.
	if err := got.Record(FieldNamespace("res_A"), "Email", "fld_C"); !errors.Is(err, ErrSymbolTaken) {
		t.Errorf("reconstructed tombstone must not be recyclable: %v", err)
	}
}

// TestLedgerSerializationIsDeterministic: two ledgers built by recording the
// same symbols in different orders must serialize to identical bytes, so the
// on-disk file does not churn under Git for a semantically identical history.
func TestLedgerSerializationIsDeterministic(t *testing.T) {
	build := func(order [][2]string) []byte {
		l := NewLedger()
		for _, s := range order {
			_ = l.Record(FieldNamespace("res_A"), s[0], ID(s[1]))
		}
		data, err := json.MarshalIndent(l, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	forward := build([][2]string{{"A", "fld_A"}, {"B", "fld_B"}, {"C", "fld_C"}})
	reverse := build([][2]string{{"C", "fld_C"}, {"B", "fld_B"}, {"A", "fld_A"}})
	if string(forward) != string(reverse) {
		t.Errorf("serialization is order-dependent:\n%s\n---\n%s", forward, reverse)
	}
}

// TestLedgerRejectsCorruptStatus: loading a ledger with an unknown status fails
// rather than admitting a symbol in an undefined state.
func TestLedgerRejectsCorruptStatus(t *testing.T) {
	bad := `{"resource":{"Customer":{"status":"zombie","entity_id":"res_A"}}}`
	var l Ledger
	if err := json.Unmarshal([]byte(bad), &l); err == nil {
		t.Error("unmarshal must reject an unknown symbol status")
	}

	noID := `{"resource":{"Customer":{"status":"live","entity_id":""}}}`
	if err := json.Unmarshal([]byte(noID), &l); err == nil {
		t.Error("unmarshal must reject a symbol with no entity_id")
	}
}

// TestLedgerMarshalShape pins the §11 structure and its sorted, indented,
// Git-friendly form.
func TestLedgerMarshalShape(t *testing.T) {
	l := NewLedger()
	_ = l.Record(NamespaceResource, "Customer", "res_A")
	_ = l.Record(FieldNamespace("res_A"), "Email2", "fld_B")
	_ = l.Record(FieldNamespace("res_A"), "Email", "fld_A")
	_ = l.Tombstone(FieldNamespace("res_A"), "Email")

	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "fields:res_A": {
    "Email": {
      "status": "tombstoned",
      "entity_id": "fld_A"
    },
    "Email2": {
      "status": "live",
      "entity_id": "fld_B"
    }
  },
  "resource": {
    "Customer": {
      "status": "live",
      "entity_id": "res_A"
    }
  }
}`
	if string(data) != want {
		t.Errorf("marshaled ledger:\n%s\nwant:\n%s", data, want)
	}
}
