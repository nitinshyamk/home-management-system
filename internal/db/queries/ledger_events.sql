-- RECORDING: the ledger. The only writer of ledger-derived columns.
--
-- Append is the only write. There is no update and no delete, and the schema
-- enforces that with triggers on all 14 ledger tables -- append-only is exactly
-- what makes a malformed row permanent, since there is no later write to fix it.
--
-- The event and its payload are inserted in the same transaction as the
-- projection update. That is O3, and it is what makes H10 a completeness check
-- rather than a correctness one.

-- name: InsertEvent :execlastid
INSERT INTO events (subject_kind, subject_id, type, occurred_at, recorded_at, note)
VALUES (?, ?, ?, ?, ?, ?);

-- name: EventsForSubject :many
SELECT id, subject_kind, subject_id, type, occurred_at, recorded_at, note
FROM events
WHERE subject_kind = ? AND subject_id = ?
ORDER BY id;

-- sqlc cannot type an aggregate like COALESCE(MAX(id), 0) and falls back to
-- interface{}, so the sequence head is read as an ordinary row instead.

-- name: LatestEventID :one
SELECT id FROM events ORDER BY id DESC LIMIT 1;

-- ---------------------------------------------------------- payload writes --

-- name: InsertQuantityPayload :exec
INSERT INTO ev_quantity (event_id, type, delta, reason) VALUES (?, ?, ?, ?);

-- name: InsertAcquisitionPayload :exec
INSERT INTO ev_acquisition (event_id, type, delta, source, price) VALUES (?, ?, ?, ?, ?);

-- name: InsertPlacementPayload :exec
INSERT INTO ev_placement (event_id, type, from_location_id, to_location_id) VALUES (?, ?, ?, ?);

-- name: InsertCustodyPayload :exec
INSERT INTO ev_custody (event_id, type, to_custody, displaced_to_id) VALUES (?, ?, ?, ?);

-- name: InsertObservationPayload :exec
INSERT INTO ev_observation (event_id, type, observed_quantity) VALUES (?, ?, ?);

-- name: InsertPresencePayload :exec
INSERT INTO ev_presence (event_id, type, present) VALUES (?, ?, ?);

-- name: InsertTerminalPayload :exec
INSERT INTO ev_terminal (event_id, type, reason) VALUES (?, ?, ?);

-- name: InsertNodeCreatedPayload :exec
INSERT INTO ev_node_created (event_id, type, parent_id) VALUES (?, ?, ?);

-- name: InsertNodeReparentedPayload :exec
INSERT INTO ev_node_reparented (event_id, type, from_parent_id, to_parent_id) VALUES (?, ?, ?, ?);

-- name: InsertNodeLifecyclePayload :exec
INSERT INTO ev_node_lifecycle (event_id, type, resolution) VALUES (?, ?, ?);

-- name: InsertKindChangedPayload :exec
INSERT INTO ev_kind_changed (event_id, type, from_kind, to_kind) VALUES (?, ?, ?, ?);

-- name: InsertUnitChangedPayload :exec
INSERT INTO ev_unit_changed (event_id, type, from_unit, to_unit) VALUES (?, ?, ?, ?);

-- name: InsertPackageSizeChangedPayload :exec
INSERT INTO ev_package_size_changed (event_id, type, from_size, to_size) VALUES (?, ?, ?, ?);

-- ----------------------------------------------------------- payload reads --
--
-- Scoped by subject rather than by event id, so reading one subject's history
-- costs a fixed number of queries regardless of how many events it has.

-- name: QuantityPayloadsForSubject :many
SELECT p.event_id, p.delta, p.reason
FROM ev_quantity p JOIN events e ON e.id = p.event_id
WHERE e.subject_kind = 'Holding' AND e.subject_id = ?;

-- name: AcquisitionPayloadsForSubject :many
SELECT p.event_id, p.delta, p.source, p.price
FROM ev_acquisition p JOIN events e ON e.id = p.event_id
WHERE e.subject_kind = 'Holding' AND e.subject_id = ?;

