-- DMS initial schema (PostgreSQL 13+, uses built-in gen_random_uuid()).
-- Follows the project ERD; additions beyond the ERD are marked "(+)".
-- =========================================================
-- DMS Initial Schema
-- PostgreSQL 13+
-- Schema: dms
-- =========================================================

CREATE SCHEMA IF NOT EXISTS dms;

-- =========================================================
-- 1. API CLIENTS
-- =========================================================

CREATE TABLE dms.api_clients (
    id          uuid         PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id   varchar(100) NOT NULL,
    name        varchar(255) NOT NULL,
    status      varchar(20)  NOT NULL DEFAULT 'ACTIVE',
    created_at  timestamptz  NOT NULL DEFAULT now(),
    updated_at  timestamptz  NOT NULL DEFAULT now(),

    CONSTRAINT uq_api_clients_client_id UNIQUE (client_id),
    CONSTRAINT chk_api_clients_status
        CHECK (status IN ('ACTIVE', 'SUSPENDED'))
);

-- =========================================================
-- 2. API CLIENT KEYS
-- =========================================================

CREATE TABLE dms.api_client_keys (
    id            uuid         PRIMARY KEY DEFAULT gen_random_uuid(),
    api_client_id uuid         NOT NULL
                  REFERENCES dms.api_clients (id),
    key_prefix    varchar(16)  NOT NULL,
    key_hash      varchar(64)  NOT NULL,
    status        varchar(20)  NOT NULL DEFAULT 'ACTIVE',
    expires_at    timestamptz,
    revoked_at    timestamptz,
    created_at    timestamptz  NOT NULL DEFAULT now(),

    CONSTRAINT uq_api_client_keys_key_hash UNIQUE (key_hash),
    CONSTRAINT chk_api_client_keys_status
        CHECK (status IN ('ACTIVE', 'REVOKED'))
);

CREATE INDEX idx_api_client_keys_client
    ON dms.api_client_keys (api_client_id);

-- =========================================================
-- 3. DOCUMENTS
-- =========================================================

CREATE TABLE dms.documents (
    id              uuid         PRIMARY KEY DEFAULT gen_random_uuid(),
    api_client_id   uuid         NOT NULL
                    REFERENCES dms.api_clients (id),
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

    CONSTRAINT chk_documents_scan_status
        CHECK (scan_status IN (
            'PENDING', 'SCANNING', 'CLEAN', 'INFECTED', 'ERROR'
        )),
    CONSTRAINT chk_documents_status
        CHECK (status IN (
            'PENDING', 'AVAILABLE', 'REJECTED', 'DELETED'
        )),
    CONSTRAINT chk_documents_size
        CHECK (size_bytes > 0),
    CONSTRAINT chk_documents_current_version
        CHECK (current_version > 0)
);

CREATE INDEX idx_documents_client_created
    ON dms.documents (api_client_id, created_at DESC)
    WHERE status <> 'DELETED';

CREATE INDEX idx_documents_client_extref
    ON dms.documents (api_client_id, external_ref)
    WHERE external_ref IS NOT NULL;

-- =========================================================
-- 4. DOCUMENT VERSIONS
-- =========================================================

CREATE TABLE dms.document_versions (
    id              uuid         PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id     uuid         NOT NULL
                    REFERENCES dms.documents (id),
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

    CONSTRAINT uq_document_versions_number
        UNIQUE (document_id, version_number),
    CONSTRAINT chk_document_versions_scan_status
        CHECK (scan_status IN (
            'PENDING', 'SCANNING', 'CLEAN', 'INFECTED', 'ERROR'
        )),
    CONSTRAINT chk_document_versions_number
        CHECK (version_number > 0),
    CONSTRAINT chk_document_versions_size
        CHECK (size_bytes > 0),
    CONSTRAINT chk_document_versions_scan_attempts
        CHECK (scan_attempts >= 0)
);

CREATE INDEX idx_document_versions_scan_queue
    ON dms.document_versions (created_at)
    WHERE scan_status IN ('PENDING', 'SCANNING');

-- =========================================================
-- 5. DOCUMENT AUDIT LOGS
-- =========================================================

CREATE TABLE dms.document_audit_logs (
    id            uuid         PRIMARY KEY DEFAULT gen_random_uuid(),
    api_client_id uuid         NOT NULL
                  REFERENCES dms.api_clients (id),
    document_id   uuid
                  REFERENCES dms.documents (id),
    action        varchar(50)  NOT NULL,
    actor_id      varchar(255),
    actor_type    varchar(20)  NOT NULL,
    outcome       varchar(20)  NOT NULL,
    metadata      jsonb        NOT NULL DEFAULT '{}'::jsonb,
    created_at    timestamptz  NOT NULL DEFAULT now(),

    CONSTRAINT chk_document_audit_actor_type
        CHECK (actor_type IN ('USER', 'SERVICE', 'SYSTEM', 'UNKNOWN')),
    CONSTRAINT chk_document_audit_outcome
        CHECK (outcome IN ('SUCCESS', 'DENIED', 'FAILED'))
);

CREATE INDEX idx_audit_client_created ON dms.document_audit_logs (api_client_id, created_at DESC);

CREATE INDEX idx_audit_document_created ON dms.document_audit_logs (document_id, created_at DESC) WHERE document_id IS NOT NULL;

-- =========================================================
-- 6. DEFAULT PRIVILEGES / SEARCH PATH
-- =========================================================

-- Supaya koneksi database dapat menggunakan schema dms
-- tanpa harus menulis prefix dms. pada setiap query.
-- Sesuaikan nama user jika diperlukan.

-- ALTER ROLE dms_user SET search_path TO dms, public;

-- =========================================================
-- END OF DMS INITIAL SCHEMA
-- =========================================================