-- Create enum type (with conditional check)
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'delivery_status') THEN
        CREATE TYPE delivery_status AS ENUM (
            'pending',
            'queued',
            'delivering',
            'delivered',
            'failed',
            'retrying'
        );
    END IF;
END $$;

-- Create main messages table
CREATE TABLE IF NOT EXISTS messages (
    id SERIAL PRIMARY KEY,
    version VARCHAR(10) NOT NULL DEFAULT '1.0',
    message_id UUID NOT NULL UNIQUE,
    idempotency_key UUID NOT NULL UNIQUE,
    timestamp TIMESTAMPTZ NOT NULL,
    sender VARCHAR(255) NOT NULL,
    subject TEXT,
    schema TEXT,
    in_reply_to UUID,
    response_type VARCHAR(50),

    -- JSON fields
    recipients JSONB NOT NULL,
    coordination JSONB,
    headers JSONB,
    payload JSONB,
    attachments JSONB,
    signature JSONB
);

-- Create message status table
CREATE TABLE IF NOT EXISTS message_statuses (
    id SERIAL PRIMARY KEY,
    message_id UUID NOT NULL REFERENCES messages(message_id) ON DELETE CASCADE,
    status delivery_status NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    next_retry TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    delivered_at TIMESTAMPTZ
);

-- Create recipient status table
CREATE TABLE IF NOT EXISTS recipient_statuses (
    id SERIAL PRIMARY KEY,
    message_id UUID NOT NULL REFERENCES messages(message_id) ON DELETE CASCADE,
    address VARCHAR(255) NOT NULL,
    status delivery_status NOT NULL DEFAULT 'pending',
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    attempts INTEGER NOT NULL DEFAULT 0,
    error_code VARCHAR(100),
    error_message TEXT,
    delivery_mode VARCHAR(10) DEFAULT 'push',
    local_delivery BOOLEAN DEFAULT FALSE,
    inbox_delivered BOOLEAN DEFAULT FALSE,
    acknowledged BOOLEAN DEFAULT FALSE,
    acknowledged_at TIMESTAMPTZ
);

-- Create indexes

-- Messages table indexes
CREATE INDEX IF NOT EXISTS idx_messages_message_id ON messages(message_id);
CREATE INDEX IF NOT EXISTS idx_messages_timestamp_desc ON messages(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_messages_idempotency_key ON messages(idempotency_key);
CREATE INDEX IF NOT EXISTS idx_messages_sender ON messages(sender);
CREATE INDEX IF NOT EXISTS idx_messages_timestamp ON messages(timestamp);
CREATE INDEX IF NOT EXISTS idx_messages_in_reply_to ON messages(in_reply_to);

-- Message statuses table indexes
CREATE INDEX IF NOT EXISTS idx_message_statuses_message_id ON message_statuses(message_id);
CREATE INDEX IF NOT EXISTS idx_message_statuses_status ON message_statuses(status);
CREATE INDEX IF NOT EXISTS idx_message_statuses_next_retry ON message_statuses(next_retry);
CREATE INDEX IF NOT EXISTS idx_message_statuses_updated_at ON message_statuses(updated_at DESC);

-- Recipient statuses table indexes
CREATE INDEX IF NOT EXISTS idx_recipient_statuses_message_id ON recipient_statuses(message_id);
CREATE INDEX IF NOT EXISTS idx_recipient_statuses_address ON recipient_statuses(address);
CREATE INDEX IF NOT EXISTS idx_recipient_statuses_status ON recipient_statuses(status);
CREATE INDEX IF NOT EXISTS idx_recipient_statuses_timestamp ON recipient_statuses(timestamp);
CREATE INDEX IF NOT EXISTS idx_recipient_statuses_delivery ON recipient_statuses(local_delivery, inbox_delivered, acknowledged);
