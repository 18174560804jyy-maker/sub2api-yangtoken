SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

CREATE TABLE IF NOT EXISTS channel_downstream_pricing_sync (
    channel_id          BIGINT PRIMARY KEY REFERENCES channels(id) ON DELETE CASCADE,
    markup_percent      NUMERIC(10,4) NOT NULL DEFAULT 0,
    model_overrides     JSONB NOT NULL DEFAULT '[]'::jsonb,
    managed_models      JSONB NOT NULL DEFAULT '[]'::jsonb,
    upstream_snapshot   JSONB,
    snapshot_fetched_at TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE channel_downstream_pricing_sync IS
    'Per-channel pricing synchronized from the fixed HeroHao downstream pricing endpoint';
COMMENT ON COLUMN channel_downstream_pricing_sync.model_overrides IS
    'Enabled state and optional manual per-million-token prices, keyed case-insensitively by the service';
COMMENT ON COLUMN channel_downstream_pricing_sync.managed_models IS
    'Model names owned by the synchronization job; used to preserve unrelated channel pricing';
COMMENT ON COLUMN channel_downstream_pricing_sync.upstream_snapshot IS
    'Last successfully validated pricing.token.models snapshot';
