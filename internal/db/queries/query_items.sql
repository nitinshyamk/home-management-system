-- QUERY for Item, hydrating the correct variant.
--
-- Both variant tables are joined so the Go layer can check I3's remaining half:
-- "at least one variant row exists" is a CHECKED invariant -- the primary key
-- gives "at most one" declaratively, but nothing declarative can require one.
-- A kind with no matching variant row surfaces here rather than as a nil deref
-- somewhere downstream.

-- name: GetItemWithVariant :one
SELECT
    i.id, i.kind, i.name, i.category_id, i.notes,
    i.placement_confirmed_at, i.created_at, i.archived_at,
    u.item_id AS unique_variant_id,
    b.item_id AS bulk_variant_id,
    b.content_unit, b.package_size
FROM items i
LEFT JOIN unique_items u ON u.item_id = i.id
LEFT JOIN bulk_items   b ON b.item_id = i.id
WHERE i.id = ?;

-- name: ListItemsWithVariant :many
SELECT
    i.id, i.kind, i.name, i.category_id, i.notes,
    i.placement_confirmed_at, i.created_at, i.archived_at,
    u.item_id AS unique_variant_id,
    b.item_id AS bulk_variant_id,
    b.content_unit, b.package_size
FROM items i
LEFT JOIN unique_items u ON u.item_id = i.id
LEFT JOIN bulk_items   b ON b.item_id = i.id
WHERE i.archived_at IS NULL
ORDER BY i.name;

-- name: ListItemsInCategoryWithVariant :many
SELECT
    i.id, i.kind, i.name, i.category_id, i.notes,
    i.placement_confirmed_at, i.created_at, i.archived_at,
    u.item_id AS unique_variant_id,
    b.item_id AS bulk_variant_id,
    b.content_unit, b.package_size
FROM items i
LEFT JOIN unique_items u ON u.item_id = i.id
LEFT JOIN bulk_items   b ON b.item_id = i.id
WHERE i.category_id = ? AND i.archived_at IS NULL
ORDER BY i.name;

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
