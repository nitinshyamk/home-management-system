-- ANNOTATION for Item: labels and knowledge only.
--
-- Absent by design: kind, content_unit, and package_size. Those TYPE a Holding
-- rather than label an Item, so they belong to the ledger (schema section 3.5) and
-- changing one is a recorded event, not an edit.

-- name: UpdateItemName :exec
UPDATE items SET name = ? WHERE id = ?;

-- name: UpdateItemCategory :exec
UPDATE items SET category_id = ? WHERE id = ?;

-- name: UpdateItemNotes :exec
UPDATE items SET notes = ? WHERE id = ?;

-- name: SetItemPlacementConfirmed :exec
UPDATE items SET placement_confirmed_at = ? WHERE id = ?;

-- name: SetItemArchived :exec
UPDATE items SET archived_at = ? WHERE id = ?;

-- name: CountLiveHoldingsOfItem :one
SELECT count(*) FROM holdings WHERE item_id = ? AND retired_at IS NULL;
