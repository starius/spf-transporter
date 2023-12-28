-- name: CreateRecord :exec
INSERT INTO transport_records (
    burn_id,
    address,
    amount,
    total_supply,
    burn_time,
    queue_up
) VALUES (
    $1, $2, $3, $4, $5, $6
);

-- name: InsertSolanaInfo :exec
UPDATE transport_records
SET invoice_time = $2,
    solana_tx_time = $3,
    solana_tx_id = $4
WHERE burn_id = $1;

-- name: SetSolanaConfirmed :exec
UPDATE transport_records
SET solana_confirmed = $2
WHERE burn_id = $1;

-- name: SetCompleted :exec
UPDATE transport_records
SET completed = $2
WHERE burn_id = $1;

-- name: SelectRecord :one
SELECT * FROM transport_records
WHERE burn_id = $1;

-- name: SelectUncompletedRecords :many
SELECT
    burn_id,
    address,
    amount,
    total_supply,
    burn_time,
    queue_up
FROM transport_records
WHERE ((completed = FALSE) AND (total_supply > $1) AND (amount + total_supply < $2))
ORDER BY total_supply;

-- name: SelectWhitelisted :many
SELECT address, amount FROM premined_whitelist;

-- name: InsertWhitelisted :exec
INSERT INTO premined_whitelist (
    address, amount
) VALUES (
    $1, $2
);

-- name: InsertEmissionSnapshot :exec
INSERT INTO emission_history (
    allowed_supply, update_time, tvl
) VALUES (
    $1, $2, $3
);

-- name: SelectLatestEmissionSnapshot :one
SELECT allowed_supply, update_time, tvl
FROM emission_history
WHERE id = currval(pg_get_serial_sequence('emission_history', 'id'));

-- name: SelectEmissionHistory :many
SELECT * FROM emission_history ORDER BY allowed_supply;
