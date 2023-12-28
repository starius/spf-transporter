CREATE TABLE IF NOT EXISTS transport_records (
    burn_id          text PRIMARY KEY,
    address          text NOT NULL,
    amount           bigint NOT NULL,
    total_supply     bigint UNIQUE,
    burn_time        timestamp,
    queue_up         timestamp,
    invoice_time     timestamp,
    solana_tx_time   timestamp,
    solana_tx_id     text,
    solana_confirmed boolean NOT NULL DEFAULT FALSE,
    completed        boolean NOT NULL DEFAULT FALSE
);

CREATE TABLE IF NOT EXISTS premined_whitelist (
    id      bigserial PRIMARY KEY,
    address text NOT NULL,
    amount  bigint NOT NULL
);

CREATE TABLE IF NOT EXISTS emission_history (
    id             bigserial PRIMARY KEY,
    allowed_supply bigint,
    update_time    timestamp,
    tvl            bigint
);
