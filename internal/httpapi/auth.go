package httpapi

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const (
	ContextKeyUserID         contextKey = "userID"
	ContextKeyMerchantID     contextKey = "merchantID"
	ContextKeyRegistrationID contextKey = "registrationID"
	ContextKeyRole           contextKey = "role"
)

type AuthConfig struct {
	APIKey        string
	PublicKeyPath string
	Issuer        string
	Audience      string
}

type AuthMiddleware struct {
	apiKey    string
	publicKey interface{}
	issuer    string
	audience  string
}

type MerchantClaims struct {
	MerchantID     string `json:"merchant_id"`
	RegistrationID string `json:"registration_id"`
	Role           string `json:"role"`
	jwt.RegisteredClaims
}

func NewAuthMiddleware(config AuthConfig) (*AuthMiddleware, error) {
	publicKeyPEM, err := os.ReadFile(config.PublicKeyPath)
	if err != nil {
		return nil, fmt.Errorf("read jwt public key: %w", err)
	}

	publicKey, err := jwt.ParseRSAPublicKeyFromPEM(publicKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse jwt public key: %w", err)
	}

	return &AuthMiddleware{
		apiKey:    config.APIKey,
		publicKey: publicKey,
		issuer:    config.Issuer,
		audience:  config.Audience,
	}, nil
}

func (m *AuthMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.Header.Get("X-API-Key")

		if apiKey == "" {
			writeError(w, http.StatusUnauthorized, "missing API key")
			return
		}

		if subtle.ConstantTimeCompare(
			[]byte(apiKey),
			[]byte(m.apiKey),
		) != 1 {
			writeError(w, http.StatusUnauthorized, "invalid API key")
			return
		}

		authHeader := r.Header.Get("Authorization")

		if !strings.HasPrefix(authHeader, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")

		claims := &MerchantClaims{}

		token, err := jwt.ParseWithClaims(
			tokenString,
			claims,
			func(token *jwt.Token) (interface{}, error) {
				if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
					return nil, fmt.Errorf(
						"unexpected signing method: %v",
						token.Header["alg"],
					)
				}

				return m.publicKey, nil
			},
			jwt.WithValidMethods([]string{"RS256"}),
			jwt.WithIssuer(m.issuer),
			jwt.WithAudience(m.audience),
			jwt.WithExpirationRequired(),
		)

		if err != nil || !token.Valid {
			writeError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		if claims.Role == "MERCHANT" && claims.MerchantID == "" {
			writeError(w, http.StatusUnauthorized, "merchant_id is required")
			return
		}

		if claims.Role == "MERCHANT_REGISTRATION" && claims.RegistrationID == "" {
			writeError(w, http.StatusUnauthorized, "registration_id is required")
			return
		}

		if err != nil || !token.Valid {
			writeError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		allowedRoles := map[string]bool{
			"MERCHANT":              true,
			"MERCHANT_REGISTRATION": true,
		}

		if !allowedRoles[claims.Role] {
			writeError(w, http.StatusForbidden, "insufficient role")
			return
		}

		ctx := context.WithValue(r.Context(), ContextKeyUserID, claims.Subject)
		ctx = context.WithValue(ctx, ContextKeyMerchantID, claims.MerchantID)
		ctx = context.WithValue(ctx, ContextKeyRegistrationID, claims.RegistrationID)
		ctx = context.WithValue(ctx, ContextKeyRole, claims.Role)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func GetUserID(ctx context.Context) string {
	value, _ := ctx.Value(ContextKeyUserID).(string)

	return value
}

func GetMerchantID(ctx context.Context) string {
	value, _ := ctx.Value(ContextKeyMerchantID).(string)

	return value
}

type APIKeyOnlyMiddleware struct {
	apiKey string
}

func NewAPIKeyMiddleware(apiKey string) *APIKeyOnlyMiddleware {
	return &APIKeyOnlyMiddleware{
		apiKey: apiKey,
	}
}

func (m *APIKeyOnlyMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.Header.Get("X-API-Key")

		if apiKey == "" {
			writeError(w, http.StatusUnauthorized, "missing API key")
			return
		}

		if subtle.ConstantTimeCompare(
			[]byte(apiKey),
			[]byte(m.apiKey),
		) != 1 {
			writeError(w, http.StatusUnauthorized, "invalid API key")
			return
		}

		next.ServeHTTP(w, r)
	})
}
