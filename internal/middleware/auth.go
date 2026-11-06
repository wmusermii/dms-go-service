package middleware

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"dms/internal/domain"
	"dms/internal/httpx"
	"dms/internal/services"
)

type principalKey struct{}

// PrincipalFrom returns the authenticated caller stored by ClientAuth.
func PrincipalFrom(ctx context.Context) domain.Principal {
	p, _ := ctx.Value(principalKey{}).(domain.Principal)
	return p
}

// ClientAuth authenticates API clients using the X-Client-Id and X-Api-Key
// headers. Calling applications may also send X-Actor-Id (their end-user's id)
// which is recorded in the audit log.
func ClientAuth(auth *services.AuthService, trustProxy bool, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, err := auth.Authenticate(r.Context(),
				strings.TrimSpace(r.Header.Get("X-Client-Id")),
				strings.TrimSpace(r.Header.Get("X-Api-Key")))
			if err != nil {
				log.Warn("authentication failed",
					"request_id", httpx.RequestID(r.Context()),
					"ip", clientIP(r, trustProxy), "path", r.URL.Path)
				httpx.Error(w, r, log, err)
				return
			}

			p.IP = clientIP(r, trustProxy)
			p.UserAgent = truncate(r.UserAgent(), 255)
			p.RequestID = httpx.RequestID(r.Context())
			if actor := truncate(strings.TrimSpace(r.Header.Get("X-Actor-Id")), 128); actor != "" {
				p.ActorID, p.ActorType = actor, domain.ActorTypeUser
			}

			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, *p)))
		})
	}
}

// AdminAuth protects /admin/v1 with a static bearer token (ADMIN_TOKEN).
func AdminAuth(token string, log *slog.Logger) func(http.Handler) http.Handler {
	want := sha256.Sum256([]byte(token))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			have := sha256.Sum256([]byte(strings.TrimSpace(got)))
			// Compare fixed-length digests in constant time.
			if !ok || subtle.ConstantTimeCompare(have[:], want[:]) != 1 {
				httpx.Error(w, r, log, domain.Unauthorized("invalid or missing admin token"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first, _, _ := strings.Cut(xff, ",")
			if ip := strings.TrimSpace(first); ip != "" {
				return truncate(ip, 64)
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}
