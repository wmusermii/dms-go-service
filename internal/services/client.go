package services

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
	"time"

	"dms/internal/domain"
	"dms/internal/repository"
)

var clientIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{2,63}$`)

// ClientService manages API clients and their keys (admin operations).
type ClientService struct {
	clients repository.ClientRepository
	keys    repository.KeyRepository
}

func NewClientService(clients repository.ClientRepository, keys repository.KeyRepository) *ClientService {
	return &ClientService{clients: clients, keys: keys}
}

func (s *ClientService) CreateClient(ctx context.Context, clientID, name string) (*domain.APIClient, error) {
	clientID = strings.TrimSpace(clientID)
	name = strings.TrimSpace(name)
	if !clientIDRe.MatchString(clientID) {
		return nil, domain.Invalid("invalid_client_id",
			"client_id must be 3-64 chars of lower-case letters, digits, '-' or '_', starting with a letter or digit")
	}
	if name == "" || len(name) > 255 {
		return nil, domain.Invalid("invalid_name", "name is required (max 255 chars)")
	}

	now := time.Now().UTC()
	c := &domain.APIClient{
		ID: domain.NewID(), ClientID: clientID, Name: name,
		Status: domain.ClientActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.clients.Create(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *ClientService) ListClients(ctx context.Context) ([]domain.APIClient, error) {
	return s.clients.List(ctx)
}

// IssueKey creates a new key and returns it in plaintext. The plaintext is not
// stored and cannot be retrieved again.
func (s *ClientService) IssueKey(ctx context.Context, clientUUID string, expiresAt *time.Time) (*domain.APIClientKey, string, error) {
	if !domain.IsUUID(clientUUID) {
		return nil, "", domain.NotFound("client not found")
	}
	if expiresAt != nil && !expiresAt.After(time.Now()) {
		return nil, "", domain.Invalid("invalid_expiry", "expires_at must be in the future")
	}
	if _, err := s.clients.GetByID(ctx, clientUUID); err != nil {
		return nil, "", err
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, "", domain.Internal(fmt.Errorf("generate key: %w", err))
	}
	plaintext := "dms_" + base64.RawURLEncoding.EncodeToString(raw)

	key := &domain.APIClientKey{
		ID:          domain.NewID(),
		APIClientID: clientUUID,
		KeyPrefix:   plaintext[:12],
		KeyHash:     HashAPIKey(plaintext),
		Status:      domain.KeyActive,
		ExpiresAt:   expiresAt,
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.keys.Create(ctx, key); err != nil {
		return nil, "", err
	}
	return key, plaintext, nil
}

func (s *ClientService) ListKeys(ctx context.Context, clientUUID string) ([]domain.APIClientKey, error) {
	if !domain.IsUUID(clientUUID) {
		return nil, domain.NotFound("client not found")
	}
	if _, err := s.clients.GetByID(ctx, clientUUID); err != nil {
		return nil, err
	}
	return s.keys.ListByClient(ctx, clientUUID)
}

func (s *ClientService) RevokeKey(ctx context.Context, clientUUID, keyID string) error {
	if !domain.IsUUID(clientUUID) || !domain.IsUUID(keyID) {
		return domain.NotFound("key not found")
	}
	return s.keys.Revoke(ctx, clientUUID, keyID)
}
