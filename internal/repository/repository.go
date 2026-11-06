// Package repository declares the persistence contracts used by services.
// The PostgreSQL implementations live in repository/postgres.
//
// Every document query takes the owning client's UUID: a client can never see
// or touch another client's documents (they simply appear as "not found").
package repository

import (
	"context"
	"time"

	"dms/internal/domain"
)

type ClientRepository interface {
	// Create returns a Conflict error when client_id is already taken.
	Create(ctx context.Context, c *domain.APIClient) error
	List(ctx context.Context) ([]domain.APIClient, error)
	GetByID(ctx context.Context, id string) (*domain.APIClient, error)
}

type KeyRepository interface {
	Create(ctx context.Context, k *domain.APIClientKey) error
	// FindByHash returns the key together with its owning client.
	FindByHash(ctx context.Context, hash string) (*domain.APIClientKey, *domain.APIClient, error)
	ListByClient(ctx context.Context, clientUUID string) ([]domain.APIClientKey, error)
	Revoke(ctx context.Context, clientUUID, keyID string) error
}

type DocumentRepository interface {
	// CreateWithVersion inserts the document and its first version atomically.
	CreateWithVersion(ctx context.Context, d *domain.Document, v *domain.DocumentVersion) error
	// AddVersion appends a version (assigning v.VersionNumber) and refreshes the document.
	AddVersion(ctx context.Context, clientUUID, docID string, v *domain.DocumentVersion) error

	GetByID(ctx context.Context, clientUUID, id string) (*domain.Document, error)
	List(ctx context.Context, clientUUID string, f domain.DocumentFilter) ([]domain.Document, int, error)
	// UpdateDescriptors replaces external_ref and metadata.
	UpdateDescriptors(ctx context.Context, clientUUID string, d *domain.Document) error
	SoftDelete(ctx context.Context, clientUUID, id string) error

	ListVersions(ctx context.Context, clientUUID, docID string) ([]domain.DocumentVersion, error)
	GetVersion(ctx context.Context, clientUUID, docID string, number int) (*domain.DocumentVersion, error)
	GetLatestCleanVersion(ctx context.Context, clientUUID, docID string) (*domain.DocumentVersion, error)
}

// ScanRepository is the work queue used by the scan worker.
type ScanRepository interface {
	// ClaimPending atomically moves up to limit PENDING versions (and SCANNING
	// ones abandoned for longer than staleAfter) to SCANNING and returns them.
	// Safe to call from several worker instances (FOR UPDATE SKIP LOCKED).
	ClaimPending(ctx context.Context, limit int, staleAfter time.Duration) ([]domain.ScanJob, error)
	// CompleteScan stores the verdict (CLEAN or INFECTED) and refreshes the document.
	CompleteScan(ctx context.Context, job domain.ScanJob, status domain.ScanStatus, signature string) error
	// ReleaseFailed returns a failed job to PENDING for retry, or marks it ERROR
	// once maxAttempts is reached. It returns the resulting status.
	ReleaseFailed(ctx context.Context, job domain.ScanJob, maxAttempts int) (domain.ScanStatus, error)
}

type AuditRepository interface {
	Insert(ctx context.Context, l *domain.AuditLog) error
}
