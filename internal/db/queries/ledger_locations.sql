-- RECORDING for Location.
--
-- Location is a model of physical reality that Holdings reference, so its
-- structure is ledger-tracked -- unlike Category, which is a descriptive
-- overlay with no ledger at all. The ledger boundary falls between them.
--
-- The Location ledger exists for COMPLETENESS OF EXPLANATION: re-parenting a
-- container moves its contents with no Holding event of their own, so without
-- NodeReparented something moved and nothing recorded why. Renaming is absent
-- for the same reason it is absent from the boundary: a rename cannot make a
-- Holding appear to have moved.

-- name: InsertLocation :execlastid
INSERT INTO locations (parent_id, name, description) VALUES (?, ?, ?);

-- name: GetLocationState :one
SELECT id, parent_id, archived_at FROM locations WHERE id = ?;

-- name: UpdateLocationParent :exec
UPDATE locations SET parent_id = ? WHERE id = ?;

-- name: SetLocationArchived :exec
UPDATE locations SET archived_at = ? WHERE id = ?;

-- name: LiftLocationChildren :exec
UPDATE locations SET parent_id = ? WHERE parent_id = ?;

-- name: HoldingsStowedAt :many
SELECT id FROM holdings WHERE stowed_location_id = ? AND retired_at IS NULL;

-- name: CountLiveLocationChildren :one
SELECT count(*) FROM locations WHERE parent_id = ? AND archived_at IS NULL;

-- L4: every Location has a creation event.

-- name: LocationsWithoutCreationEvent :many
SELECT l.id FROM locations l
WHERE NOT EXISTS (
    SELECT 1 FROM events e
    WHERE e.subject_kind = 'Location' AND e.subject_id = l.id AND e.type = 'NodeCreated'
);

-- name: LocationAncestorIDs :many
WITH RECURSIVE chain(id, parent_id) AS (
    SELECT locations.id, locations.parent_id FROM locations WHERE locations.id = ?
    UNION ALL
    SELECT l.id, l.parent_id FROM locations l JOIN chain ON l.id = chain.parent_id
)
SELECT id FROM chain LIMIT 256;

-- name: LocationExists :one
SELECT EXISTS(SELECT 1 FROM locations WHERE id = ?);

-- Live children only. Archiving disposes of what is still there; an already
-- archived child was disposed of once and does not move again. The equivalent
-- query in query_locations.sql lists all children including archived ones,
-- which is right for browsing and wrong for this.

-- name: LiveChildLocationIDs :many
SELECT id FROM locations
WHERE parent_id = ? AND archived_at IS NULL
ORDER BY id;
