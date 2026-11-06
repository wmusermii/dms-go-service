-- DMS initial schema (PostgreSQL 13+, uses built-in gen_random_uuid()).
-- Follows the project ERD; additions beyond the ERD are marked "(+)".

CREATE TABLE api_clients (
    id          uuid         PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id   varchar(100) NOT NULL,
    name        varchar(255) NOT NULL,
    status      varchar(20)  NOT NULL DEFAULT 'ACTIVE',
    created_at  timestamptz  NOT NULL DEFAULT now(),   -- (+)
    updated_at  timestamptz  NOT NULL DEFAULT now(),   -- (+)
    CONSTRAINT uq_api_clients_client_id UNIQUE (client_id),
    CONSTRAINT chk_api_clients_status CHECK (status IN ('ACTIVE', 'SUSPENDED'))
);

CREATE TABLE api_client_keys (
    id            uuid         PRIMARY KEY DEFAULT gen_random_uuid(),
    api_client_id uuid         NOT NULL REFERENCES api_clients (id),
    key_prefix    varchar(16)  NOT NULL,                -- (+) first chars of the key, for identification only
    key_hash      varchar(64)  NOT NULL,                -- hex SHA-256 of the full key
    status        varchar(20)  NOT NULL DEFAULT 'ACTIVE',
    expires_at    timestamptz,
    revoked_at    timestamptz,
    created_at    timestamptz  NOT NULL DEFAULT now(),  -- (+)
    CONSTRAINT uq_api_client_keys_key_hash UNIQUE (key_hash),
    CONSTRAINT chk_api_client_keys_status CHECK (status IN ('ACTIVE', 'REVOKED'))
);
CREATE INDEX idx_api_client_keys_client ON api_client_keys (api_client_id);

CREATE TABLE documents (
    id              uuid         PRIMARY KEY DEFAULT gen_random_uuid(),
    api_client_id   uuid         NOT NULL REFERENCES api_clients (id),
    external_ref    varchar(255),                       -- (+) caller's own reference, e.g. "order-1001"
    original_name   varchar(255) NOT NULL,
    mime_type       varchar(150) NOT NULL,
    size_bytes      bigint       NOT NULL,
    checksum_sha256 char(64)     NOT NULL,
    storage_key     text         NOT NULL,
    scan_status     varchar(20)  NOT NULL DEFAULT 'PENDING',
    status          varchar(20)  NOT NULL DEFAULT 'PENDING',
    current_version integer      NOT NULL DEFAULT 1,    -- (+)
    metadata        jsonb        NOT NULL DEFAULT '{}'::jsonb,
    created_at      timestamptz  NOT NULL DEFAULT now(),-- (+)
    updated_at      timestamptz  NOT NULL DEFAULT now(),-- (+)
    deleted_at      timestamptz,                        -- (+) soft delete
    CONSTRAINT chk_documents_scan_status CHECK (scan_status IN ('PENDING', 'SCANNING', 'CLEAN', 'INFECTED', 'ERROR')),
    CONSTRAINT chk_documents_status CHECK (status IN ('PENDING', 'AVAILABLE', 'REJECTED', 'DELETED')),
    CONSTRAINT chk_documents_size CHECK (size_bytes > 0)
);
CREATE INDEX idx_documents_client_created ON documents (api_client_id, created_at DESC) WHERE status <> 'DELETED';
CREATE INDEX idx_documents_client_extref ON documents (api_client_id, external_ref) WHERE external_ref IS NOT NULL;

CREATE TABLE document_versions (
    id              uuid         PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id     uuid         NOT NULL REFERENCES documents (id),
    version_number  integer      NOT NULL,
    original_name   varchar(255) NOT NULL,              -- (+) per-version file info
    mime_type       varchar(150) NOT NULL,              -- (+)
    size_bytes      bigint       NOT NULL,              -- (+)
    checksum_sha256 char(64)     NOT NULL,              -- (+)
    storage_key     text         NOT NULL,
    scan_status     varchar(20)  NOT NULL DEFAULT 'PENDING',
    scan_signature  varchar(255),                       -- (+) detected malware signature, if any
    scan_attempts   integer      NOT NULL DEFAULT 0,    -- (+) worker retry counter
    scanned_at      timestamptz,                        -- (+)
    created_at      timestamptz  NOT NULL DEFAULT now(),-- (+)
    updated_at      timestamptz  NOT NULL DEFAULT now(),-- (+)
    CONSTRAINT uq_document_versions_number UNIQUE (document_id, version_number),
    CONSTRAINT chk_document_versions_scan_status CHECK (scan_status IN ('PENDING', 'SCANNING', 'CLEAN', 'INFECTED', 'ERROR'))
);
-- Keeps the scan worker's "claim next job" query cheap.
CREATE INDEX idx_document_versions_scan_queue ON document_versions (created_at)
    WHERE scan_status IN ('PENDING', 'SCANNING');

CREATE TABLE document_audit_logs (
    id            uuid         PRIMARY KEY DEFAULT gen_random_uuid(),
    api_client_id uuid         NOT NULL REFERENCES api_clients (id),
    document_id   uuid         REFERENCES documents (id),
    action        varchar(50)  NOT NULL,
    actor_id      varchar(255),
    actor_type    varchar(20)  NOT NULL,
    outcome       varchar(20)  NOT NULL,
    metadata      jsonb        NOT NULL DEFAULT '{}'::jsonb,
    created_at    timestamptz  NOT NULL DEFAULT now()   -- (+)
);
CREATE INDEX idx_audit_client_created ON document_audit_logs (api_client_id, created_at DESC);
CREATE INDEX idx_audit_document_created ON document_audit_logs (document_id, created_at DESC) WHERE document_id IS NOT NULL;
