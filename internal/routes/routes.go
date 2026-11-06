// Package routes wires URL paths to handlers and middleware. Start here when
// tracing a request: routes → middleware → handlers → services → repository /
// connector.
package routes

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"dms/internal/domain"
	"dms/internal/handlers"
	"dms/internal/httpx"
	"dms/internal/middleware"
	"dms/internal/services"
)

// Deps are the collaborators the router needs.
type Deps struct {
	Log        *slog.Logger
	Auth       *services.AuthService
	Health     *handlers.HealthHandler
	Documents  *handlers.DocumentHandler
	Admin      *handlers.AdminHandler // nil disables /admin/v1
	AdminToken string
	TrustProxy bool
}

// New builds the HTTP handler for the whole service.
func New(d Deps) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Recover(d.Log))
	r.Use(middleware.Logger(d.Log))

	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		httpx.Error(w, req, d.Log, domain.NotFound("route not found"))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		httpx.JSON(w, http.StatusMethodNotAllowed, map[string]any{
			"error": map[string]any{
				"code":       "method_not_allowed",
				"message":    "method not allowed",
				"request_id": httpx.RequestID(req.Context()),
			},
		})
	})

	// Unauthenticated probes.
	r.Get("/healthz", d.Health.Live)
	r.Get("/readyz", d.Health.Ready)

	// Public API used by consuming applications (X-Client-Id + X-Api-Key).
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.ClientAuth(d.Auth, d.TrustProxy, d.Log))

		r.Route("/documents", func(r chi.Router) {
			r.Post("/", d.Documents.Upload)
			r.Get("/", d.Documents.List)

			r.Route("/{documentID}", func(r chi.Router) {
				r.Get("/", d.Documents.Get)
				r.Patch("/", d.Documents.Update)
				r.Delete("/", d.Documents.Delete)

				r.Get("/download", d.Documents.Download)
				r.Get("/preview", d.Documents.Preview)

				r.Get("/versions", d.Documents.ListVersions)
				r.Post("/versions", d.Documents.UploadVersion)
			})
		})
	})

	// Operator API for onboarding applications (Authorization: Bearer ADMIN_TOKEN).
	if d.Admin != nil {
		r.Route("/admin/v1", func(r chi.Router) {
			r.Use(middleware.AdminAuth(d.AdminToken, d.Log))

			r.Post("/clients", d.Admin.CreateClient)
			r.Get("/clients", d.Admin.ListClients)
			r.Get("/clients/{clientID}/keys", d.Admin.ListKeys)
			r.Post("/clients/{clientID}/keys", d.Admin.CreateKey)
			r.Post("/clients/{clientID}/keys/{keyID}/revoke", d.Admin.RevokeKey)
		})
	}

	return r
}
