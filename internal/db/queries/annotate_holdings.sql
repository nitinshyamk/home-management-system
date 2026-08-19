-- ANNOTATION for Holding: the three fields that label rather than record.
--
-- Everything else about a Holding -- where it is, how much of it there is,
-- whether it is out, whether it still exists -- is recorded, because those are
-- claims about physical reality and replay must reproduce them. These three are
-- not:
--
--   expires_on     what the packet says. A correction is a correction, not a
--                  new fact about the world. It carries no quantity and moves
--                  nothing, so recording it would put a non-event in the
--                  ledger (E7).
--   snoozed_until  a note to the person, about the interface's own nagging.
--                  It has no physical meaning at all.
--   label          which one of several this is. A name, like every other name
--                  in the system.
--
-- This file did not exist through v01, so a Holding could not be labelled, its
-- printed date corrected, or its nudge silenced -- the same gap
-- annotate_locations.sql closed for Locations.

-- name: UpdateHoldingExpiry :exec
UPDATE holdings SET expires_on = ? WHERE id = ?;

-- name: UpdateHoldingSnooze :exec
UPDATE holdings SET snoozed_until = ? WHERE id = ?;

-- name: UpdateUniqueHoldingLabel :exec
UPDATE unique_holdings SET label = ? WHERE holding_id = ?;

-- Annotation must not invent rows, so every update checks its subject first.
-- A retired Holding is still annotatable: correcting the label on something
-- put away is exactly the kind of revision annotation is for.

-- expires_on is part of H8's key, so revising it can walk a Holding into a slot
-- another Holding already occupies. Annotation must not do that: merging two
-- Holdings into one is a RECORDING decision (O1) with events to show for it, so
-- the annotation refuses and leaves the merge to an operation.
--
-- No rows for a Unique Holding, which has no slot and so no such hazard.

-- name: BulkHoldingSlotOf :one
SELECT h.item_id, h.stowed_location_id, b.unit_basis
FROM holdings h
JOIN bulk_holdings b ON b.holding_id = h.id
WHERE h.id = ?;

-- name: CountHoldingsInSlot :one
SELECT count(*) FROM holdings h
JOIN bulk_holdings b ON b.holding_id = h.id
WHERE h.retired_at IS NULL
  AND h.id != ?
  AND h.item_id = ?
  AND h.stowed_location_id = ?
  AND b.unit_basis = ?
  AND coalesce(h.expires_on, '') = ?;

-- name: HoldingIsLive :one
SELECT EXISTS(SELECT 1 FROM holdings WHERE id = ?);

-- name: UniqueHoldingIsLive :one
SELECT EXISTS(SELECT 1 FROM unique_holdings WHERE holding_id = ?);
