-- QUERY for Holding. The join a display needs is the holdings_with_detail view,
-- defined once in 0007 -- these three queries differ only in what they select
-- FROM it, which is the whole of what actually differs between them.

-- name: ListHoldingsWithDetail :many
SELECT * FROM holdings_with_detail
ORDER BY item_name, id;

-- name: GetHoldingWithDetail :one
SELECT * FROM holdings_with_detail
WHERE id = ?;

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

-- Every live Holding of one Item, wherever it is kept. This is what the
-- composing operations plan against: consuming from a bag has to know which
-- holdings exist at the location, in which unit basis, before it can decide
-- whether a package must be opened.

-- name: HoldingsOfItemWithDetail :many
SELECT * FROM holdings_with_detail
WHERE item_id = ? AND retired_at IS NULL
ORDER BY id;
