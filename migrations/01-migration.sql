-- =====================================================================
-- DMS (Document Management Service) — migration + seed, satu file.
-- PostgreSQL 13+ (memakai gen_random_uuid() bawaan).
--
-- Menjalankan (database kosong / belum ada schema dms):
--   psql -h localhost -U <user> -d <database> -v ON_ERROR_STOP=1 -f 01-migration.sql
--
-- Semua objek memakai nama schema eksplisit (dms.*), jadi tidak bergantung
-- pada search_path. Aplikasi membaca schema ini lewat
--   DATABASE_URL=...?search_path=dms,public
--
-- File ini sengaja TIDAK memakai IF NOT EXISTS pada tabel: jika schema dms
-- lama masih ada, script gagal (dan di-rollback) alih-alih diam-diam
-- melewati tabel yang strukturnya berbeda. Untuk mengulang dari nol:
--   DROP SCHEMA dms CASCADE;      -- MENGHAPUS SEMUA DATA di schema dms
-- =====================================================================

BEGIN;

CREATE SCHEMA dms;

-- ---------------------------------------------------------------------
-- 1. api_clients — satu baris = satu aplikasi pengguna DMS (tenant).
--    Setiap aplikasi memiliki ruang dokumen yang terisolasi.
-- ---------------------------------------------------------------------
CREATE TABLE dms.api_clients (
    id          uuid         PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id   varchar(100) NOT NULL,
    name        varchar(255) NOT NULL,
    status      varchar(20)  NOT NULL DEFAULT 'ACTIVE',
    created_at  timestamptz  NOT NULL DEFAULT now(),
    updated_at  timestamptz  NOT NULL DEFAULT now(),

    CONSTRAINT uq_api_clients_client_id UNIQUE (client_id),
    CONSTRAINT chk_api_clients_status CHECK (status IN ('ACTIVE', 'SUSPENDED'))
);

COMMENT ON TABLE  dms.api_clients           IS 'Aplikasi yang terdaftar. Setiap client memiliki ruang dokumen yang terisolasi.';
COMMENT ON COLUMN dms.api_clients.client_id IS 'Identifier publik aplikasi (header X-Client-Id). Bukan rahasia.';

