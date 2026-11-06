package services

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"time"

	"dms/internal/domain"
	"dms/internal/repository"
)

// HashAPIKey returns the hex SHA-256 stored in api_client_keys.key_hash. API
// keys are 256-bit random values, so a fast hash is sufficient (no password
// stretching needed) and allows an indexed lookup.
func HashAPIKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

type AuthService struct {
	keys repository.KeyRepository
}

func NewAuthService(keys repository.KeyRepository) *AuthService {
	return &AuthService{keys: keys}
}

// Authenticate validates the X-Client-Id / X-Api-Key pair. Every failure
// returns the same generic error so callers cannot probe which part was wrong.
func (s *AuthService) Authenticate(ctx context.Context, clientID, apiKey string) (*domain.Principal, error) {
	fail := domain.Unauthorized("invalid or missing credentials")
	if clientID == "" || apiKey == "" {
		return nil, fail
	}

	key, client, err := s.keys.FindByHash(ctx, HashAPIKey(apiKey))
	if err != nil {
		var appErr *domain.AppError
		if errors.As(err, &appErr) && appErr.Kind == domain.KindNotFound {
			return nil, fail
		}
		return nil, domain.Internal(err)
	}

	// The key must belong to the client_id presented with it.
	if subtle.ConstantTimeCompare([]byte(client.ClientID), []byte(clientID)) != 1 {
		return nil, fail
	}
	if client.Status != domain.ClientActive || key.Status != domain.KeyActive || key.RevokedAt != nil {
		return nil, fail
	}
	if key.ExpiresAt != nil && !key.ExpiresAt.After(time.Now()) {
		return nil, fail
	}

	return &domain.Principal{
		ClientUUID: client.ID,
		ClientID:   client.ClientID,
		KeyID:      key.ID,
		ActorType:  domain.ActorTypeClient,
	}, nil
}
