-- The ledger (schema section 1.6): base plus 13 payload shapes.
--
-- 24 event types reduce to 13 payload shapes. The grouping is semantic --
-- events share a shape because they do the same kind of thing, never because
-- their columns happened to line up.
--
-- The composite-FK trick appears here a third time. events carries
-- UNIQUE (id, type), and each payload table CHECKs its own type set and keys
-- against the pair. A Consumed event therefore CANNOT carry an ev_custody
-- payload -- that is E5, enforced by referential integrity.
--
-- events.id IS the sequence. AUTOINCREMENT is monotonic and never reuses a
-- value, which satisfies E2 without a second column.

-- +goose Up

CREATE TABLE events (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    subject_kind TEXT    NOT NULL CHECK (subject_kind IN ('Holding','Location','Item')),
    subject_id   INTEGER NOT NULL,
    type         TEXT    NOT NULL,
    occurred_at  TEXT    NOT NULL,
    recorded_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    note         TEXT,

    UNIQUE (id, type),

    -- Each type belongs to exactly one subject family.
    CHECK (
        (subject_kind = 'Holding' AND type IN (
            'HoldingCreated', 'Acquired', 'Moved', 'Rehomed', 'Consumed',
            'Discarded', 'Opened', 'Split', 'Merged', 'Adjusted', 'CheckedOut',
            'Returned', 'MarkedLost', 'Found', 'Counted', 'Verified', 'Gone'
        ))
     OR (subject_kind = 'Location' AND type IN (
            'NodeCreated', 'NodeReparented', 'NodeArchived', 'NodeRestored'
        ))
     OR (subject_kind = 'Item' AND type IN (
            'ItemKindChanged', 'ItemUnitChanged', 'ItemPackageSizeChanged'
        ))
    )
);

CREATE INDEX idx_events_subject ON events(subject_kind, subject_id, id);


-- Signed milli-unit change. E6: a resolved amount, never a formula --
-- Opened records +2000g, not "one package's worth", so a later
-- package_size edit cannot rewrite what it meant.
CREATE TABLE ev_quantity (
    event_id   INTEGER PRIMARY KEY,
    type       TEXT NOT NULL CHECK (type IN ('Consumed', 'Discarded', 'Opened', 'Split', 'Merged', 'Adjusted')),
    delta      INTEGER NOT NULL,
    reason     TEXT,

    FOREIGN KEY (event_id, type) REFERENCES events(id, type) ON DELETE RESTRICT
);

-- price is in minor currency units.
CREATE TABLE ev_acquisition (
    event_id   INTEGER PRIMARY KEY,
    type       TEXT NOT NULL CHECK (type IN ('Acquired')),
    delta      INTEGER NOT NULL,
    source     TEXT,
    price      INTEGER,

    FOREIGN KEY (event_id, type) REFERENCES events(id, type) ON DELETE RESTRICT
);

-- HoldingCreated shares this shape rather than getting its own: the payload
-- is byte-identical to a null-from Moved. Kept a distinct TYPE (one enum
-- value) so existence and containment stay separate categories.
CREATE TABLE ev_placement (
    event_id   INTEGER PRIMARY KEY,
    type       TEXT NOT NULL CHECK (type IN ('HoldingCreated', 'Moved', 'Rehomed')),
    from_location_id INTEGER REFERENCES locations(id) ON DELETE RESTRICT,
    to_location_id   INTEGER NOT NULL REFERENCES locations(id) ON DELETE RESTRICT,

    FOREIGN KEY (event_id, type) REFERENCES events(id, type) ON DELETE RESTRICT,
    -- Creation is a placement from nowhere, and only creation is.
    CHECK ((type = 'HoldingCreated') = (from_location_id IS NULL))
);

CREATE TABLE ev_custody (
    event_id   INTEGER PRIMARY KEY,
    type       TEXT NOT NULL CHECK (type IN ('CheckedOut', 'Returned', 'MarkedLost', 'Found')),
    to_custody      TEXT NOT NULL CHECK (to_custody IN ('AtRest','Out','Lost')),
    displaced_to_id INTEGER REFERENCES locations(id) ON DELETE RESTRICT,

    FOREIGN KEY (event_id, type) REFERENCES events(id, type) ON DELETE RESTRICT,
    CHECK (displaced_to_id IS NULL OR to_custody = 'Out')
);

