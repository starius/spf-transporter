-- name: BurnIDExists :one
SELECT
    exists (SELECT 1 FROM unconfirmed_burns WHERE unconfirmed_burns.burn_id = $1) AS unconfirmed,
    exists (SELECT 1 FROM premined_transports WHERE premined_transports.burn_id = $1) AS premined,
    exists (SELECT 1 FROM airdrop_transports WHERE airdrop_transports.burn_id = $1) AS airdrop,
    exists (SELECT 1 FROM queue_transports WHERE queue_transports.burn_id = $1) AS queue;

-- name: CompletedQueueSupply :one
SELECT COALESCE(MAX(supply_after), 0) FROM queue_transports
INNER JOIN solana_transactions ON solana_transactions.id = queue_transports.solana_id
WHERE solana_transactions.confirmed = TRUE;

-- name: ConfirmedPreminedSupply :one
SELECT COALESCE(MAX(supply_after), 0) FROM premined_transports
WHERE solana_id IS NOT NULL;

-- name: ConfirmedAirdropSupply :one
SELECT COALESCE(MAX(supply_after), 0) FROM airdrop_transports
WHERE solana_id IS NOT NULL;

-- name: UnconfirmedAmount :one
SELECT COALESCE(SUM(amount), 0) FROM unconfirmed_burns WHERE tx_type = $1;

-- name: PreminedSupply :one
SELECT COALESCE(MAX(supply_after), 0) FROM premined_transports;

-- name: AirdropSupply :one
SELECT COALESCE(MAX(supply_after), 0) FROM airdrop_transports;

-- name: UncompletedQueueSupply :one
SELECT COALESCE(MAX(supply_after), 0) FROM queue_transports;

-- name: QueueSize :one
SELECT SUM(supply_after - supply_before) FROM queue_transports
WHERE solana_id IS NULL;

-- name: InsertToQueue :exec
INSERT INTO queue_transports (
    burn_id,
    solana_address,
    supply_after,
    supply_before,
    burn_time,
    queue_up
) VALUES (
    $1, $2, $3, $4, $5, $6
);

-- name: InsertToAirdrop :exec
INSERT INTO airdrop_transports (
    burn_id,
    solana_address,
    supply_after,
    supply_before,
    burn_time
) VALUES (
    $1, $2, $3, $4, $5
);

-- name: InsertToPremined :exec
INSERT INTO premined_transports (
    burn_id,
    address,
    supply_after,
    supply_before,
    burn_time,
    solana_address
) VALUES (
    $1, $2, $3, $4, $5, $6
);

-- name: InsertSolanaTransaction :exec
INSERT INTO solana_transactions (
    id,
    broadcast_time
) VALUES (
    $1, $2
);

-- name: ConfirmSolanaTransaction :exec
UPDATE solana_transactions
SET confirmation_time = $2, confirmed = TRUE
WHERE id = $1;

-- name: SelectSolanaTransaction :one
SELECT * FROM solana_transactions
WHERE id = $1;

-- name: RemoveSolanaTransactionFromPremined :exec
UPDATE premined_transports
SET solana_id = NULL WHERE burn_id = $1;

-- name: InsertSolanaTransactionToPremined :exec
UPDATE premined_transports
SET solana_id = $2 WHERE burn_id = $1;

-- name: RemoveSolanaTransactionFromAirdrop :exec
UPDATE airdrop_transports
SET solana_id = NULL WHERE burn_id = $1;

-- name: InsertSolanaTransactionToAirdrop :exec
UPDATE airdrop_transports
SET solana_id = $2 WHERE burn_id = $1;

-- name: RemoveSolanaTransactionFromQueue :exec
UPDATE queue_transports
SET solana_id = NULL WHERE burn_id = $1;

-- name: InsertSolanaTransactionToQueue :exec
UPDATE queue_transports
SET solana_id = $2 WHERE burn_id = $1;

-- name: SelectUnconfirmedRecord :one
SELECT * FROM unconfirmed_burns
WHERE burn_id = $1;

-- name: SelectPreminedRecord :one
SELECT * FROM premined_transports
WHERE burn_id = $1;

-- name: SelectAirdropRecord :one
SELECT * FROM airdrop_transports
WHERE burn_id = $1;

-- name: SelectQueueRecord :one
SELECT * FROM queue_transports
WHERE burn_id = $1;

-- name: SelectUncompletedPremined :many
SELECT * FROM premined_transports
WHERE solana_id IS NULL
ORDER BY supply_after;

-- name: SelectUncompletedAirdrop :many
SELECT * FROM airdrop_transports
WHERE solana_id IS NULL
ORDER BY supply_after;

-- name: SelectUncompletedFromQueue :many
SELECT
    burn_id,
    solana_address,
    supply_after,
    supply_before,
    burn_time,
    queue_up
FROM queue_transports
WHERE ((solana_id IS NULL) AND (supply_after < $1))
ORDER BY supply_after;

-- name: SelectSolanaUnconfirmed :many
SELECT burn_id FROM queue_transports
INNER JOIN solana_transactions ON solana_transactions.id = queue_transports.solana_id
WHERE ((queue_transports.solana_id IS NOT NULL) AND solana_transactions.confirmed = FALSE)
UNION
SELECT burn_id FROM premined_transports
INNER JOIN solana_transactions ON solana_transactions.id = premined_transports.solana_id
WHERE ((premined_transports.solana_id IS NOT NULL) AND solana_transactions.confirmed = FALSE)
UNION
SELECT burn_id FROM airdrop_transports
INNER JOIN solana_transactions ON solana_transactions.id = airdrop_transports.solana_id
WHERE ((airdrop_transports.solana_id IS NOT NULL) AND solana_transactions.confirmed = FALSE);

-- name: DecreasePremined :exec
UPDATE premined_limits
SET transported = transported - $2
WHERE address = $1;

-- name: IncreasePreminedTransported :exec
UPDATE premined_limits
SET transported = transported + $2
WHERE address = $1;

-- name: SelectAllPreminedLimits :many
SELECT * FROM premined_limits WHERE blocked = FALSE;

-- name: SelectPremined :many
SELECT * FROM premined_limits WHERE (address = ANY($1::text[])) AND (blocked = FALSE);

-- name: InsertPremined :exec
INSERT INTO premined_limits (
    address, allowed_max, transported
) VALUES (
    $1, $2, $3
);

-- name: InsertUnconfirmed :exec
INSERT INTO unconfirmed_burns (
    burn_id, amount, solana_address, premined_address, time, tx_type
) VALUES (
    $1, $2, $3, $4, $5, $6
);

-- name: SelectUnconfirmed :many
SELECT * FROM unconfirmed_burns WHERE time < $1 ORDER BY time;

-- name: RemoveUnconfirmed :exec
DELETE FROM unconfirmed_burns WHERE burn_id = ANY($1::text[]);

-- name: ReadFlag :one
SELECT value FROM global_flags
WHERE name = $1;

-- name: UpdateFlag :exec
UPDATE global_flags SET value = $2
WHERE name = $1;

-- name: InsertFlag :exec
INSERT INTO global_flags (
    name, value
) VALUES (
    $1, $2
);
