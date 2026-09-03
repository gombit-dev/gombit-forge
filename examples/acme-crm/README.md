# Acme CRM — example `ProjectSpec`

[`spec.json`](spec.json) is the canonical-JSON application model the
[tutorial](../../docs/tutorial.md) builds and runs: a two-resource CRM
(Customer, Invoice) exercising every MVP field type, a `belongs_to`
relationship, the CRUD/visibility toggles, the declared list-query surface
(search / filter / sort), a numeric aggregate, and all four page kinds
(table, form, detail, dashboard).

It is a real, validated spec — `spec.Unmarshal` → `spec.Validate` →
`compiler.Compile` produces a 23-file tree. Load it with:

```go
data, _ := os.ReadFile("examples/acme-crm/spec.json")
s, _ := spec.Unmarshal(data)
files, _ := compiler.Compile(s, "example.com/app")
```

The stable IDs (`res_…`, `fld_…`, `pag_…`, `nav_…`) are ULIDs. In the product
they are minted by the editor; here they are fixed so the file is copy-pasteable
and every cross-reference (a `belongs_to` `target`, a page's `resource`, a
behavior's field lists, a dashboard card's `field`) resolves. Never rename a
field by editing its `code_name` in place — identity is the ID, not the name
(ADR-001).
