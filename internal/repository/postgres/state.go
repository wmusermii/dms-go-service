package postgres

import (
	"context"
	"fmt"
)

// refreshDocumentState re-derives a document row from its versions. It is the
// single place that decides document.status, so uploads and scan results can
// never drift apart:
//
//   - the descriptive columns (name, mime, size, checksum, key, scan_status,
//     current_version) mirror the LATEST uploaded version;
//   - status is AVAILABLE if any version is CLEAN (an older clean version keeps
//     being served while a newer one is still scanning), PENDING while at least
//     one version awaits scanning, otherwise REJECTED;
//   - a soft-deleted document stays DELETED.
//
// Must run inside the same transaction that changed the versions.
func refreshDocumentState(ctx context.Context, q querier, docID string) error {
	_, err := q.Exec(ctx, `
		UPDATE documents d SET
			original_name   = lv.original_name,
			mime_type       = lv.mime_type,
			size_bytes      = lv.size_bytes,
			checksum_sha256 = lv.checksum_sha256,
			storage_key     = lv.storage_key,
			scan_status     = lv.scan_status,
			current_version = lv.version_number,
			status = CASE
				WHEN d.status = 'DELETED' THEN 'DELETED'
				WHEN EXISTS (SELECT 1 FROM document_versions v
				              WHERE v.document_id = d.id AND v.scan_status = 'CLEAN') THEN 'AVAILABLE'
				WHEN EXISTS (SELECT 1 FROM document_versions v
				              WHERE v.document_id = d.id AND v.scan_status IN ('PENDING', 'SCANNING')) THEN 'PENDING'
				ELSE 'REJECTED'
			END,
			updated_at = now()
		FROM (
			SELECT original_name, mime_type, size_bytes, checksum_sha256, storage_key,
			       scan_status, version_number
			  FROM document_versions
			 WHERE document_id = $1
			 ORDER BY version_number DESC
			 LIMIT 1
		) lv
		WHERE d.id = $1`, docID)
	if err != nil {
		return fmt.Errorf("refresh document state: %w", err)
	}
	return nil
}
