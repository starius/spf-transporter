CREATE TABLE IF NOT EXISTS global_flags (
    name  text PRIMARY KEY,
    value boolean
);

CREATE TYPE transport_type AS ENUM ('airdrop', 'premined', 'regular');

CREATE TABLE IF NOT EXISTS unconfirmed_burns (
    burn_id          text PRIMARY KEY,
    constraint       burn_id_len check (char_length(burn_id) = 64),
    amount           bigint NOT NULL,
    solana_address   text NOT NULL,
    constraint       solana_address_len check (char_length(solana_address) between 32 and 44),
    premined_address text REFERENCES premined_limits(address),
    constraint       premined_address_len check (char_length(premined_address) = 76),
    height           bigint,
    time             timestamp,
    tx_type          transport_type
);

CREATE TABLE IF NOT EXISTS solana_transactions (
    id                text PRIMARY KEY,
    constraint        id_not_empty check (id <> ''),
    broadcast_time    timestamp NOT NULL,
    confirmation_time timestamp,
    confirmed         boolean NOT NULL DEFAULT FALSE
);

CREATE TABLE IF NOT EXISTS premined_limits (
    address     text PRIMARY KEY,
    constraint  address_len check (char_length(address) = 76),
    allowed_max bigint NOT NULL,
    constraint  allowed_max_positive check (allowed_max > 0),
    transported bigint NOT NULL DEFAULT 0,
    constraint  transported_lte_allowed_max check (transported <= allowed_max),
    blocked     boolean NOT NULL DEFAULT FALSE
);

CREATE TABLE IF NOT EXISTS premined_transports (
    burn_id        text PRIMARY KEY,
    constraint     burn_id_len check (char_length(burn_id) = 64),
    address        text NOT NULL REFERENCES premined_limits(address),
    supply_after   bigint UNIQUE NOT NULL,
    supply_before  bigint UNIQUE REFERENCES premined_transports(supply_after),
    constraint     supply_grows check (supply_before < supply_after),
    burn_time      timestamp,
    solana_address text NOT NULL,
    constraint     solana_address_len check (char_length(solana_address) between 32 and 44),
    solana_id      text REFERENCES solana_transactions(id)
);

CREATE TABLE IF NOT EXISTS airdrop_transports (
    burn_id        text PRIMARY KEY,
    constraint     burn_id_len check (char_length(burn_id) = 64),
    solana_address text NOT NULL,
    constraint     solana_address_len check (char_length(solana_address) between 32 and 44),
    supply_after   bigint UNIQUE NOT NULL,
    supply_before  bigint UNIQUE REFERENCES airdrop_transports(supply_after),
    constraint     supply_grows check (supply_before < supply_after),
    burn_time      timestamp,
    solana_id      text REFERENCES solana_transactions(id)
);

CREATE TABLE IF NOT EXISTS queue_transports (
    burn_id        text PRIMARY KEY,
    constraint     burn_id_len check (char_length(burn_id) = 64),
    solana_address text NOT NULL,
    constraint     solana_address_len check (char_length(solana_address) between 32 and 44),
    supply_after   bigint UNIQUE NOT NULL,
    supply_before  bigint UNIQUE REFERENCES queue_transports(supply_after),
    constraint     supply_grows check (supply_before < supply_after),
    burn_time      timestamp,
    queue_up       timestamp,
    solana_id      text REFERENCES solana_transactions(id)
);