-- name: PlacementPayloadsForSubject :many
SELECT p.event_id, p.from_location_id, p.to_location_id
FROM ev_placement p JOIN events e ON e.id = p.event_id
WHERE e.subject_kind = 'Holding' AND e.subject_id = ?;

-- name: CustodyPayloadsForSubject :many
SELECT p.event_id, p.to_custody, p.displaced_to_id
FROM ev_custody p JOIN events e ON e.id = p.event_id
WHERE e.subject_kind = 'Holding' AND e.subject_id = ?;

-- name: ObservationPayloadsForSubject :many
SELECT p.event_id, p.observed_quantity
FROM ev_observation p JOIN events e ON e.id = p.event_id
WHERE e.subject_kind = 'Holding' AND e.subject_id = ?;

-- name: PresencePayloadsForSubject :many
SELECT p.event_id, p.present
FROM ev_presence p JOIN events e ON e.id = p.event_id
WHERE e.subject_kind = 'Holding' AND e.subject_id = ?;

-- name: TerminalPayloadsForSubject :many
SELECT p.event_id, p.reason
FROM ev_terminal p JOIN events e ON e.id = p.event_id
WHERE e.subject_kind = 'Holding' AND e.subject_id = ?;

-- name: NodeCreatedPayloadsForSubject :many
SELECT p.event_id, p.parent_id
FROM ev_node_created p JOIN events e ON e.id = p.event_id
WHERE e.subject_kind = 'Location' AND e.subject_id = ?;

-- name: NodeReparentedPayloadsForSubject :many
SELECT p.event_id, p.from_parent_id, p.to_parent_id
FROM ev_node_reparented p JOIN events e ON e.id = p.event_id
WHERE e.subject_kind = 'Location' AND e.subject_id = ?;

-- name: NodeLifecyclePayloadsForSubject :many
SELECT p.event_id, p.resolution
FROM ev_node_lifecycle p JOIN events e ON e.id = p.event_id
WHERE e.subject_kind = 'Location' AND e.subject_id = ?;

-- name: KindChangedPayloadsForSubject :many
SELECT p.event_id, p.from_kind, p.to_kind
FROM ev_kind_changed p JOIN events e ON e.id = p.event_id
WHERE e.subject_kind = 'Item' AND e.subject_id = ?;

-- name: UnitChangedPayloadsForSubject :many
SELECT p.event_id, p.from_unit, p.to_unit
FROM ev_unit_changed p JOIN events e ON e.id = p.event_id
WHERE e.subject_kind = 'Item' AND e.subject_id = ?;

-- name: PackageSizeChangedPayloadsForSubject :many
SELECT p.event_id, p.from_size, p.to_size
FROM ev_package_size_changed p JOIN events e ON e.id = p.event_id
WHERE e.subject_kind = 'Item' AND e.subject_id = ?;

-- ------------------------------------------------------------------ replay --

-- name: EventsForSubjectAfter :many
SELECT id, subject_kind, subject_id, type, occurred_at, recorded_at, note
FROM events
WHERE subject_kind = ? AND subject_id = ? AND id > ?
ORDER BY id;

-- ReplayCheckpoint. Freely deletable by design (K2): deleting every checkpoint
-- changes performance, never results. Deliberately NOT protected by the
-- immutability triggers that guard the ledger itself, because it is rebuildable
-- from the ledger and a malformed one costs a rebuild rather than the truth.

-- name: GetCheckpoint :one
SELECT holding_id, through_sequence, as_of, projection
FROM replay_checkpoints WHERE holding_id = ?;

-- name: UpsertCheckpoint :exec
INSERT INTO replay_checkpoints (holding_id, through_sequence, as_of, projection)
VALUES (?, ?, ?, ?)
ON CONFLICT(holding_id) DO UPDATE SET
    through_sequence = excluded.through_sequence,
    as_of            = excluded.as_of,
    projection       = excluded.projection;

-- name: DeleteAllCheckpoints :exec
DELETE FROM replay_checkpoints;

-- name: CountCheckpoints :one
SELECT count(*) FROM replay_checkpoints;
