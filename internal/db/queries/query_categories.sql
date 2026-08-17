-- QUERY: reads only. No writes of any kind.
--
-- Depth is derived from the parent chain, never stored. The previous system
-- stored it and had to maintain it on every re-parent.
--
-- Every recursive CTE carries a row LIMIT. C1 is a `checked` invariant upheld at
-- write time, so a cycle should be impossible -- but an uncapped recursive query
-- against a corrupt tree hangs rather than failing, and a hang is the worst way
-- to learn about corruption. The Go layer treats a result that reaches the limit
-- as an error, not as a truncated answer.
--
-- The CTEs select only base-table columns, and depth is computed in Go. sqlc's
-- SQLite analyser cannot type a CTE column with no base-table origin: a computed
-- `depth` fails with "column does not exist", and an explicit CAST does not help.
-- Confirmed against a minimal reproduction, so it is a tool limitation rather
-- than a quirk of these queries.

-- name: GetCategory :one
SELECT id, parent_id, name, description, created_at, archived_at
FROM categories WHERE id = ?;

-- name: ListCategories :many
SELECT id, parent_id, name, description, created_at, archived_at
FROM categories
WHERE archived_at IS NULL
ORDER BY COALESCE(parent_id, 0), name;

-- name: ListRootCategories :many
SELECT id, parent_id, name, description, created_at, archived_at
FROM categories
WHERE parent_id IS NULL AND archived_at IS NULL
ORDER BY name;

-- name: ListChildCategories :many
SELECT id, parent_id, name, description, created_at, archived_at
FROM categories
WHERE parent_id = ? AND archived_at IS NULL
ORDER BY name;

-- D13: path, root-first. Renders with PRESENT-DAY names -- history reading
-- "Moved to Spice Shelf" tells you where to look today, which is more useful
-- than the archaeologically faithful "Moved to Shelf 2".

-- name: CategoryPath :many
WITH RECURSIVE chain(id, parent_id, name, description, created_at, archived_at) AS (
    SELECT categories.id, categories.parent_id, categories.name, categories.description,
           categories.created_at, categories.archived_at
    FROM categories WHERE categories.id = ?
    UNION ALL
    SELECT c.id, c.parent_id, c.name, c.description, c.created_at, c.archived_at
    FROM categories c JOIN chain ON c.id = chain.parent_id
)
SELECT id, parent_id, name, description, created_at, archived_at
FROM chain LIMIT 256;

-- D14/D15: the subtree, which is what lets Items sit at any node without
-- breaking any report.

-- name: CategoryDescendants :many
WITH RECURSIVE sub(id, parent_id, name, description, created_at, archived_at) AS (
    SELECT categories.id, categories.parent_id, categories.name, categories.description,
           categories.created_at, categories.archived_at
    FROM categories WHERE categories.id = ?
    UNION ALL
    SELECT c.id, c.parent_id, c.name, c.description, c.created_at, c.archived_at
    FROM categories c JOIN sub ON c.parent_id = sub.id
)
SELECT id, parent_id, name, description, created_at, archived_at
FROM sub WHERE archived_at IS NULL LIMIT 4096;

-- name: CountItemsInCategoryTree :one
WITH RECURSIVE sub(id) AS (
    SELECT categories.id FROM categories WHERE categories.id = ?
    UNION ALL
    SELECT c.id FROM categories c JOIN sub ON c.parent_id = sub.id
)
SELECT count(*) FROM items
WHERE items.archived_at IS NULL AND items.category_id IN (SELECT id FROM sub);
