package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gombit-dev/gombit/config"

	"github.com/gombit-dev/gombit-forge/controlplane/internal/githubexport"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/mail"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/platform"
)

var update = flag.Bool("update", false, "rewrite controlplane/openapi.json from the server's document")

// documentPath is the committed OpenAPI document: the contract the editor's
// generated TypeScript client (controlplane/web/src/api/generated) is built from.
var documentPath = filepath.Join("..", "..", "openapi.json")

// TestOpenAPIDocumentIsCommitted holds controlplane/openapi.json to the
// document a fully configured server serves at /openapi.json: every optional
// route set on, built by newApp as main builds it. A route or schema change
// fails here until the document is regenerated (go test ./cmd/server -update),
// and then the client with it (gombit client generate, in controlplane/web).
//
// Building the app needs a database handle but queries nothing, so an in-memory
// SQLite one serves, and the test runs without Postgres.
func TestOpenAPIDocumentIsCommitted(t *testing.T) {
	env := map[string]string{
		"GOMBIT_DATABASE_DRIVER": "sqlite",
		"GOMBIT_DATABASE_DSN":    "file::memory:",
		"GOMBIT_AUTH_MODE":       "cookie",
		"GOMBIT_JWT_SECRET":      "openapi-document-test-secret-0123456789",
	}
	cfg, err := config.LoadFromEnv(func(key string) (string, bool) {
		v, ok := env[key]
		return v, ok
	})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	db, err := platform.OpenDatabase(cfg.Database)
	if err != nil {
		t.Fatalf("database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	app, _, err := newApp(cfg, db, features{
		mailer: &mail.SMTP{Addr: "127.0.0.1:1", From: "forge@example.test"},
		github: &githubFeature{
			cfg:             githubexport.Config{ClientID: "id", ClientSecret: "secret", RedirectURL: "http://localhost/callback"},
			successRedirect: "/",
		},
		cloud: &cloudFeature{baseURL: "http://127.0.0.1:1", token: "token"},
	})
	if err != nil {
		t.Fatalf("newApp: %v", err)
	}

	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /openapi.json = %d", rec.Code)
	}
	var got bytes.Buffer
	if err := json.Indent(&got, rec.Body.Bytes(), "", "  "); err != nil {
		t.Fatalf("document is not JSON: %v", err)
	}
	got.WriteByte('\n')

	if *update {
		if err := os.WriteFile(documentPath, got.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	// #nosec G304 -- a fixed path in this repository.
	want, err := os.ReadFile(documentPath)
	if err != nil {
		t.Fatalf("read the committed document: %v (run go test ./cmd/server -run TestOpenAPIDocumentIsCommitted -update)", err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Fatalf("controlplane/openapi.json is not the document the server serves; regenerate it with\n" +
			"  go test ./cmd/server -run TestOpenAPIDocumentIsCommitted -update\n" +
			"and then the editor's client with\n" +
			"  cd web && gombit client generate --spec ../openapi.json")
	}
}
