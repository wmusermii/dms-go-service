package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"dms/internal/domain"
	"dms/internal/httpx"
	"dms/internal/services"
)

// AdminHandler exposes client / API-key management under /admin/v1.
type AdminHandler struct {
	svc *services.ClientService
	log *slog.Logger
}

func NewAdminHandler(svc *services.ClientService, log *slog.Logger) *AdminHandler {
	return &AdminHandler{svc: svc, log: log}
}

func (h *AdminHandler) CreateClient(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ClientID string `json:"client_id"`
		Name     string `json:"name"`
	}
	if err := decodeJSON(w, r, &body, false); err != nil {
		httpx.Error(w, r, h.log, err)
		return
	}
	c, err := h.svc.CreateClient(r.Context(), body.ClientID, body.Name)
	if err != nil {
		httpx.Error(w, r, h.log, err)
		return
	}
	httpx.Data(w, http.StatusCreated, c)
}

func (h *AdminHandler) ListClients(w http.ResponseWriter, r *http.Request) {
	cs, err := h.svc.ListClients(r.Context())
	if err != nil {
		httpx.Error(w, r, h.log, err)
		return
	}
	httpx.Data(w, http.StatusOK, cs)
}

func (h *AdminHandler) CreateKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ExpiresAt *time.Time `json:"expires_at"`
	}
	// The body is optional: an empty body issues a non-expiring key.
	if err := decodeJSON(w, r, &body, true); err != nil {
		httpx.Error(w, r, h.log, err)
		return
	}
	key, plaintext, err := h.svc.IssueKey(r.Context(), chi.URLParam(r, "clientID"), body.ExpiresAt)
	if err != nil {
		httpx.Error(w, r, h.log, err)
		return
	}
	// The plaintext key is returned exactly once; only its hash is stored.
	httpx.Data(w, http.StatusCreated, map[string]any{
		"key":     plaintext,
		"details": key,
		"notice":  "store this key now; it cannot be shown again",
	})
}

func (h *AdminHandler) ListKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := h.svc.ListKeys(r.Context(), chi.URLParam(r, "clientID"))
	if err != nil {
		httpx.Error(w, r, h.log, err)
		return
	}
	httpx.Data(w, http.StatusOK, keys)
}

func (h *AdminHandler) RevokeKey(w http.ResponseWriter, r *http.Request) {
	err := h.svc.RevokeKey(r.Context(), chi.URLParam(r, "clientID"), chi.URLParam(r, "keyID"))
	if err != nil {
		httpx.Error(w, r, h.log, err)
		return
	}
	httpx.NoContent(w)
}

// decodeJSON strictly decodes a small JSON body (unknown fields are rejected).
// With optional=true an empty body is accepted and leaves dst untouched.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any, optional bool) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			if optional {
				return nil
			}
			return domain.Invalid("invalid_json", "request body is required")
		}
		return domain.Invalid("invalid_json", "request body is not valid JSON: "+err.Error())
	}
	return nil
}
