-- QUERY for Location. Mirrors the Category reads: the tree BEHAVIOUR is one
-- abstraction applied twice, even though the entities are deliberately distinct.
--
-- Same sqlc constraint as query_categories.sql: recursive CTEs select only
-- base-table columns, and depth is derived in Go.

-- name: GetLocation :one
SELECT id, parent_id, name, description, created_at, archived_at
FROM locations WHERE id = ?;

-- name: ListRootLocations :many
SELECT id, parent_id, name, description, created_at, archived_at
FROM locations WHERE parent_id IS NULL AND archived_at IS NULL ORDER BY name;

-- name: ListChildLocations :many
SELECT id, parent_id, name, description, created_at, archived_at
FROM locations WHERE parent_id = ? AND archived_at IS NULL ORDER BY name;

-- name: LocationPath :many
WITH RECURSIVE chain(id, parent_id, name, description, created_at, archived_at) AS (
    SELECT locations.id, locations.parent_id, locations.name, locations.description,
           locations.created_at, locations.archived_at
    FROM locations WHERE locations.id = ?
    UNION ALL
    SELECT l.id, l.parent_id, l.name, l.description, l.created_at, l.archived_at
    FROM locations l JOIN chain ON l.id = chain.parent_id
)
SELECT id, parent_id, name, description, created_at, archived_at FROM chain LIMIT 256;

-- name: LocationDescendants :many
WITH RECURSIVE sub(id, parent_id, name, description, created_at, archived_at) AS (
    SELECT locations.id, locations.parent_id, locations.name, locations.description,
           locations.created_at, locations.archived_at
    FROM locations WHERE locations.id = ?
    UNION ALL
    SELECT l.id, l.parent_id, l.name, l.description, l.created_at, l.archived_at
    FROM locations l JOIN sub ON l.parent_id = sub.id
)
SELECT id, parent_id, name, description, created_at, archived_at
FROM sub WHERE archived_at IS NULL LIMIT 4096;

-- name: CountHoldingsInLocationTree :one
WITH RECURSIVE sub(id) AS (
    SELECT locations.id FROM locations WHERE locations.id = ?
    UNION ALL
    SELECT l.id FROM locations l JOIN sub ON l.parent_id = sub.id
)
SELECT count(*) FROM holdings
WHERE holdings.retired_at IS NULL AND holdings.stowed_location_id IN (SELECT id FROM sub);
