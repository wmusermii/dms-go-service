package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"dms/internal/domain"
)

const keyCols = `id::text, api_client_id::text, key_prefix, key_hash, status, expires_at, revoked_at, created_at`

func scanKey(s scanner, k *domain.APIClientKey) error {
	var status string
	if err := s.Scan(&k.ID, &k.APIClientID, &k.KeyPrefix, &k.KeyHash, &status,
		&k.ExpiresAt, &k.RevokedAt, &k.CreatedAt); err != nil {
		return err
	}
	k.Status = domain.KeyStatus(status)
	return nil
}

type KeyRepo struct{ pool *pgxpool.Pool }

func NewKeyRepo(pool *pgxpool.Pool) *KeyRepo { return &KeyRepo{pool: pool} }

func (r *KeyRepo) Create(ctx context.Context, k *domain.APIClientKey) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO api_client_keys (id, api_client_id, key_prefix, key_hash, status, expires_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		k.ID, k.APIClientID, k.KeyPrefix, k.KeyHash, string(k.Status), k.ExpiresAt, k.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert api_client_key: %w", err)
	}
	return nil
}

func (r *KeyRepo) FindByHash(ctx context.Context, hash string) (*domain.APIClientKey, *domain.APIClient, error) {
	var (
		k            domain.APIClientKey
		c            domain.APIClient
		kStatus, cSt string
	)
	err := r.pool.QueryRow(ctx,
		`SELECT k.id::text, k.api_client_id::text, k.key_prefix, k.key_hash, k.status,
		        k.expires_at, k.revoked_at, k.created_at,
		        c.id::text, c.client_id, c.name, c.status, c.created_at, c.updated_at
		   FROM api_client_keys k
		   JOIN api_clients c ON c.id = k.api_client_id
		  WHERE k.key_hash = $1`, hash).
		Scan(&k.ID, &k.APIClientID, &k.KeyPrefix, &k.KeyHash, &kStatus,
			&k.ExpiresAt, &k.RevokedAt, &k.CreatedAt,
			&c.ID, &c.ClientID, &c.Name, &cSt, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		if isNoRows(err) {
			return nil, nil, domain.NotFound("key not found")
		}
		return nil, nil, fmt.Errorf("find api_client_key: %w", err)
	}
	k.Status = domain.KeyStatus(kStatus)
	c.Status = domain.ClientStatus(cSt)
	return &k, &c, nil
}

func (r *KeyRepo) ListByClient(ctx context.Context, clientUUID string) ([]domain.APIClientKey, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+keyCols+` FROM api_client_keys WHERE api_client_id = $1 ORDER BY created_at DESC`, clientUUID)
	if err != nil {
		return nil, fmt.Errorf("list api_client_keys: %w", err)
	}
	defer rows.Close()

	out := []domain.APIClientKey{}
	for rows.Next() {
		var k domain.APIClientKey
		if err := scanKey(rows, &k); err != nil {
			return nil, fmt.Errorf("scan api_client_key: %w", err)
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (r *KeyRepo) Revoke(ctx context.Context, clientUUID, keyID string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE api_client_keys
		    SET status = 'REVOKED', revoked_at = COALESCE(revoked_at, now())
		  WHERE id = $1 AND api_client_id = $2`, keyID, clientUUID)
	if err != nil {
		return fmt.Errorf("revoke api_client_key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.NotFound("key not found")
	}
	return nil
}
