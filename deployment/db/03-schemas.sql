-- Create schemas table
CREATE TABLE IF NOT EXISTS schemas (
    id SERIAL PRIMARY KEY,
    domain VARCHAR(255) NOT NULL,
    entity VARCHAR(255) NOT NULL,
    version VARCHAR(64) NOT NULL,
    definition JSONB NOT NULL,
    published_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    signature VARCHAR(512),
    checksum VARCHAR(64),
    size BIGINT DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Create unique index on domain, entity, and version
CREATE UNIQUE INDEX IF NOT EXISTS idx_schema_ver ON schemas (domain, entity, version);
