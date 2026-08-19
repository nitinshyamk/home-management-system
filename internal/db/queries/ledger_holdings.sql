-- RECORDING for Holding: creation, and the projection writes.
--
-- Holdings are created HERE rather than in internal/origin because their
-- ledger-derived state is VERIFIED. Verification demands a reconstruction
-- independent of the state being verified, so creation must be recorded as an
-- event -- and stowed_location_id is NOT NULL, so the creating INSERT
-- necessarily writes a ledger-derived column (schema section 3.11).

-- expires_on is written here rather than annotated afterwards because H8 keys
-- on it: two Holdings of one Item in one place on one basis are the same
-- Holding unless their expiry differs. A creation that dropped it would let an
-- operation ask for a slot that cannot exist, be told it does not, and create a
-- duplicate -- which is exactly what happened while this column was missing.

-- name: InsertHolding :execlastid
INSERT INTO holdings (item_id, kind, stowed_location_id, expires_on) VALUES (?, ?, ?, ?);

-- name: InsertUniqueHoldingVariant :exec
INSERT INTO unique_holdings (holding_id, kind, label, custody) VALUES (?, 'Unique', ?, 'AtRest');

-- name: InsertBulkHoldingVariant :exec
INSERT INTO bulk_holdings (holding_id, kind, quantity, unit_basis) VALUES (?, 'Bulk', 0, ?);

-- The projection: the ledger-derived attributes, and nothing else. Immutable
-- and directly-mutable attributes are absent by construction (K3).

-- name: GetHoldingProjection :one
SELECT h.id, h.kind, h.stowed_location_id, h.retired_at,
       b.quantity, b.unit_basis,
       u.custody, u.custody_since, u.displaced_to_id
FROM holdings h
LEFT JOIN bulk_holdings b   ON b.holding_id = h.id
LEFT JOIN unique_holdings u ON u.holding_id = h.id
WHERE h.id = ?;

-- name: UpdateHoldingBaseProjection :exec
UPDATE holdings SET stowed_location_id = ?, retired_at = ? WHERE id = ?;

-- name: UpdateBulkHoldingProjection :exec
UPDATE bulk_holdings SET quantity = ? WHERE holding_id = ?;

-- name: UpdateUniqueHoldingProjection :exec
UPDATE unique_holdings SET custody = ?, custody_since = ?, displaced_to_id = ?
WHERE holding_id = ?;

-- H11: every Holding has a creation event. Not declaratively expressible, so it
-- is upheld transactionally and checked by this query.

-- name: HoldingsWithoutCreationEvent :many
SELECT h.id FROM holdings h
WHERE NOT EXISTS (
    SELECT 1 FROM events e
    WHERE e.subject_kind = 'Holding' AND e.subject_id = h.id AND e.type = 'HoldingCreated'
);

-- H8: no two ACTIVE Holdings share (item, stowed location, unit basis, expiry).
--
-- Transactional rather than declarative: a partial unique index cannot express
-- it, because two null expiries must compare EQUAL here and SQL says they do
-- not. So the operations uphold it and this query checks it -- and the check
-- earns its place, since the layer that broke H8 in practice was not the
-- operations at all but an INSERT below them that silently dropped expires_on.
--
-- coalesce is what makes two unknown expiries one slot rather than two.
--
-- Bulk only, and not as a shortcut: unit_basis lives on BulkHolding, so H8's
-- key does not exist for a Unique Holding. Two identical cables on one shelf
-- are two Holdings by design -- it is what Promote produces, N at a time.

-- name: DuplicateHoldingSlots :many
SELECT h.item_id, h.stowed_location_id, b.unit_basis,
       coalesce(h.expires_on, '') AS expires_on, count(*) AS holdings
FROM holdings h
JOIN bulk_holdings b ON b.holding_id = h.id
WHERE h.retired_at IS NULL
GROUP BY h.item_id, h.stowed_location_id, b.unit_basis, coalesce(h.expires_on, '')
HAVING count(*) > 1
ORDER BY h.item_id, h.stowed_location_id;

-- name: ListHoldingIDs :many
SELECT id FROM holdings ORDER BY id;

-- name: GetItemKind :one
SELECT kind FROM items WHERE id = ?;
