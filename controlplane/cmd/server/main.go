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

	"github.com/gombit-dev/gombit-forge/controlplane/internal/exportjob"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/exportworker"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/ghexport"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/githubconnect"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/githubexport"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/org"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/platform"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/project"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/projectspec"
	"github.com/gombit-dev/gombit-forge/internal/compiler"
	"github.com/gombit-dev/gombit-forge/internal/gombit"
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
	// stopWorker cancels the background export worker on shutdown; it stays a
	// no-op unless the export feature registers below.
	stopWorker := func() {}
	if ghCfg, successRedirect, ok := githubOAuthConfig(); ok {
		if err := githubconnect.Register(app, ghCfg, successRedirect); err != nil {
			_ = db.Close()
			log.Fatal(err)
		}
		if err := registerGitHubExport(app, ghCfg, &stopWorker); err != nil {
			_ = db.Close()
			log.Fatal(err)
		}
	}

	app.OnStop(func(context.Context) error {
		stopWorker() // before closing the DB the worker uses
		return db.Close()
	})

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
	set := 0
	for _, v := range []string{clientID, clientSecret, redirectURL} {
		if v != "" {
			set++
		}
	}
	if set < 3 {
		// All-empty is the intentional "feature off" path and stays silent; a
		// partial config is almost always a typo (a wrong var name, a missing
		// secret) that would otherwise fail as a mystery 404 on the connect
		// route, so surface it.
		if set > 0 {
			log.Printf("github oauth: partially configured (%d/3 of CLIENT_ID/CLIENT_SECRET/REDIRECT_URL set); connect flow disabled", set)
		}
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

// registerGitHubExport wires the asynchronous GitHub export (#85): the job
// routes plus the background worker that runs them. Enqueue happens in the HTTP
// request; the toolchain-heavy source assembly (scaffold, tidy, migrations)
// runs only in the worker, so no request performs a build.
//
// It sets *stopWorker to the worker's cancel func so main can stop it on
// shutdown before closing the database the worker uses. The export stack shares
// one projectspec.Source both as the worker's frozen-revision resolver and as
// ghexport's head-spec source.
func registerGitHubExport(app *framework.App, ghCfg githubexport.Config, stopWorker *func()) error {
	db := app.DB()
	projectSvc := project.NewService(db)
	src := projectspec.NewSource(projectSvc)
	tokens := githubconnect.NewService(db, githubconnect.NewExchanger(ghCfg, nil))
	pub := ghexport.NewPublisher(ghCfg, nil)

	// Query the gombit toolchain version once for export provenance. Export needs
	// the toolchain anyway; if it's unavailable the feature still wires and
	// provenance records "unknown" rather than failing startup.
	cli := &gombit.CLI{}
	gombitVersion := "unknown"
	if v, err := cli.Version(context.Background()); err == nil {
		gombitVersion = v.String()
	} else {
		log.Printf("github export: gombit toolchain version unavailable (%v); provenance will record %q", err, gombitVersion)
	}
	exporter := ghexport.NewService(tokens, src, compiler.GombitToolchain{CLI: cli}, pub, gombitVersion)

	jobs := exportjob.NewService(db)
	if err := exportjob.Register(app, jobs, projectSvc, org.NewService(db)); err != nil {
		return err
	}

	worker := exportworker.New(jobs, src, exporter, 0, nil)
	ctx, cancel := context.WithCancel(context.Background())
	*stopWorker = cancel
	go worker.Run(ctx)
	return nil
}
