package controlplane_test

import (
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"

	"github.com/gombit-dev/gombit-forge/controlplane/internal/cloudbuild"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/exportjob"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/githubconnect"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/org"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/project"
)

// TestAllRoutesRegisterTogether registers every route set the server can mount
// on one API, as cmd/server does with GitHub export and Cloud build both
// configured. Huma names a response schema after its Go type, so two packages'
// same-named types collide and the second registration panics ("duplicate
// name"), which crashed the server at boot. The optional route sets are wired
// only when their configuration is set, so no other test mounts them together.
func TestAllRoutesRegisterTogether(t *testing.T) {
	_, api := humatest.New(t)
	const prefix = "/api/v1"
	var gate huma.Middlewares

	org.RegisterRoutes(api, prefix, gate, nil)
	project.RegisterRoutes(api, prefix, gate, nil, nil)
	githubconnect.RegisterRoutes(api, prefix, gate, nil, "/")
	exportjob.RegisterRoutes(api, prefix, gate, nil, nil, nil)
	cloudbuild.RegisterRoutes(api, prefix, gate, nil, nil, nil, nil)

	paths := api.OpenAPI().Paths
	for _, want := range []string{
		prefix + "/organizations",
		prefix + "/projects/{projectID}/spec",
		prefix + "/integrations/github/connect",
		prefix + "/export-jobs/{jobID}",
		prefix + "/build-jobs/{jobID}",
		prefix + "/projects/{projectID}/deploy",
	} {
		if _, ok := paths[want]; !ok {
			t.Errorf("OpenAPI document has no %s", want)
		}
	}
}
