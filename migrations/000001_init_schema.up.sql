CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY,
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    role VARCHAR(50) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS shipments (
    id UUID PRIMARY KEY,
    tracking_number VARCHAR(64) UNIQUE NOT NULL,
    customer_id UUID NOT NULL REFERENCES users(id),
    status VARCHAR(50) NOT NULL,
    sender_name VARCHAR(255) NOT NULL,
    sender_phone VARCHAR(50) NOT NULL,
    sender_address TEXT NOT NULL,
    recipient_name VARCHAR(255) NOT NULL,
    recipient_phone VARCHAR(50) NOT NULL,
    recipient_address TEXT NOT NULL,
    origin_city VARCHAR(100) NOT NULL,
    destination_city VARCHAR(100) NOT NULL,
    weight_kg NUMERIC(10, 2) NOT NULL,
    length_cm NUMERIC(10, 2) NOT NULL,
    width_cm NUMERIC(10, 2) NOT NULL,
    height_cm NUMERIC(10, 2) NOT NULL,
    delivery_type VARCHAR(50) NOT NULL,
    price_cents BIGINT NOT NULL,
    currency VARCHAR(10) NOT NULL DEFAULT 'RUB',
    idempotency_key VARCHAR(255) UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_shipments_customer_created ON shipments (customer_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_shipments_status_created ON shipments (status, created_at DESC);

CREATE TABLE IF NOT EXISTS shipment_events (
    id UUID PRIMARY KEY,
    shipment_id UUID NOT NULL REFERENCES shipments(id) ON DELETE CASCADE,
    event_id VARCHAR(128) UNIQUE NOT NULL,
    old_status VARCHAR(50) NOT NULL,
    new_status VARCHAR(50) NOT NULL,
    source VARCHAR(100) NOT NULL,
    description TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_shipment_events_shipment_created ON shipment_events (shipment_id, created_at);

CREATE TABLE IF NOT EXISTS payments (
    id UUID PRIMARY KEY,
    shipment_id UUID NOT NULL REFERENCES shipments(id),
    external_payment_id VARCHAR(128) UNIQUE NOT NULL,
    idempotency_key VARCHAR(255) UNIQUE NOT NULL,
    amount_cents BIGINT NOT NULL,
    currency VARCHAR(10) NOT NULL DEFAULT 'RUB',
    status VARCHAR(50) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS processed_webhooks (
    event_id VARCHAR(128) PRIMARY KEY,
    provider VARCHAR(50) NOT NULL,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS outbox_events (
    id UUID PRIMARY KEY,
    event_type VARCHAR(100) NOT NULL,
    aggregate_id UUID NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'PENDING',
    retry_count INT NOT NULL DEFAULT 0,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_outbox_events_status_created ON outbox_events (status, created_at);

CREATE TABLE IF NOT EXISTS webhook_subscriptions (
    id UUID PRIMARY KEY,
    customer_id UUID NOT NULL REFERENCES users(id),
    target_url TEXT NOT NULL,
    events TEXT[] NOT NULL,
    secret VARCHAR(255) NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id UUID PRIMARY KEY,
    subscription_id UUID NOT NULL REFERENCES webhook_subscriptions(id) ON DELETE CASCADE,
    event_id VARCHAR(128) NOT NULL,
    status_code INT,
    attempt INT NOT NULL,
    request_body TEXT NOT NULL,
    response_body TEXT,
    success BOOLEAN NOT NULL,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_event ON webhook_deliveries (event_id, attempt);
