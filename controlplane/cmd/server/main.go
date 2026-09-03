// Command server boots the Forge control plane.
//
// The control plane is itself a Gombit application (DESIGN.md §6, D7): Forge
// dogfoods Gombit rather than building a bespoke backend. It runs with
// cookie/session auth (DESIGN.md §20, D5) and is PostgreSQL-backed (D4).
//
// In cookie mode framework.New mounts the admin surface automatically, with no
// explicit wiring here: admin.Mount serves the gated data plane at
// /api/v1/admin/*, and the framework-owned admin SPA (gombit's internal/adminui)
// is served at /admin/. The admin catalog is empty until a model registers with
// it — the M1 model issues (User, Organization, Project, …) fill it in.
package main

import (
	"context"
	"log"
	"os"

	"github.com/gombit-dev/gombit/config"
	"github.com/gombit-dev/gombit/framework"

	"github.com/gombit-dev/gombit-forge/controlplane/internal/githubconnect"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/githubexport"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/org"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/platform"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/project"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	db, err := platform.OpenDatabase(cfg.Database)
	if err != nil {
		log.Fatal(err)
	}

	app, err := framework.New(
		framework.WithConfig(cfg),
		framework.WithDatabase(db),
	)
	if err != nil {
		_ = db.Close()
		log.Fatal(err)
	}

	// Feature packages register explicitly (Gombit does not discover them by
	// reflection). Tenancy is the first; the project API (#39) is the second.
	if err := org.Register(app); err != nil {
		_ = db.Close()
		log.Fatal(err)
	}
	if err := project.Register(app); err != nil {
		_ = db.Close()
		log.Fatal(err)
	}
	// GitHub repository export (#85) is optional: it registers only when the
	// OAuth app credentials are configured, so the control plane runs fine
	// without them. GitHub OAuth is not part of Gombit's typed config, so its
	// settings are read from the environment here at the composition root, not
	// inside a runtime package.
	if ghCfg, successRedirect, ok := githubOAuthConfig(); ok {
		if err := githubconnect.Register(app, ghCfg, successRedirect); err != nil {
			_ = db.Close()
			log.Fatal(err)
		}
	}

	app.OnStop(func(context.Context) error { return db.Close() })

	if err := framework.Run(app); err != nil {
		log.Fatal(err)
	}
}

// githubOAuthConfig reads the GitHub OAuth app settings from the environment.
// It reports ok=false (and the feature stays unregistered) unless the client
// id, secret and redirect URL are all set — the three the OAuth handshake
// cannot run without. The success redirect defaults to the app root.
func githubOAuthConfig() (cfg githubexport.Config, successRedirect string, ok bool) {
	clientID := os.Getenv("GITHUB_OAUTH_CLIENT_ID")
	clientSecret := os.Getenv("GITHUB_OAUTH_CLIENT_SECRET")
	redirectURL := os.Getenv("GITHUB_OAUTH_REDIRECT_URL")
	if clientID == "" || clientSecret == "" || redirectURL == "" {
		return githubexport.Config{}, "", false
	}
	successRedirect = os.Getenv("GITHUB_OAUTH_SUCCESS_REDIRECT")
	if successRedirect == "" {
		successRedirect = "/"
	}
	return githubexport.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		// OAuthBaseURL and APIBaseURL default to public GitHub; override via
		// GITHUB_OAUTH_BASE_URL / GITHUB_API_BASE_URL for GitHub Enterprise.
		OAuthBaseURL: os.Getenv("GITHUB_OAUTH_BASE_URL"),
		APIBaseURL:   os.Getenv("GITHUB_API_BASE_URL"),
	}, successRedirect, true
}
