-- QUERY for Holding, joining everything a display needs in one pass.
--
-- Both variant tables are joined so the Go layer can hydrate the right one and
-- surface the checked half of H5: "at least one variant row" cannot be expressed
-- declaratively, so a kind with no matching row must fail loudly here rather
-- than as a nil dereference downstream.

-- name: ListHoldingsWithDetail :many
SELECT
    h.id, h.item_id, h.kind, h.stowed_location_id,
    h.expires_on, h.snoozed_until, h.retired_at, h.created_at,
    i.name AS item_name,
    l.name AS location_name,
    b.quantity, b.unit_basis,
    bi.content_unit, bi.package_size,
    u.label, u.custody, u.custody_since, u.displaced_to_id
FROM holdings h
JOIN items i ON i.id = h.item_id
JOIN locations l ON l.id = h.stowed_location_id
LEFT JOIN bulk_holdings b   ON b.holding_id = h.id
LEFT JOIN bulk_items bi     ON bi.item_id = h.item_id
LEFT JOIN unique_holdings u ON u.holding_id = h.id
ORDER BY i.name, h.id;

-- name: GetHoldingWithDetail :one
SELECT
    h.id, h.item_id, h.kind, h.stowed_location_id,
    h.expires_on, h.snoozed_until, h.retired_at, h.created_at,
    i.name AS item_name,
    l.name AS location_name,
    b.quantity, b.unit_basis,
    bi.content_unit, bi.package_size,
    u.label, u.custody, u.custody_since, u.displaced_to_id
FROM holdings h
JOIN items i ON i.id = h.item_id
JOIN locations l ON l.id = h.stowed_location_id
LEFT JOIN bulk_holdings b   ON b.holding_id = h.id
LEFT JOIN bulk_items bi     ON bi.item_id = h.item_id
LEFT JOIN unique_holdings u ON u.holding_id = h.id
WHERE h.id = ?;

-- D7: on-hand for a Bulk item, summed in content units across live holdings.
--
-- A Package-basis holding contributes quantity x package_size, and BOTH are in
-- milli-units -- so the product is milli-squared and has to be divided by the
-- scale. Getting this wrong reports a thousand times too much, which is the
-- standing hazard of scaled-integer arithmetic and the reason the scale is
-- named rather than written as a literal in Go.

-- name: BulkOnHand :one
SELECT COALESCE(SUM(
    CASE WHEN b.unit_basis = 'Package'
         THEN (b.quantity * COALESCE(bi.package_size, 0)) / 1000
         ELSE b.quantity END
), 0) AS on_hand_milli
FROM holdings h
JOIN bulk_holdings b ON b.holding_id = h.id
LEFT JOIN bulk_items bi ON bi.item_id = h.item_id
WHERE h.item_id = ? AND h.retired_at IS NULL;

-- D7 for Unique items is a different formula entirely: a COUNT of live
-- holdings, not a sum. That asymmetry is why on_hand dispatches on kind.

-- name: UniqueOnHand :one
SELECT count(*) FROM holdings WHERE item_id = ? AND retired_at IS NULL;
