-- Queries that exist to be REJECTED by the schema.
--
-- The production query set is designed to make violations inexpressible -- that
-- is its job -- so it cannot prove the database rejects them. If InsertBulkItem
-- hardcodes kind = 'Bulk', a test calling it proves the query file contains a
-- literal, not that the composite foreign key exists.
--
-- Every constrained value is therefore a PARAMETER here. sqlc types these from
-- the schema without evaluating constraints, so rejection happens at runtime
-- where the test wants it -- and a column rename breaks this package too.
--
-- NEVER imported outside _test.go. scripts/archlint.sh enforces that.

-- ---------------------------------------------------------------- fixtures --

-- name: NewCategory :execlastid
INSERT INTO categories (name) VALUES (?);

-- name: NewLocation :execlastid
INSERT INTO locations (name) VALUES (?);

-- name: NewItem :execlastid
INSERT INTO items (kind, name, category_id) VALUES (?, ?, ?);

-- name: NewHolding :execlastid
INSERT INTO holdings (item_id, kind, stowed_location_id) VALUES (?, ?, ?);

-- name: NewEvent :execlastid
INSERT INTO events (subject_kind, subject_id, type, occurred_at) VALUES (?, ?, ?, ?);

-- ------------------------------------------------- variant correspondence --

-- name: AttachUniqueItem :exec
INSERT INTO unique_items (item_id, kind) VALUES (?, ?);

-- name: AttachBulkItem :exec
INSERT INTO bulk_items (item_id, kind, content_unit, package_size) VALUES (?, ?, ?, ?);

-- name: AttachUniqueHolding :exec
INSERT INTO unique_holdings (holding_id, kind, custody, custody_since, displaced_to_id)
VALUES (?, ?, ?, ?, ?);

-- name: AttachBulkHolding :exec
INSERT INTO bulk_holdings (holding_id, kind, quantity, unit_basis) VALUES (?, ?, ?, ?);

-- ------------------------------------------------- payload correspondence --

-- name: AttachQuantityPayload :exec
INSERT INTO ev_quantity (event_id, type, delta, reason) VALUES (?, ?, ?, ?);

-- name: AttachCustodyPayload :exec
INSERT INTO ev_custody (event_id, type, to_custody, displaced_to_id) VALUES (?, ?, ?, ?);

-- name: AttachPlacementPayload :exec
INSERT INTO ev_placement (event_id, type, from_location_id, to_location_id) VALUES (?, ?, ?, ?);

-- name: AttachPresencePayload :exec
INSERT INTO ev_presence (event_id, type, present) VALUES (?, ?, ?);

-- ------------------------------------------------------ ledger mutability --

-- name: MutateEvent :exec
UPDATE events SET note = ? WHERE id = ?;

-- name: RemoveEvent :exec
DELETE FROM events WHERE id = ?;

-- name: MutateQuantityPayload :exec
UPDATE ev_quantity SET delta = ? WHERE event_id = ?;

-- name: RemoveQuantityPayload :exec
DELETE FROM ev_quantity WHERE event_id = ?;

-- ------------------------------------------------------ referential holds --

-- name: RemoveCategory :exec
DELETE FROM categories WHERE id = ?;

-- name: RemoveLocation :exec
DELETE FROM locations WHERE id = ?;

-- name: ListEventIDs :many
SELECT id FROM events ORDER BY id;
