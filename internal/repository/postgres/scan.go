package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"dms/internal/domain"
)

type ScanRepo struct{ pool *pgxpool.Pool }

func NewScanRepo(pool *pgxpool.Pool) *ScanRepo { return &ScanRepo{pool: pool} }

func (r *ScanRepo) ClaimPending(ctx context.Context, limit int, staleAfter time.Duration) ([]domain.ScanJob, error) {
	rows, err := r.pool.Query(ctx, `
		UPDATE document_versions v
		   SET scan_status = 'SCANNING',
		       scan_attempts = v.scan_attempts + 1,
		       updated_at = now()
		  FROM documents d
		 WHERE d.id = v.document_id
		   AND v.id IN (
		       SELECT id FROM document_versions
		        WHERE scan_status = 'PENDING'
		           OR (scan_status = 'SCANNING' AND updated_at < now() - make_interval(secs => $2))
		        ORDER BY created_at
		        LIMIT $1
		        FOR UPDATE SKIP LOCKED)
		RETURNING v.id::text, v.document_id::text, d.api_client_id::text, v.storage_key, v.scan_attempts`,
		limit, staleAfter.Seconds())
	if err != nil {
		return nil, fmt.Errorf("claim scan jobs: %w", err)
	}
	defer rows.Close()

	var jobs []domain.ScanJob
	for rows.Next() {
		var j domain.ScanJob
		if err := rows.Scan(&j.VersionID, &j.DocumentID, &j.APIClientID, &j.StorageKey, &j.Attempts); err != nil {
			return nil, fmt.Errorf("scan job row: %w", err)
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

func (r *ScanRepo) CompleteScan(ctx context.Context, job domain.ScanJob, status domain.ScanStatus, signature string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`UPDATE document_versions
		    SET scan_status = $2, scan_signature = $3, scanned_at = now(), updated_at = now()
		  WHERE id = $1`,
		job.VersionID, string(status), nullIfEmpty(signature)); err != nil {
		return fmt.Errorf("store scan result: %w", err)
	}
	if err := refreshDocumentState(ctx, tx, job.DocumentID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func (r *ScanRepo) ReleaseFailed(ctx context.Context, job domain.ScanJob, maxAttempts int) (domain.ScanStatus, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	err = tx.QueryRow(ctx,
		`UPDATE document_versions
		    SET scan_status = CASE WHEN scan_attempts >= $2 THEN 'ERROR' ELSE 'PENDING' END,
		        updated_at = now()
		  WHERE id = $1
		  RETURNING scan_status`, job.VersionID, maxAttempts).Scan(&status)
	if err != nil {
		return "", fmt.Errorf("release scan job: %w", err)
	}
	if err := refreshDocumentState(ctx, tx, job.DocumentID); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}
	return domain.ScanStatus(status), nil
}
