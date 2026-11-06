package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"dms/internal/domain"
)

const docCols = `id::text, api_client_id::text, COALESCE(external_ref, ''), original_name, mime_type,
	size_bytes, checksum_sha256, storage_key, scan_status, status, current_version, metadata,
	created_at, updated_at`

func scanDocument(s scanner, d *domain.Document) error {
	var scanStatus, status string
	if err := s.Scan(&d.ID, &d.APIClientID, &d.ExternalRef, &d.OriginalName, &d.MimeType,
		&d.SizeBytes, &d.ChecksumSHA256, &d.StorageKey, &scanStatus, &status, &d.CurrentVersion,
		&d.Metadata, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return err
	}
	d.ScanStatus = domain.ScanStatus(scanStatus)
	d.Status = domain.DocumentStatus(status)
	if d.Metadata == nil {
		d.Metadata = map[string]any{}
	}
	return nil
}

const verCols = `v.id::text, v.document_id::text, v.version_number, v.original_name, v.mime_type,
	v.size_bytes, v.checksum_sha256, v.storage_key, v.scan_status, COALESCE(v.scan_signature, ''),
	v.scan_attempts, v.scanned_at, v.created_at`

func scanVersion(s scanner, v *domain.DocumentVersion) error {
	var scanStatus string
	if err := s.Scan(&v.ID, &v.DocumentID, &v.VersionNumber, &v.OriginalName, &v.MimeType,
		&v.SizeBytes, &v.ChecksumSHA256, &v.StorageKey, &scanStatus, &v.ScanSignature,
		&v.ScanAttempts, &v.ScannedAt, &v.CreatedAt); err != nil {
		return err
	}
	v.ScanStatus = domain.ScanStatus(scanStatus)
	return nil
}

type DocumentRepo struct{ pool *pgxpool.Pool }

func NewDocumentRepo(pool *pgxpool.Pool) *DocumentRepo { return &DocumentRepo{pool: pool} }

func insertVersion(ctx context.Context, q querier, v *domain.DocumentVersion) error {
	_, err := q.Exec(ctx,
		`INSERT INTO document_versions
		   (id, document_id, version_number, original_name, mime_type, size_bytes,
		    checksum_sha256, storage_key, scan_status, scan_attempts, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'PENDING', 0, $9, $9)`,
		v.ID, v.DocumentID, v.VersionNumber, v.OriginalName, v.MimeType, v.SizeBytes,
		v.ChecksumSHA256, v.StorageKey, v.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert document_version: %w", err)
	}
	return nil
}

func (r *DocumentRepo) CreateWithVersion(ctx context.Context, d *domain.Document, v *domain.DocumentVersion) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx,
		`INSERT INTO documents
		   (id, api_client_id, external_ref, original_name, mime_type, size_bytes, checksum_sha256,
		    storage_key, scan_status, status, current_version, metadata, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'PENDING', 'PENDING', 1, $9, $10, $10)`,
		d.ID, d.APIClientID, nullIfEmpty(d.ExternalRef), d.OriginalName, d.MimeType, d.SizeBytes,
		d.ChecksumSHA256, d.StorageKey, jsonOrEmpty(d.Metadata), d.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert document: %w", err)
	}

	v.VersionNumber = 1
	if err := insertVersion(ctx, tx, v); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func (r *DocumentRepo) AddVersion(ctx context.Context, clientUUID, docID string, v *domain.DocumentVersion) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Row lock serialises concurrent uploads to the same document so version
	// numbers are gap-free and unique.
	var current int
	err = tx.QueryRow(ctx,
		`SELECT current_version FROM documents
		  WHERE id = $1 AND api_client_id = $2 AND status <> 'DELETED'
		  FOR UPDATE`, docID, clientUUID).Scan(&current)
	if err != nil {
		if isNoRows(err) {
			return domain.NotFound("document not found")
		}
		return fmt.Errorf("lock document: %w", err)
	}

	v.DocumentID = docID
	v.VersionNumber = current + 1
	if err := insertVersion(ctx, tx, v); err != nil {
		return err
	}
	if err := refreshDocumentState(ctx, tx, docID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func (r *DocumentRepo) GetByID(ctx context.Context, clientUUID, id string) (*domain.Document, error) {
	var d domain.Document
	err := scanDocument(r.pool.QueryRow(ctx,
		`SELECT `+docCols+` FROM documents
		  WHERE id = $1 AND api_client_id = $2 AND status <> 'DELETED'`, id, clientUUID), &d)
	if err != nil {
		if isNoRows(err) {
			return nil, domain.NotFound("document not found")
		}
		return nil, fmt.Errorf("get document: %w", err)
	}
	return &d, nil
}

func (r *DocumentRepo) List(ctx context.Context, clientUUID string, f domain.DocumentFilter) ([]domain.Document, int, error) {
	where := []string{"api_client_id = $1", "status <> 'DELETED'"}
	args := []any{clientUUID}
	if f.Status != "" {
		args = append(args, f.Status)
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	if f.ExternalRef != "" {
		args = append(args, f.ExternalRef)
		where = append(where, fmt.Sprintf("external_ref = $%d", len(args)))
	}
	cond := strings.Join(where, " AND ")

	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM documents WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count documents: %w", err)
	}

	args = append(args, f.Limit, f.Offset)
	q := fmt.Sprintf(`SELECT %s FROM documents WHERE %s ORDER BY created_at DESC, id LIMIT $%d OFFSET $%d`,
		docCols, cond, len(args)-1, len(args))
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list documents: %w", err)
	}
	defer rows.Close()

	out := []domain.Document{}
	for rows.Next() {
		var d domain.Document
		if err := scanDocument(rows, &d); err != nil {
			return nil, 0, fmt.Errorf("scan document: %w", err)
		}
		out = append(out, d)
	}
	return out, total, rows.Err()
}

