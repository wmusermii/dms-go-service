// Package httpx holds the small HTTP helpers shared by middleware and
// handlers: JSON envelopes, error mapping and the request-id context.
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"dms/internal/domain"
)

type requestIDKey struct{}

// WithRequestID stores the request id in ctx.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID returns the request id stored in ctx, or "".
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// ListMeta is the pagination block of list responses.
type ListMeta struct {
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// JSON writes v as a JSON body.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Data writes {"data": ...}.
func Data(w http.ResponseWriter, status int, data any) {
	JSON(w, status, map[string]any{"data": data})
}

// List writes {"data": [...], "meta": {...}}.
func List(w http.ResponseWriter, data any, meta ListMeta) {
	JSON(w, http.StatusOK, map[string]any{"data": data, "meta": meta})
}

// NoContent writes an empty 204.
func NoContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

// Error maps err to an HTTP response. Unexpected errors are logged and
// reported to the client as a generic 500.
func Error(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		appErr = domain.Internal(err)
	}
	if appErr.Kind == domain.KindInternal {
		log.Error("request failed", "request_id", RequestID(r.Context()),
			"method", r.Method, "path", r.URL.Path, "error", appErr.Err)
	}
	JSON(w, statusFor(appErr.Kind), map[string]any{
		"error": map[string]any{
			"code":       appErr.Code,
			"message":    appErr.Message,
			"request_id": RequestID(r.Context()),
		},
	})
}

func statusFor(k domain.Kind) int {
	switch k {
	case domain.KindInvalid:
		return http.StatusBadRequest
	case domain.KindUnauthorized:
		return http.StatusUnauthorized
	case domain.KindNotFound:
		return http.StatusNotFound
	case domain.KindConflict, domain.KindNotAvailable:
		return http.StatusConflict
	case domain.KindTooLarge:
		return http.StatusRequestEntityTooLarge
	case domain.KindUnsupported:
		return http.StatusUnsupportedMediaType
	default:
		return http.StatusInternalServerError
	}
}
