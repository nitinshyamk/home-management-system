-- QUERY for Item. The variant join is the items_with_variant view, defined once
-- in 0007 -- these queries differ only in what they select FROM it.

-- name: GetItemWithVariant :one
SELECT * FROM items_with_variant
WHERE id = ?;

-- name: ListItemsWithVariant :many
SELECT * FROM items_with_variant
WHERE archived_at IS NULL
ORDER BY name;

-- name: ListItemsInCategoryWithVariant :many
SELECT * FROM items_with_variant
WHERE category_id = ? AND archived_at IS NULL
ORDER BY name;

-- D20's raw material: Items filed directly at a Category that has children, and
-- whose placement has not been confirmed. The nudge fires only where a plausible
-- sibling exists, which is why the child count matters.

-- name: ListUnconfirmedItemsAtBranchCategories :many
SELECT
    i.id, i.name, i.category_id, c.name AS category_name,
    (SELECT count(*) FROM categories ch WHERE ch.parent_id = c.id AND ch.archived_at IS NULL) AS child_count
FROM items i
JOIN categories c ON c.id = i.category_id
WHERE i.archived_at IS NULL
  AND i.placement_confirmed_at IS NULL
  AND (SELECT count(*) FROM categories ch WHERE ch.parent_id = c.id AND ch.archived_at IS NULL) > 0
ORDER BY c.name, i.name;
