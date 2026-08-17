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
