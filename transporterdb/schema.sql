CREATE TABLE IF NOT EXISTS global_flags (
    name  text PRIMARY KEY,
    value boolean
);

CREATE TYPE transport_type AS ENUM ('airdrop', 'premined', 'regular');

CREATE TABLE IF NOT EXISTS unconfirmed_burns (
    burn_id          text PRIMARY KEY,
    amount           bigint NOT NULL,
    solana_address   text NOT NULL,
    premined_address text,
    time             timestamp,
    tx_type          transport_type
);

CREATE TABLE IF NOT EXISTS solana_transactions (
    id                text PRIMARY KEY,
    broadcast_time    timestamp,
    confirmation_time timestamp,
    confirmed         boolean NOT NULL DEFAULT FALSE
);

CREATE TABLE IF NOT EXISTS premined_limits (
    address     text PRIMARY KEY,
    allowed_max bigint NOT NULL,
    transported bigint,
    blocked     boolean NOT NULL DEFAULT FALSE
);

CREATE TABLE IF NOT EXISTS premined_transports (
    burn_id        text PRIMARY KEY,
    address        text NOT NULL REFERENCES premined_limits(address),
    supply_after   bigint UNIQUE NOT NULL,
    supply_before  bigint UNIQUE REFERENCES premined_transports(supply_after),
    burn_time      timestamp,
    solana_address text NOT NULL,
    solana_id      text REFERENCES solana_transactions(id)
);

CREATE TABLE IF NOT EXISTS airdrop_transports (
    burn_id        text PRIMARY KEY,
    solana_address text NOT NULL,
    supply_after   bigint UNIQUE NOT NULL,
    supply_before  bigint UNIQUE REFERENCES airdrop_transports(supply_after),
    burn_time      timestamp,
    solana_id      text REFERENCES solana_transactions(id)
);

CREATE TABLE IF NOT EXISTS queue_transports (
    burn_id        text PRIMARY KEY,
    solana_address text NOT NULL,
    supply_after   bigint UNIQUE NOT NULL,
    supply_before  bigint UNIQUE REFERENCES queue_transports(supply_after),
    burn_time      timestamp,
    queue_up       timestamp,
    solana_id      text REFERENCES solana_transactions(id)
);
