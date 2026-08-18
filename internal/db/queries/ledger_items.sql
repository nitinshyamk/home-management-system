-- RECORDING for Item: the three attributes that TYPE a Holding.
--
-- kind, content_unit, and package_size determine which variant a Holding is,
-- which attributes it has, which events are legal against it, and how its
-- stored number is read. Changing one is a recorded event, not an edit.
-- name and category determine none of those and belong to annotation.

-- name: UpdateItemKind :exec
UPDATE items SET kind = ? WHERE id = ?;

-- name: UpdateBulkItemUnit :exec
UPDATE bulk_items SET content_unit = ? WHERE item_id = ?;

-- name: UpdateBulkItemPackageSize :exec
UPDATE bulk_items SET package_size = ? WHERE item_id = ?;

-- name: GetItemTypingState :one
SELECT i.id, i.kind, b.content_unit, b.package_size
FROM items i LEFT JOIN bulk_items b ON b.item_id = i.id
WHERE i.id = ?;

-- Changing an Item's kind means swapping its variant row: the variant
-- references items(id, kind), so the old row must go before the kind changes
-- and the new one after.

-- name: DeleteUniqueItemVariant :exec
DELETE FROM unique_items WHERE item_id = ?;

-- name: DeleteBulkItemVariant :exec
DELETE FROM bulk_items WHERE item_id = ?;

-- name: AddUniqueItemVariant :exec
INSERT INTO unique_items (item_id, kind) VALUES (?, 'Unique');

-- name: CountLiveHoldingsOfItemForLedger :one
SELECT count(*) FROM holdings WHERE item_id = ? AND retired_at IS NULL;

-- Demotion re-creates the bulk variant, so the ledger needs its own insert.
-- The one in origin_items.sql belongs to origination and stays there: a
-- demotion is a recorded change to an Item that already exists, not a birth.

-- name: AddBulkItemVariant :exec
INSERT INTO bulk_items (item_id, kind, content_unit, package_size) VALUES (?, 'Bulk', ?, ?);

-- Promote and Demote replace an Item's variant row, and holdings references
-- items(id, kind). Any holding row of the old kind -- RETIRED OR NOT -- still
-- carries that kind, so the composite foreign key refuses the parent update
-- while one exists. Counting live holdings is not enough to know whether a
-- kind change can succeed.

-- name: CountAnyHoldingsOfItem :one
SELECT count(*) FROM holdings WHERE item_id = ?;