-- ---------------------------------------------------------------------
-- 2. api_client_keys — kredensial aplikasi. Hanya hash SHA-256 (hex) yang
--    disimpan; kunci asli tampil sekali saat diterbitkan.
-- ---------------------------------------------------------------------
CREATE TABLE dms.api_client_keys (
    id            uuid         PRIMARY KEY DEFAULT gen_random_uuid(),
    api_client_id uuid         NOT NULL REFERENCES dms.api_clients (id),
    key_prefix    varchar(16)  NOT NULL,
    key_hash      varchar(64)  NOT NULL,
    status        varchar(20)  NOT NULL DEFAULT 'ACTIVE',
    expires_at    timestamptz,
    revoked_at    timestamptz,
    created_at    timestamptz  NOT NULL DEFAULT now(),

    CONSTRAINT uq_api_client_keys_key_hash UNIQUE (key_hash),
    CONSTRAINT chk_api_client_keys_status CHECK (status IN ('ACTIVE', 'REVOKED')),
    CONSTRAINT chk_api_client_keys_hash_hex CHECK (key_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT chk_api_client_keys_revoked CHECK (status <> 'REVOKED' OR revoked_at IS NOT NULL)
);

CREATE INDEX idx_api_client_keys_client ON dms.api_client_keys (api_client_id);

COMMENT ON COLUMN dms.api_client_keys.key_prefix IS 'Beberapa karakter awal kunci, hanya untuk identifikasi manusia.';
COMMENT ON COLUMN dms.api_client_keys.key_hash   IS 'SHA-256 hex dari kunci lengkap. Plaintext tidak pernah disimpan.';

-- ---------------------------------------------------------------------
-- 3. documents — metadata dokumen. Kolom file mencerminkan versi TERBARU.
--    Setiap query harus dibatasi api_client_id yang terautentikasi.
--
--    status      : PENDING   belum ada versi yang lolos scan
--                  AVAILABLE ada >= 1 versi CLEAN (boleh diunduh)
--                  REJECTED  semua versi INFECTED / ERROR
--                  DELETED   soft delete
--    scan_status : status scan versi terbaru
-- ---------------------------------------------------------------------
CREATE TABLE dms.documents (
    id              uuid         PRIMARY KEY DEFAULT gen_random_uuid(),
    api_client_id   uuid         NOT NULL REFERENCES dms.api_clients (id),
    external_ref    varchar(255),
    original_name   varchar(255) NOT NULL,
    mime_type       varchar(150) NOT NULL,
    size_bytes      bigint       NOT NULL,
    checksum_sha256 char(64)     NOT NULL,
    storage_key     text         NOT NULL,
    scan_status     varchar(20)  NOT NULL DEFAULT 'PENDING',
    status          varchar(20)  NOT NULL DEFAULT 'PENDING',
    current_version integer      NOT NULL DEFAULT 1,
    metadata        jsonb        NOT NULL DEFAULT '{}'::jsonb,
    created_at      timestamptz  NOT NULL DEFAULT now(),
    updated_at      timestamptz  NOT NULL DEFAULT now(),
    deleted_at      timestamptz,

    CONSTRAINT uq_documents_storage_key UNIQUE (storage_key),
    CONSTRAINT chk_documents_scan_status CHECK (scan_status IN ('PENDING', 'SCANNING', 'CLEAN', 'INFECTED', 'ERROR')),
    CONSTRAINT chk_documents_status CHECK (status IN ('PENDING', 'AVAILABLE', 'REJECTED', 'DELETED')),
    CONSTRAINT chk_documents_size CHECK (size_bytes > 0),
    CONSTRAINT chk_documents_checksum_hex CHECK (checksum_sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT chk_documents_current_version CHECK (current_version >= 1),
    CONSTRAINT chk_documents_deleted_at CHECK ((status = 'DELETED') = (deleted_at IS NOT NULL))
);

CREATE INDEX idx_documents_client_created
    ON dms.documents (api_client_id, created_at DESC)
    WHERE status <> 'DELETED';

CREATE INDEX idx_documents_client_extref
    ON dms.documents (api_client_id, external_ref)
    WHERE external_ref IS NOT NULL;

COMMENT ON TABLE  dms.documents              IS 'Metadata dokumen. Setiap baca/ubah/hapus HARUS dibatasi api_client_id yang terautentikasi.';
COMMENT ON COLUMN dms.documents.id           IS 'Identifier opak yang dikembalikan ke aplikasi. UUID saja bukan otorisasi.';
COMMENT ON COLUMN dms.documents.external_ref IS 'Referensi bebas dari aplikasi pemanggil (mis. id pengguna). Tidak unik; satu referensi boleh dipakai banyak dokumen.';
COMMENT ON COLUMN dms.documents.storage_key  IS 'Lokasi fisik di storage privat. Jangan pernah diekspos ke aplikasi pemanggil.';

-- ---------------------------------------------------------------------
-- 4. document_versions — riwayat file; tiap versi punya storage_key dan
--    hasil scan sendiri. Hanya versi CLEAN yang pernah dilayani.
-- ---------------------------------------------------------------------
CREATE TABLE dms.document_versions (
    id              uuid         PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id     uuid         NOT NULL REFERENCES dms.documents (id),
    version_number  integer      NOT NULL,
    original_name   varchar(255) NOT NULL,
    mime_type       varchar(150) NOT NULL,
    size_bytes      bigint       NOT NULL,
    checksum_sha256 char(64)     NOT NULL,
    storage_key     text         NOT NULL,
    scan_status     varchar(20)  NOT NULL DEFAULT 'PENDING',
    scan_signature  varchar(255),
    scan_attempts   integer      NOT NULL DEFAULT 0,
    scanned_at      timestamptz,
    created_at      timestamptz  NOT NULL DEFAULT now(),
    updated_at      timestamptz  NOT NULL DEFAULT now(),

    CONSTRAINT uq_document_versions_number UNIQUE (document_id, version_number),
    CONSTRAINT uq_document_versions_storage_key UNIQUE (storage_key),
    CONSTRAINT chk_document_versions_scan_status CHECK (scan_status IN ('PENDING', 'SCANNING', 'CLEAN', 'INFECTED', 'ERROR')),
    CONSTRAINT chk_document_versions_size CHECK (size_bytes > 0),
    CONSTRAINT chk_document_versions_checksum_hex CHECK (checksum_sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT chk_document_versions_number CHECK (version_number >= 1)
);

-- Mempercepat antrean scan worker ("ambil job berikutnya").
CREATE INDEX idx_document_versions_scan_queue
    ON dms.document_versions (created_at)
    WHERE scan_status IN ('PENDING', 'SCANNING');

COMMENT ON COLUMN dms.document_versions.scan_attempts IS 'Jumlah percobaan scan; worker berhenti di SCAN_MAX_ATTEMPTS lalu menandai ERROR.';

-- ---------------------------------------------------------------------
-- 5. document_audit_logs — jejak audit. actor_id adalah referensi
--    eksternal (header X-Actor-Id); DMS tidak punya tabel user.
-- ---------------------------------------------------------------------
CREATE TABLE dms.document_audit_logs (
    id            uuid         PRIMARY KEY DEFAULT gen_random_uuid(),
    api_client_id uuid         NOT NULL REFERENCES dms.api_clients (id),
    document_id   uuid         REFERENCES dms.documents (id),
    action        varchar(50)  NOT NULL,
    actor_id      varchar(255),
    actor_type    varchar(20)  NOT NULL,
    outcome       varchar(20)  NOT NULL,
    metadata      jsonb        NOT NULL DEFAULT '{}'::jsonb,
    created_at    timestamptz  NOT NULL DEFAULT now(),

    CONSTRAINT chk_audit_actor_type CHECK (actor_type IN ('CLIENT', 'USER', 'SYSTEM')),
    CONSTRAINT chk_audit_outcome CHECK (outcome IN ('SUCCESS', 'DENIED', 'FAILURE'))
);

CREATE INDEX idx_audit_client_created
    ON dms.document_audit_logs (api_client_id, created_at DESC);

CREATE INDEX idx_audit_document_created
    ON dms.document_audit_logs (document_id, created_at DESC)
    WHERE document_id IS NOT NULL;

-- =====================================================================
-- SEED (khusus development; hapus blok ini untuk production)
--
-- Hanya membuat client. API key SENGAJA tidak di-seed: kunci plaintext
-- harus dibuat secara acak dan hanya ditampilkan sekali. Terbitkan lewat
-- admin API setelah aplikasi berjalan:
--   POST /admin/v1/clients/00000000-0000-4000-8000-000000000001/keys
--   Authorization: Bearer <ADMIN_TOKEN>
-- Id client dibuat tetap agar mudah dipakai di Insomnia (client_uuid).
-- =====================================================================
INSERT INTO dms.api_clients (id, client_id, name, status)
VALUES ('00000000-0000-4000-8000-000000000001', 'local-dev-app', 'Local Development Application', 'ACTIVE')
ON CONFLICT (client_id) DO NOTHING;

COMMIT;
