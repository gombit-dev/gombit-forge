package org

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gombit-dev/gombit/auth"
	"github.com/gombit-dev/gombit/framework"
)

// cookieSecurityName is the OpenAPI security scheme the admin plane also uses;
// naming it here documents that these routes are cookie-session protected.
const cookieSecurityName = "cookieAuth"

// Register mounts the org tenancy routes on the app. It is called explicitly
// from main — Gombit does not discover feature packages by reflection.
//
// Every operation sits behind the cookie-session gate, so the control plane's
// tenancy API is only reachable by an authenticated user; per-org authorization
// then happens inside each handler against the caller's role.
func Register(app *framework.App) error {
	authSvc, err := auth.NewService(app.DB(), app.Config())
	if err != nil {
		return err
	}
	h := &Handler{svc: NewService(app.DB())}

	prefix := app.Config().API.Prefix
	api := app.API()
	gate := huma.Middlewares{authSvc.RequireCookieSession()}
	security := []map[string][]string{{cookieSecurityName: {}}}
	tags := []string{"Organizations"}

	huma.Register(api, huma.Operation{
		OperationID: "create-organization",
		Method:      http.MethodPost,
		Path:        prefix + "/organizations",
		Summary:     "Create an organization",
		Tags:        tags,
		Security:    security,
		Middlewares: gate,
	}, h.createOrganization)

	huma.Register(api, huma.Operation{
		OperationID: "list-organization-members",
		Method:      http.MethodGet,
		Path:        prefix + "/organizations/{orgID}/members",
		Summary:     "List organization members",
		Tags:        tags,
		Security:    security,
		Middlewares: gate,
	}, h.listMembers)

	huma.Register(api, huma.Operation{
		OperationID: "invite-organization-member",
		Method:      http.MethodPost,
		Path:        prefix + "/organizations/{orgID}/invitations",
		Summary:     "Invite a member to an organization",
		Tags:        tags,
		Security:    security,
		Middlewares: gate,
	}, h.inviteMember)

	huma.Register(api, huma.Operation{
		OperationID: "accept-invitation",
		Method:      http.MethodPost,
		Path:        prefix + "/invitations/accept",
		Summary:     "Accept an invitation to join an organization",
		Tags:        tags,
		Security:    security,
		Middlewares: gate,
	}, h.acceptInvitation)

	return nil
}
