package gen

import (
	"fmt"
	"path"
	"strings"

	"github.com/gombit-dev/gombit-forge/internal/compiler/graph"
)

// Admin generates admin.go per admin-visible resource under
// internal/forge_generated/<resource>/ (DESIGN.md §9 stage 6, §4.3).
//
// The generated code registers the model with Gombit's admin through the
// public admin.Register entry point (ADR-004 D3); it declares no admin data
// plane, permission model or UI of its own. Managed apps use cookie auth
// (DESIGN.md D5), which is what admin.Register requires.
//
// Only resources whose behavior marks them admin-visible produce a file, and
// admin create/update/delete follow the same CRUD toggles as the API; list and
// detail are always enabled so a visible resource can be browsed. Files come
// back in the graph's authored resource order.
func Admin(g *graph.Graph) ([]File, error) {
	if g == nil {
		return nil, fmt.Errorf("gen: nil graph")
	}
	if err := validatePackages(g); err != nil {
		return nil, err
	}

	var files []File
	for _, resource := range g.Resources {
		if err := validateNames(resource); err != nil {
			return nil, err
		}
		if !resource.Spec.Behavior.AdminVisible {
			continue
		}

		view := newAdminView(resource)
		src, err := renderTemplate(adminTemplate, view)
		if err != nil {
			return nil, err
		}
		file, err := formatGo(path.Join(PackageDir(resource), "admin.go"), src)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

// adminView augments resourceView with the admin registration fields.
type adminView struct {
	resourceView
	Slug     string
	Singular string
	Plural   string // empty means "let Gombit derive it"
}

func newAdminView(resource *graph.Resource) adminView {
	plural := resource.Spec.LabelPlural
	if strings.TrimSpace(plural) == resource.Spec.Label {
		// Identical to the singular label carries no information; let Gombit
		// derive the plural instead of emitting a redundant value.
		plural = ""
	}
	return adminView{
		resourceView: newResourceView(resource),
		Slug:         resource.Spec.StorageName,
		Singular:     resource.Spec.Label,
		Plural:       plural,
	}
}