-- Bulk only. The Unique counterpart is ev_presence -- Counted used to mean
-- both, which was the one event type carrying two different meanings.
CREATE TABLE ev_observation (
    event_id   INTEGER PRIMARY KEY,
    type       TEXT NOT NULL CHECK (type IN ('Counted')),
    observed_quantity INTEGER NOT NULL,

    FOREIGN KEY (event_id, type) REFERENCES events(id, type) ON DELETE RESTRICT
);

-- Unique only.
CREATE TABLE ev_presence (
    event_id   INTEGER PRIMARY KEY,
    type       TEXT NOT NULL CHECK (type IN ('Verified')),
    present INTEGER NOT NULL CHECK (present IN (0,1)),

    FOREIGN KEY (event_id, type) REFERENCES events(id, type) ON DELETE RESTRICT
);

CREATE TABLE ev_terminal (
    event_id   INTEGER PRIMARY KEY,
    type       TEXT NOT NULL CHECK (type IN ('Gone')),
    reason TEXT,

    FOREIGN KEY (event_id, type) REFERENCES events(id, type) ON DELETE RESTRICT
);

-- NULL parent means a root node. No name: names are labels and carry
-- current state only (schema section 3.5).
CREATE TABLE ev_node_created (
    event_id   INTEGER PRIMARY KEY,
    type       TEXT NOT NULL CHECK (type IN ('NodeCreated')),
    parent_id INTEGER REFERENCES locations(id) ON DELETE RESTRICT,

    FOREIGN KEY (event_id, type) REFERENCES events(id, type) ON DELETE RESTRICT
);

-- The event that makes reorganization accountable: re-parenting a container
-- moves its contents with no Holding event of their own.
CREATE TABLE ev_node_reparented (
    event_id   INTEGER PRIMARY KEY,
    type       TEXT NOT NULL CHECK (type IN ('NodeReparented')),
    from_parent_id INTEGER REFERENCES locations(id) ON DELETE RESTRICT,
    to_parent_id   INTEGER REFERENCES locations(id) ON DELETE RESTRICT,

    FOREIGN KEY (event_id, type) REFERENCES events(id, type) ON DELETE RESTRICT
);

CREATE TABLE ev_node_lifecycle (
    event_id   INTEGER PRIMARY KEY,
    type       TEXT NOT NULL CHECK (type IN ('NodeArchived', 'NodeRestored')),
    resolution TEXT CHECK (resolution IS NULL OR resolution IN ('Lift','Move','Block')),

    FOREIGN KEY (event_id, type) REFERENCES events(id, type) ON DELETE RESTRICT
);

CREATE TABLE ev_kind_changed (
    event_id   INTEGER PRIMARY KEY,
    type       TEXT NOT NULL CHECK (type IN ('ItemKindChanged')),
    from_kind TEXT NOT NULL CHECK (from_kind IN ('Unique','Bulk')),
    to_kind   TEXT NOT NULL CHECK (to_kind   IN ('Unique','Bulk')),

    FOREIGN KEY (event_id, type) REFERENCES events(id, type) ON DELETE RESTRICT,
    CHECK (from_kind <> to_kind)
);

-- Both sides nullable: a Unique item has no unit, so promotion records
-- {g -> NULL}. Without it the discarded definition is unrecoverable.
CREATE TABLE ev_unit_changed (
    event_id   INTEGER PRIMARY KEY,
    type       TEXT NOT NULL CHECK (type IN ('ItemUnitChanged')),
    from_unit TEXT REFERENCES units(code) ON DELETE RESTRICT,
    to_unit   TEXT REFERENCES units(code) ON DELETE RESTRICT,

    FOREIGN KEY (event_id, type) REFERENCES events(id, type) ON DELETE RESTRICT
);

