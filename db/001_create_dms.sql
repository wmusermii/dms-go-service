-- =============================================================
-- DMS Service Migration
-- Database : PostgreSQL
-- Schema   : dms
-- =============================================================

BEGIN;

CREATE SCHEMA IF NOT EXISTS dms;

-- =============================================================
-- TABLE: dms.documents
-- =============================================================

CREATE TABLE IF NOT EXISTS dms.documents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    original_name VARCHAR(255) NOT NULL,
    mime_type VARCHAR(120) NOT NULL,
    size_bytes BIGINT NOT NULL CHECK (size_bytes >= 0),
    checksum_sha256 CHAR(64),
    storage_key TEXT NOT NULL,
    current_version INTEGER NOT NULL DEFAULT 1,
    document_type VARCHAR(50),
    entity_type VARCHAR(50),
    entity_id VARCHAR(100),
    merchant_id VARCHAR(100),
    registration_id VARCHAR(100),
    status VARCHAR(20) NOT NULL DEFAULT 'ACTIVE',
    uploaded_by VARCHAR(100),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_dms_documents_merchant
    ON dms.documents (merchant_id);

CREATE INDEX IF NOT EXISTS idx_dms_documents_registration
    ON dms.documents (registration_id);

CREATE INDEX IF NOT EXISTS idx_dms_documents_entity
    ON dms.documents (entity_type, entity_id);

CREATE INDEX IF NOT EXISTS idx_dms_documents_status
    ON dms.documents (status);

CREATE INDEX IF NOT EXISTS idx_dms_documents_created_at
    ON dms.documents (created_at DESC);

CREATE INDEX IF NOT EXISTS idx_dms_documents_document_type
    ON dms.documents (document_type);

-- =============================================================
-- TABLE: dms.document_versions
-- =============================================================

CREATE TABLE IF NOT EXISTS dms.document_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id UUID NOT NULL REFERENCES dms.documents(id) ON DELETE CASCADE,
    version_number INTEGER NOT NULL,
    storage_key TEXT NOT NULL,
    checksum_sha256 CHAR(64),
    size_bytes BIGINT NOT NULL CHECK (size_bytes >= 0),
    mime_type VARCHAR(120) NOT NULL,
    created_by VARCHAR(100),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_dms_document_versions_document_version
        UNIQUE (document_id, version_number)
);

CREATE INDEX IF NOT EXISTS idx_dms_document_versions_document
    ON dms.document_versions (document_id);

-- =============================================================
-- TABLE: dms.document_audit_logs
-- =============================================================

CREATE TABLE IF NOT EXISTS dms.document_audit_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id UUID REFERENCES dms.documents(id) ON DELETE SET NULL,
    action VARCHAR(50) NOT NULL,
    actor_id VARCHAR(100),
    actor_role VARCHAR(50),
    ip_address VARCHAR(45),
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_dms_document_audit_logs_document
    ON dms.document_audit_logs (document_id);

CREATE INDEX IF NOT EXISTS idx_dms_document_audit_logs_action
    ON dms.document_audit_logs (action);

CREATE INDEX IF NOT EXISTS idx_dms_document_audit_logs_created_at
    ON dms.document_audit_logs (created_at DESC);

-- =============================================================
-- SEED: Merchant Registration
-- =============================================================

INSERT INTO dms.documents (
    id,
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
    uploaded_by,
    created_at,
    updated_at
)
VALUES
(
    '11111111-1111-4111-8111-111111111111',
    'seed-ktp.jpg',
    'image/jpeg',
    102400,
    '1111111111111111111111111111111111111111111111111111111111111111',
    'seeds/2026/11/05/11111111-1111-4111-8111-111111111111/v1_seed-ktp.jpg',
    1,
    'KTP',
    'MERCHANT_REGISTRATION',
    'REG-2026-001',
    'MRC-2026-001',
    'REG-2026-001',
    'ACTIVE',
    'merchant-user-123',
    NOW(),
    NOW()
),
(
    '22222222-2222-4222-8222-222222222222',
    'seed-npwp.pdf',
    'application/pdf',
    204800,
    '2222222222222222222222222222222222222222222222222222222222222222',
    'seeds/2026/11/05/22222222-2222-4222-8222-222222222222/v1_seed-npwp.pdf',
    1,
    'NPWP',
    'MERCHANT_REGISTRATION',
    'REG-2026-001',
    'MRC-2026-001',
    'REG-2026-001',
    'ACTIVE',
    'merchant-user-123',
    NOW(),
    NOW()
)
ON CONFLICT (id) DO NOTHING;

-- =============================================================
-- SEED: Document versions
-- =============================================================

INSERT INTO dms.document_versions (
    id,
    document_id,
    version_number,
    storage_key,
    checksum_sha256,
    size_bytes,
    mime_type,
    created_by,
    created_at
)
VALUES
(
    '33333333-3333-4333-8333-333333333333',
    '11111111-1111-4111-8111-111111111111',
    1,
    'seeds/2026/11/05/11111111-1111-4111-8111-111111111111/v1_seed-ktp.jpg',
    '1111111111111111111111111111111111111111111111111111111111111111',
    102400,
    'image/jpeg',
    'merchant-user-123',
    NOW()
),
(
    '44444444-4444-4444-8444-444444444444',
    '22222222-2222-4222-8222-222222222222',
    1,
    'seeds/2026/11/05/22222222-2222-4222-8222-222222222222/v1_seed-npwp.pdf',
    '2222222222222222222222222222222222222222222222222222222222222222',
    204800,
    'application/pdf',
    'merchant-user-123',
    NOW()
)
ON CONFLICT (document_id, version_number) DO NOTHING;

-- =============================================================
-- SEED: Audit logs
-- =============================================================

INSERT INTO dms.document_audit_logs (
    id,
    document_id,
    action,
    actor_id,
    actor_role,
    metadata,
    created_at
)
VALUES
(
    '55555555-5555-4555-8555-555555555555',
    '11111111-1111-4111-8111-111111111111',
    'SEED',
    'system',
    'SYSTEM',
    '{"source": "migration", "note": "initial seed data"}'::JSONB,
    NOW()
),
(
    '66666666-6666-4666-8666-666666666666',
    '22222222-2222-4222-8222-222222222222',
    'SEED',
    'system',
    'SYSTEM',
    '{"source": "migration", "note": "initial seed data"}'::JSONB,
    NOW()
)
ON CONFLICT (id) DO NOTHING;

COMMIT;