func (r *DocumentRepo) UpdateDescriptors(ctx context.Context, clientUUID string, d *domain.Document) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE documents SET external_ref = $3, metadata = $4, updated_at = now()
		  WHERE id = $1 AND api_client_id = $2 AND status <> 'DELETED'`,
		d.ID, clientUUID, nullIfEmpty(d.ExternalRef), jsonOrEmpty(d.Metadata))
	if err != nil {
		return fmt.Errorf("update document: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.NotFound("document not found")
	}
	return nil
}

func (r *DocumentRepo) SoftDelete(ctx context.Context, clientUUID, id string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE documents SET status = 'DELETED', deleted_at = now(), updated_at = now()
		  WHERE id = $1 AND api_client_id = $2 AND status <> 'DELETED'`, id, clientUUID)
	if err != nil {
		return fmt.Errorf("delete document: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.NotFound("document not found")
	}
	return nil
}

func (r *DocumentRepo) ListVersions(ctx context.Context, clientUUID, docID string) ([]domain.DocumentVersion, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+verCols+`
		   FROM document_versions v
		   JOIN documents d ON d.id = v.document_id
		  WHERE v.document_id = $1 AND d.api_client_id = $2 AND d.status <> 'DELETED'
		  ORDER BY v.version_number DESC`, docID, clientUUID)
	if err != nil {
		return nil, fmt.Errorf("list versions: %w", err)
	}
	defer rows.Close()

	out := []domain.DocumentVersion{}
	for rows.Next() {
		var v domain.DocumentVersion
		if err := scanVersion(rows, &v); err != nil {
			return nil, fmt.Errorf("scan version: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *DocumentRepo) GetVersion(ctx context.Context, clientUUID, docID string, number int) (*domain.DocumentVersion, error) {
	var v domain.DocumentVersion
	err := scanVersion(r.pool.QueryRow(ctx,
		`SELECT `+verCols+`
		   FROM document_versions v
		   JOIN documents d ON d.id = v.document_id
		  WHERE v.document_id = $1 AND d.api_client_id = $2 AND d.status <> 'DELETED'
		    AND v.version_number = $3`, docID, clientUUID, number), &v)
	if err != nil {
		if isNoRows(err) {
			return nil, domain.NotFound("document version not found")
		}
		return nil, fmt.Errorf("get version: %w", err)
	}
	return &v, nil
}

func (r *DocumentRepo) GetLatestCleanVersion(ctx context.Context, clientUUID, docID string) (*domain.DocumentVersion, error) {
	var v domain.DocumentVersion
	err := scanVersion(r.pool.QueryRow(ctx,
		`SELECT `+verCols+`
		   FROM document_versions v
		   JOIN documents d ON d.id = v.document_id
		  WHERE v.document_id = $1 AND d.api_client_id = $2 AND d.status <> 'DELETED'
		    AND v.scan_status = 'CLEAN'
		  ORDER BY v.version_number DESC
		  LIMIT 1`, docID, clientUUID), &v)
	if err != nil {
		if isNoRows(err) {
			return nil, domain.NotFound("no clean version available")
		}
		return nil, fmt.Errorf("get latest clean version: %w", err)
	}
	return &v, nil
}
