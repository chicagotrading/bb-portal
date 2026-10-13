-- name: FindExecLogsToIngest :many
SELECT
    bi.id AS bazel_invocation_id,
    bi.instance_name_bazel_invocations AS instance_name_id,
    d.rev2_instance_name,
    d.digest_function,
    d.hash,
    d.size_bytes
FROM bazel_invocations bi
JOIN files f ON f.id = bi.exec_log_id
JOIN digests d ON d.id = f.digest_id
LEFT JOIN exec_log_summaries s ON s.bazel_invocation_id = bi.id
WHERE
    bi.exec_log_id IS NOT NULL
    AND (bi.bep_completed OR bi.ended_at IS NOT NULL)
    AND (
        s.id IS NULL
        OR (
            s.status = 'PENDING'
            AND s.started_at < sqlc.arg(stale_before)::timestamptz
            AND s.attempts < sqlc.arg(max_attempts)::integer
        )
    )
ORDER BY bi.id
LIMIT sqlc.arg(batch_limit)::bigint;

-- name: ClaimExecLogSummary :one
INSERT INTO exec_log_summaries AS s (
    bazel_invocation_id, status, attempts, started_at,
    spawn_count, cache_hits, executed_count, parse_ms, bytes
)
VALUES (sqlc.arg(bazel_invocation_id)::bigint, 'PENDING', 1, sqlc.arg(now)::timestamptz, 0, 0, 0, 0, 0)
ON CONFLICT (bazel_invocation_id) DO UPDATE
SET attempts = s.attempts + 1, started_at = EXCLUDED.started_at
WHERE
    s.status = 'PENDING'
    AND s.started_at < sqlc.arg(stale_before)::timestamptz
    AND s.attempts < sqlc.arg(max_attempts)::integer
RETURNING s.id, s.attempts;

-- name: FailAbandonedExecLogSummaries :execrows
UPDATE exec_log_summaries
SET
    status = 'FAILED',
    error = 'Abandoned after repeated attempts' || coalesce(': ' || nullif(error, ''), ''),
    finished_at = sqlc.arg(now)::timestamptz
WHERE
    status = 'PENDING'
    AND started_at < sqlc.arg(stale_before)::timestamptz
    AND attempts >= sqlc.arg(max_attempts)::integer;

-- name: InsertMissingActionKeys :exec
INSERT INTO action_keys (instance_name_id, target_label, mnemonic, primary_output)
SELECT
    sqlc.arg(instance_name_id)::bigint,
    input.target_label,
    input.mnemonic,
    input.primary_output
FROM (
    SELECT
        unnest(sqlc.arg(target_labels)::text[]) AS target_label,
        unnest(sqlc.arg(mnemonics)::text[]) AS mnemonic,
        unnest(sqlc.arg(primary_outputs)::text[]) AS primary_output
) AS input
ORDER BY input.target_label, input.mnemonic, input.primary_output
ON CONFLICT (instance_name_id, target_label, mnemonic, primary_output) DO NOTHING;

-- name: ListActionKeyLabels :many
SELECT DISTINCT target_label
FROM action_keys
WHERE instance_name_id = sqlc.arg(instance_name_id)::bigint;

-- name: GetActionKeyIDs :many
SELECT k.id, k.target_label, k.mnemonic, k.primary_output
FROM (
    SELECT
        unnest(sqlc.arg(target_labels)::text[]) AS target_label,
        unnest(sqlc.arg(mnemonics)::text[]) AS mnemonic,
        unnest(sqlc.arg(primary_outputs)::text[]) AS primary_output
) AS input
JOIN action_keys k
    ON k.instance_name_id = sqlc.arg(instance_name_id)::bigint
    AND k.target_label = input.target_label
    AND k.mnemonic = input.mnemonic
    AND k.primary_output = input.primary_output;

-- name: GetActionVersionIDs :many
SELECT v.id, v.action_key_id, v.digest_hash, v.digest_size_bytes
FROM (
    SELECT
        unnest(sqlc.arg(action_key_ids)::bigint[]) AS action_key_id,
        unnest(sqlc.arg(digest_hashes)::bytea[]) AS digest_hash,
        unnest(sqlc.arg(digest_size_bytes)::bigint[]) AS digest_size_bytes
) AS input
JOIN action_versions v
    ON v.action_key_id = input.action_key_id
    AND v.digest_hash = input.digest_hash
    AND v.digest_size_bytes = input.digest_size_bytes;

-- name: InsertMissingActionVersions :exec
INSERT INTO action_versions (action_key_id, digest_hash, digest_size_bytes, groups, first_seen_at, last_seen_at)
SELECT
    input.action_key_id,
    input.digest_hash,
    input.digest_size_bytes,
    input.groups::jsonb,
    sqlc.arg(now)::timestamptz,
    sqlc.arg(now)::timestamptz
FROM (
    SELECT
        unnest(sqlc.arg(action_key_ids)::bigint[]) AS action_key_id,
        unnest(sqlc.arg(digest_hashes)::bytea[]) AS digest_hash,
        unnest(sqlc.arg(digest_size_bytes)::bigint[]) AS digest_size_bytes,
        unnest(sqlc.arg(groups)::text[]) AS groups
) AS input
ORDER BY input.action_key_id, input.digest_hash
ON CONFLICT (action_key_id, digest_hash, digest_size_bytes) DO NOTHING;

-- name: TouchActionVersions :execrows
UPDATE action_versions
SET last_seen_at = sqlc.arg(now)::timestamptz
WHERE
    id = ANY(sqlc.arg(ids)::bigint[])
    AND last_seen_at < sqlc.arg(touch_before)::timestamptz;

-- name: DeleteUnusedActionVersions :execrows
DELETE FROM action_versions
WHERE id IN (
    SELECT v.id
    FROM action_versions v
    WHERE
        v.last_seen_at < sqlc.arg(seen_before)::timestamptz
        AND NOT EXISTS (
            SELECT 1 FROM action_executions e
            WHERE e.action_version_id = v.id
        )
    LIMIT sqlc.arg(batch_limit)::bigint
);

-- name: DeleteUnusedActionKeys :execrows
DELETE FROM action_keys
WHERE id IN (
    SELECT k.id
    FROM action_keys k
    WHERE
        NOT EXISTS (
            SELECT 1 FROM action_versions v
            WHERE v.action_key_id = k.id
        )
        AND NOT EXISTS (
            SELECT 1 FROM action_executions e
            WHERE e.action_key_id = k.id
        )
    LIMIT sqlc.arg(batch_limit)::bigint
);
