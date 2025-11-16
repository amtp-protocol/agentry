-- Create agents table
CREATE TABLE IF NOT EXISTS agents (
    id SERIAL PRIMARY KEY,
    address VARCHAR(255) NOT NULL UNIQUE,
    delivery_mode VARCHAR(10) DEFAULT 'push',
    push_target VARCHAR(500),
    headers JSONB,
    api_key VARCHAR(255),
    supported_schemas JSONB,
    requires_schema BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_access TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Create index on agents address
CREATE INDEX IF NOT EXISTS idx_agents_address ON agents(address);