-- Both sides nullable, same reason as ev_unit_changed.
CREATE TABLE ev_package_size_changed (
    event_id   INTEGER PRIMARY KEY,
    type       TEXT NOT NULL CHECK (type IN ('ItemPackageSizeChanged')),
    from_size INTEGER CHECK (from_size IS NULL OR from_size > 0),
    to_size   INTEGER CHECK (to_size   IS NULL OR to_size   > 0),

    FOREIGN KEY (event_id, type) REFERENCES events(id, type) ON DELETE RESTRICT
);

-- Ledger immutability (E1). The ledger is authoritative and unrepairable:
-- append-only is exactly what makes a malformed row permanent, since there is
-- no later write to correct it -- only a compensating event that leaves the bad
-- one in place forever. Unlike the leaf-node triggers in the previous system,
-- these encode no business rule and so can never acquire a legitimate
-- exception: there is no valid UPDATE of an event.

-- +goose StatementBegin
CREATE TRIGGER events_no_update BEFORE UPDATE ON events BEGIN
    SELECT RAISE(ABORT, 'events is append-only (E1)');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER events_no_delete BEFORE DELETE ON events BEGIN
    SELECT RAISE(ABORT, 'events is append-only (E1)');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER ev_quantity_no_update BEFORE UPDATE ON ev_quantity BEGIN
    SELECT RAISE(ABORT, 'ev_quantity is append-only (E1)');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER ev_quantity_no_delete BEFORE DELETE ON ev_quantity BEGIN
    SELECT RAISE(ABORT, 'ev_quantity is append-only (E1)');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER ev_acquisition_no_update BEFORE UPDATE ON ev_acquisition BEGIN
    SELECT RAISE(ABORT, 'ev_acquisition is append-only (E1)');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER ev_acquisition_no_delete BEFORE DELETE ON ev_acquisition BEGIN
    SELECT RAISE(ABORT, 'ev_acquisition is append-only (E1)');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER ev_placement_no_update BEFORE UPDATE ON ev_placement BEGIN
    SELECT RAISE(ABORT, 'ev_placement is append-only (E1)');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER ev_placement_no_delete BEFORE DELETE ON ev_placement BEGIN
    SELECT RAISE(ABORT, 'ev_placement is append-only (E1)');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER ev_custody_no_update BEFORE UPDATE ON ev_custody BEGIN
    SELECT RAISE(ABORT, 'ev_custody is append-only (E1)');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER ev_custody_no_delete BEFORE DELETE ON ev_custody BEGIN
    SELECT RAISE(ABORT, 'ev_custody is append-only (E1)');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER ev_observation_no_update BEFORE UPDATE ON ev_observation BEGIN
    SELECT RAISE(ABORT, 'ev_observation is append-only (E1)');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER ev_observation_no_delete BEFORE DELETE ON ev_observation BEGIN
    SELECT RAISE(ABORT, 'ev_observation is append-only (E1)');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER ev_presence_no_update BEFORE UPDATE ON ev_presence BEGIN
    SELECT RAISE(ABORT, 'ev_presence is append-only (E1)');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER ev_presence_no_delete BEFORE DELETE ON ev_presence BEGIN
    SELECT RAISE(ABORT, 'ev_presence is append-only (E1)');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER ev_terminal_no_update BEFORE UPDATE ON ev_terminal BEGIN
    SELECT RAISE(ABORT, 'ev_terminal is append-only (E1)');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER ev_terminal_no_delete BEFORE DELETE ON ev_terminal BEGIN
    SELECT RAISE(ABORT, 'ev_terminal is append-only (E1)');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER ev_node_created_no_update BEFORE UPDATE ON ev_node_created BEGIN
    SELECT RAISE(ABORT, 'ev_node_created is append-only (E1)');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER ev_node_created_no_delete BEFORE DELETE ON ev_node_created BEGIN
    SELECT RAISE(ABORT, 'ev_node_created is append-only (E1)');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER ev_node_reparented_no_update BEFORE UPDATE ON ev_node_reparented BEGIN
    SELECT RAISE(ABORT, 'ev_node_reparented is append-only (E1)');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER ev_node_reparented_no_delete BEFORE DELETE ON ev_node_reparented BEGIN
    SELECT RAISE(ABORT, 'ev_node_reparented is append-only (E1)');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER ev_node_lifecycle_no_update BEFORE UPDATE ON ev_node_lifecycle BEGIN
    SELECT RAISE(ABORT, 'ev_node_lifecycle is append-only (E1)');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER ev_node_lifecycle_no_delete BEFORE DELETE ON ev_node_lifecycle BEGIN
    SELECT RAISE(ABORT, 'ev_node_lifecycle is append-only (E1)');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER ev_kind_changed_no_update BEFORE UPDATE ON ev_kind_changed BEGIN
    SELECT RAISE(ABORT, 'ev_kind_changed is append-only (E1)');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER ev_kind_changed_no_delete BEFORE DELETE ON ev_kind_changed BEGIN
    SELECT RAISE(ABORT, 'ev_kind_changed is append-only (E1)');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER ev_unit_changed_no_update BEFORE UPDATE ON ev_unit_changed BEGIN
    SELECT RAISE(ABORT, 'ev_unit_changed is append-only (E1)');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER ev_unit_changed_no_delete BEFORE DELETE ON ev_unit_changed BEGIN
    SELECT RAISE(ABORT, 'ev_unit_changed is append-only (E1)');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER ev_package_size_changed_no_update BEFORE UPDATE ON ev_package_size_changed BEGIN
    SELECT RAISE(ABORT, 'ev_package_size_changed is append-only (E1)');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER ev_package_size_changed_no_delete BEFORE DELETE ON ev_package_size_changed BEGIN
    SELECT RAISE(ABORT, 'ev_package_size_changed is append-only (E1)');
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS ev_package_size_changed_no_delete;
DROP TRIGGER IF EXISTS ev_package_size_changed_no_update;
DROP TRIGGER IF EXISTS ev_unit_changed_no_delete;
DROP TRIGGER IF EXISTS ev_unit_changed_no_update;
DROP TRIGGER IF EXISTS ev_kind_changed_no_delete;
DROP TRIGGER IF EXISTS ev_kind_changed_no_update;
DROP TRIGGER IF EXISTS ev_node_lifecycle_no_delete;
DROP TRIGGER IF EXISTS ev_node_lifecycle_no_update;
DROP TRIGGER IF EXISTS ev_node_reparented_no_delete;
DROP TRIGGER IF EXISTS ev_node_reparented_no_update;
DROP TRIGGER IF EXISTS ev_node_created_no_delete;
DROP TRIGGER IF EXISTS ev_node_created_no_update;
DROP TRIGGER IF EXISTS ev_terminal_no_delete;
DROP TRIGGER IF EXISTS ev_terminal_no_update;
DROP TRIGGER IF EXISTS ev_presence_no_delete;
DROP TRIGGER IF EXISTS ev_presence_no_update;
DROP TRIGGER IF EXISTS ev_observation_no_delete;
DROP TRIGGER IF EXISTS ev_observation_no_update;
DROP TRIGGER IF EXISTS ev_custody_no_delete;
DROP TRIGGER IF EXISTS ev_custody_no_update;
DROP TRIGGER IF EXISTS ev_placement_no_delete;
DROP TRIGGER IF EXISTS ev_placement_no_update;
DROP TRIGGER IF EXISTS ev_acquisition_no_delete;
DROP TRIGGER IF EXISTS ev_acquisition_no_update;
DROP TRIGGER IF EXISTS ev_quantity_no_delete;
DROP TRIGGER IF EXISTS ev_quantity_no_update;
DROP TRIGGER IF EXISTS events_no_delete;
DROP TRIGGER IF EXISTS events_no_update;
DROP TABLE ev_package_size_changed;
DROP TABLE ev_unit_changed;
DROP TABLE ev_kind_changed;
DROP TABLE ev_node_lifecycle;
DROP TABLE ev_node_reparented;
DROP TABLE ev_node_created;
DROP TABLE ev_terminal;
DROP TABLE ev_presence;
DROP TABLE ev_observation;
DROP TABLE ev_custody;
DROP TABLE ev_placement;
DROP TABLE ev_acquisition;
DROP TABLE ev_quantity;
DROP INDEX idx_events_subject;
DROP TABLE events;
