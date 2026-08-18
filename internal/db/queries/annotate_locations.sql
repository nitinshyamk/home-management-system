-- ANNOTATION: revises directly-mutable attributes on rows that already exist.
-- No INSERT -- annotation never brings anything into being.
--
-- A Location's name and description are labels, so they are annotation even
-- though everything else about a Location is recorded. Renaming a shelf does
-- not move anything, and recording it as an event would make the ledger claim
-- otherwise: replay reconstructs containment, and a rename must be invisible
-- to that reconstruction.
--
-- This file did not exist through v01, which is why a Location could not be
-- renamed at all.

-- name: UpdateLocationName :exec
UPDATE locations SET name = ? WHERE id = ?;

-- name: UpdateLocationDescription :exec
UPDATE locations SET description = ? WHERE id = ?;

-- Annotation must not invent rows, so every update checks its subject first.
-- An archived Location is still nameable: correcting the label on something
-- put away is exactly the kind of revision annotation is for.

-- name: LocationIsLive :one
SELECT EXISTS(SELECT 1 FROM locations WHERE id = ?);
