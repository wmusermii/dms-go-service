-- =============================================================
-- Generic Document Management Service (DMS)
-- PostgreSQL
--
-- Fresh-install schema.
-- No merchant_id, tenant_id, registration_id, or user table.
--
-- Ownership model:
--   api_clients -> documents
-- Each registered application owns an isolated document namespace.
-- A document UUID alone is NOT an authorization credential.
-- Every document operation must also scope by authenticated api_client_id.
-- =============================================================

BEGIN;

CREATE SCHEMA IF NOT EXISTS dms;

-- =============================================================
-- 1. API CLIENTS
-- One row represents one consuming application.
-- Each application has its own isolated document namespace.
-- =============================================================

CREATE TABLE IF NOT EXISTS dms.api_clients (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id       VARCHAR(100) NOT NULL UNIQUE,
    name            VARCHAR(150) NOT NULL,
    description     TEXT,
    status          VARCHAR(20) NOT NULL DEFAULT 'ACTIVE'
                    CHECK (status IN ('ACTIVE', 'INACTIVE')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE dms.api_clients IS
    'Registered consuming applications. Each client owns an isolated document namespace.';
COMMENT ON COLUMN dms.api_clients.client_id IS
    'Public application identifier; never use it as a secret.';


-- =============================================================
-- 2. API CLIENT KEYS
-- Supports overlapping keys during rotation.
-- Store only a SHA-256/HMAC-SHA-256 hex digest, never plaintext.
-- The plaintext key must be shown only once when generated.
-- =============================================================

CREATE TABLE IF NOT EXISTS dms.api_client_keys (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    api_client_id   UUID NOT NULL
                    REFERENCES dms.api_clients(id) ON DELETE CASCADE,
    key_name        VARCHAR(100) NOT NULL DEFAULT 'default',
    key_prefix      VARCHAR(20) NOT NULL,
    key_hash        CHAR(64) NOT NULL UNIQUE,
    status          VARCHAR(20) NOT NULL DEFAULT 'ACTIVE'
                    CHECK (status IN ('ACTIVE', 'REVOKED', 'EXPIRED')),
    expires_at      TIMESTAMPTZ,
    revoked_at      TIMESTAMPTZ,
    last_used_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_dms_api_client_keys_hash_hex
        CHECK (key_hash ~ '^[0-9a-fA-F]{64}$'),

    CONSTRAINT chk_dms_api_client_keys_revoked
        CHECK (status <> 'REVOKED' OR revoked_at IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS idx_dms_api_client_keys_client
    ON dms.api_client_keys (api_client_id);

CREATE INDEX IF NOT EXISTS idx_dms_api_client_keys_active
    ON dms.api_client_keys (api_client_id, expires_at)
    WHERE status = 'ACTIVE' AND revoked_at IS NULL;

COMMENT ON TABLE dms.api_client_keys IS
    'Application credentials. Supports key rotation; plaintext API keys must never be stored.';


-- =============================================================
-- 3. DOCUMENTS
-- The application that uploaded a file owns it.
-- The consuming application stores the returned document UUID.
-- No business-specific identifiers are required by DMS.
-- =============================================================

CREATE TABLE IF NOT EXISTS dms.documents (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    api_client_id       UUID NOT NULL
                        REFERENCES dms.api_clients(id) ON DELETE RESTRICT,

    original_name       VARCHAR(255) NOT NULL,
    stored_name         VARCHAR(255) NOT NULL,
    file_extension      VARCHAR(20),
    mime_type           VARCHAR(120) NOT NULL,
    size_bytes          BIGINT NOT NULL CHECK (size_bytes >= 0),
    checksum_sha256     CHAR(64) NOT NULL,
    storage_key         TEXT NOT NULL UNIQUE,

    current_version     INTEGER NOT NULL DEFAULT 1
                        CHECK (current_version >= 1),

    -- ACTIVE / DELETED controls logical availability.
    status              VARCHAR(20) NOT NULL DEFAULT 'ACTIVE'
                        CHECK (status IN ('ACTIVE', 'DELETED')),

    -- Fail closed: preview/download should be allowed only when CLEAN.
    scan_status         VARCHAR(20) NOT NULL DEFAULT 'PENDING'
                        CHECK (scan_status IN (
                            'PENDING',
                            'CLEAN',
                            'INFECTED',
                            'ERROR',
                            'NOT_SCANNED'
                        )),
    scanned_at          TIMESTAMPTZ,
    scan_engine         VARCHAR(100),
    scan_details        JSONB,

    -- Optional external reference supplied by the consuming application.
    -- DMS does not register or validate users/actors.
    uploaded_by         VARCHAR(150),

    -- Optional generic metadata; no business schema is imposed by DMS.
    metadata            JSONB NOT NULL DEFAULT '{}'::JSONB,

    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at          TIMESTAMPTZ,

    CONSTRAINT chk_dms_documents_checksum_hex
        CHECK (checksum_sha256 ~ '^[0-9a-fA-F]{64}$'),

    CONSTRAINT chk_dms_documents_deleted_at
        CHECK (
            (status = 'DELETED' AND deleted_at IS NOT NULL)
            OR
            (status = 'ACTIVE' AND deleted_at IS NULL)
        )
);

CREATE INDEX IF NOT EXISTS idx_dms_documents_client_created
    ON dms.documents (api_client_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_dms_documents_client_status
    ON dms.documents (api_client_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_dms_documents_client_scan
    ON dms.documents (api_client_id, scan_status);

CREATE INDEX IF NOT EXISTS idx_dms_documents_client_checksum
    ON dms.documents (api_client_id, checksum_sha256);

CREATE INDEX IF NOT EXISTS idx_dms_documents_metadata
    ON dms.documents USING GIN (metadata);

COMMENT ON TABLE dms.documents IS
    'Generic file metadata. Every read/update/delete must be scoped by authenticated api_client_id.';
COMMENT ON COLUMN dms.documents.id IS
    'Opaque document identifier returned to the consuming application. UUID is not a substitute for authorization.';
COMMENT ON COLUMN dms.documents.storage_key IS
    'Private storage location. Never expose the physical path to consuming applications.';
COMMENT ON COLUMN dms.documents.scan_status IS
    'Preview/download policy: allow only CLEAN files. PENDING, INFECTED, ERROR and NOT_SCANNED are not downloadable.';


-- =============================================================
-- 4. DOCUMENT VERSIONS
-- Retains file history if versioning is enabled by the service.
-- Each version has its own storage key and security scan result.
-- =============================================================

CREATE TABLE IF NOT EXISTS dms.document_versions (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id         UUID NOT NULL
                        REFERENCES dms.documents(id) ON DELETE RESTRICT,
    version_number      INTEGER NOT NULL CHECK (version_number >= 1),
    storage_key         TEXT NOT NULL UNIQUE,
    original_name       VARCHAR(255) NOT NULL,
    mime_type           VARCHAR(120) NOT NULL,
    size_bytes          BIGINT NOT NULL CHECK (size_bytes >= 0),
    checksum_sha256     CHAR(64) NOT NULL,
    scan_status         VARCHAR(20) NOT NULL DEFAULT 'PENDING'
                        CHECK (scan_status IN (
                            'PENDING',
                            'CLEAN',
                            'INFECTED',
                            'ERROR',
                            'NOT_SCANNED'
                        )),
    scanned_at          TIMESTAMPTZ,
    scan_engine         VARCHAR(100),
    created_by          VARCHAR(150),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_dms_document_versions_document_version
        UNIQUE (document_id, version_number),

    CONSTRAINT chk_dms_document_versions_checksum_hex
        CHECK (checksum_sha256 ~ '^[0-9a-fA-F]{64}$')
);

CREATE INDEX IF NOT EXISTS idx_dms_document_versions_document
    ON dms.document_versions (document_id, version_number DESC);


-- =============================================================
-- 5. DOCUMENT AUDIT LOGS
-- Actor is an external reference; no DMS user table is required.
-- Keep logs even when a document is logically deleted.
-- =============================================================

CREATE TABLE IF NOT EXISTS dms.document_audit_logs (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    api_client_id       UUID NOT NULL
                        REFERENCES dms.api_clients(id) ON DELETE RESTRICT,
    document_id         UUID
                        REFERENCES dms.documents(id) ON DELETE SET NULL,

    action              VARCHAR(50) NOT NULL,
    outcome             VARCHAR(20) NOT NULL DEFAULT 'SUCCESS'
                        CHECK (outcome IN ('SUCCESS', 'DENIED', 'FAILED')),

    actor_id            VARCHAR(150),
    actor_type          VARCHAR(20)
                        CHECK (actor_type IS NULL OR actor_type IN (
                            'USER', 'SERVICE', 'SYSTEM', 'UNKNOWN'
                        )),

    ip_address          INET,
    user_agent          TEXT,
    metadata            JSONB NOT NULL DEFAULT '{}'::JSONB,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_dms_audit_client_created
    ON dms.document_audit_logs (api_client_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_dms_audit_document_created
    ON dms.document_audit_logs (document_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_dms_audit_action_created
    ON dms.document_audit_logs (action, created_at DESC);


-- =============================================================
-- 6. OPTIONAL SEED
-- Creates a local development client only.
-- No API key is seeded because plaintext credentials must be
-- generated securely and displayed only once by an admin/CLI tool.
-- Remove this block for production deployments if desired.
-- =============================================================

INSERT INTO dms.api_clients (
    client_id,
    name,
    description,
    status
)
VALUES (
    'local-dev-app',
    'Local Development Application',
    'Development-only API client. Generate and register its API key separately.',
    'ACTIVE'
)
ON CONFLICT (client_id) DO NOTHING;

COMMIT;
