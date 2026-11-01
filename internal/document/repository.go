package document

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("document not found")

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Create(ctx context.Context, doc *Document) error {
	query := `
	INSERT INTO dms.documents (
		original_name,
		mime_type,
		size_bytes,
		checksum_sha256,
		storage_key,
		current_version,
		document_type,
		entity_type,
		entity_id,
		merchant_id,
		registration_id,
		status,
		uploaded_by
	)
	VALUES (
		$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
	)
	RETURNING
		id,
		created_at,
		updated_at
`

	var uploadedBy *string

	if doc.UploadedBy != "" {
		uploadedBy = &doc.UploadedBy
	}

	err := r.pool.QueryRow(
		ctx,
		query,
		doc.OriginalName,
		doc.MimeType,
		doc.SizeBytes,
		doc.ChecksumSHA256,
		doc.StorageKey,
		doc.CurrentVersion,
		doc.DocumentType,
		doc.EntityType,
		doc.EntityID,
		doc.MerchantID,
		doc.RegistrationID,
		doc.Status,
		uploadedBy,
	).Scan(
		&doc.ID,
		&doc.CreatedAt,
		&doc.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("insert document: %w", err)
	}

	return nil
}

func (r *Repository) GetByID(ctx context.Context, id string) (*Document, error) {
	query := `
		SELECT
			id::text,
			original_name,
			mime_type,
			size_bytes,
			checksum_sha256,
			storage_key,
			current_version,
			document_type,
			entity_type,
			entity_id,
			status,
			uploaded_by::text,
			created_at,
			updated_at
		FROM dms.documents
		WHERE id = $1
			AND deleted_at IS NULL
	`

	var doc Document

	err := r.pool.QueryRow(ctx, query, id).Scan(
		&doc.ID,
		&doc.OriginalName,
		&doc.MimeType,
		&doc.SizeBytes,
		&doc.ChecksumSHA256,
		&doc.StorageKey,
		&doc.CurrentVersion,
		&doc.DocumentType,
		&doc.EntityType,
		&doc.EntityID,
		&doc.Status,
		&doc.UploadedBy,
		&doc.CreatedAt,
		&doc.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}

	if err != nil {
		return nil, err
	}

	return &doc, nil
}

func (r *Repository) List(ctx context.Context, limit, offset int) ([]Document, error) {
	query := `
		SELECT
			id::text,
			original_name,
			mime_type,
			size_bytes,
			checksum_sha256,
			storage_key,
			current_version,
			document_type,
			entity_type,
			entity_id,
			status,
			uploaded_by::text,
			created_at,
			updated_at
		FROM dms.documents
		WHERE deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := r.pool.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	documents := make([]Document, 0)

	for rows.Next() {
		var doc Document

		err := rows.Scan(
			&doc.ID,
			&doc.OriginalName,
			&doc.MimeType,
			&doc.SizeBytes,
			&doc.ChecksumSHA256,
			&doc.StorageKey,
			&doc.CurrentVersion,
			&doc.DocumentType,
			&doc.EntityType,
			&doc.EntityID,
			&doc.Status,
			&doc.UploadedBy,
			&doc.CreatedAt,
			&doc.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		documents = append(documents, doc)
	}

	return documents, rows.Err()
}

func (r *Repository) SoftDelete(ctx context.Context, id string) error {
	query := `
		UPDATE dms.documents
		SET
			status = 'DELETED',
			deleted_at = NOW(),
			updated_at = NOW()
		WHERE id = $1
			AND deleted_at IS NULL
	`

	commandTag, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return err
	}

	if commandTag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

func (r *Repository) GetStorageKey(ctx context.Context, id string) (string, error) {
	query := `
		SELECT storage_key
		FROM dms.documents
		WHERE id = $1
			AND deleted_at IS NULL
	`

	var storageKey string

	err := r.pool.QueryRow(ctx, query, id).Scan(&storageKey)

	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}

	if err != nil {
		return "", err
	}

	return storageKey, nil
}

func (r *Repository) TouchUpdatedAt(ctx context.Context, id string) error {
	query := `
		UPDATE dms.documents
		SET updated_at = $1
		WHERE id = $2
	`

	_, err := r.pool.Exec(ctx, query, time.Now(), id)

	return err
}
