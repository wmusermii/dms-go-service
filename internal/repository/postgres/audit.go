package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"dms/internal/domain"
)

type AuditRepo struct{ pool *pgxpool.Pool }

func NewAuditRepo(pool *pgxpool.Pool) *AuditRepo { return &AuditRepo{pool: pool} }

func (r *AuditRepo) Insert(ctx context.Context, l *domain.AuditLog) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO document_audit_logs
		   (id, api_client_id, document_id, action, actor_id, actor_type, outcome, metadata, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		l.ID, l.APIClientID, nullIfEmpty(l.DocumentID), l.Action, nullIfEmpty(l.ActorID),
		l.ActorType, l.Outcome, jsonOrEmpty(l.Metadata), l.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert audit log: %w", err)
	}
	return nil
}
