-- Holding: base plus two variants (schema §1.5).
--
-- holdings carries FOREIGN KEY (item_id, kind) -> items(id, kind), which makes
-- H3 (holding.kind = item.kind) structural rather than a maintained rule, and
-- UNIQUE (id, kind) so its own variant tables can key against the pair (H4).
--
-- stowed_location, not location: for Unique it is where the thing belongs, for
-- Bulk where the stuff is. "Where it is kept" is one concept true of both, and
-- it makes displaced_to legible as the deviation from stowed — representable
-- only on the variant that can deviate.
--
-- unit_basis lives here as an IMMUTABLE attribute (schema §3.3). No event
-- changes it: Opened creates a new Holding with a different basis rather than
-- converting one. Replay never reconstructs it; it is read off the row.

-- +goose Up

CREATE TABLE holdings (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    item_id            INTEGER NOT NULL,
    kind               TEXT    NOT NULL CHECK (kind IN ('Unique','Bulk')),
    stowed_location_id INTEGER NOT NULL REFERENCES locations(id) ON DELETE RESTRICT,
    expires_on         TEXT,
    snoozed_until      TEXT,
    retired_at         TEXT,
    created_at         TEXT    NOT NULL DEFAULT (datetime('now')),

    UNIQUE (id, kind),
    FOREIGN KEY (item_id, kind) REFERENCES items(id, kind) ON DELETE RESTRICT
);

CREATE INDEX idx_holdings_item     ON holdings(item_id);
CREATE INDEX idx_holdings_location ON holdings(stowed_location_id);
CREATE INDEX idx_holdings_active   ON holdings(retired_at) WHERE retired_at IS NULL;

CREATE TABLE unique_holdings (
    holding_id      INTEGER PRIMARY KEY,
    kind            TEXT NOT NULL CHECK (kind = 'Unique'),
    label           TEXT,
    custody         TEXT NOT NULL DEFAULT 'AtRest'
                         CHECK (custody IN ('AtRest','Out','Lost')),
    custody_since   TEXT,
    displaced_to_id INTEGER REFERENCES locations(id) ON DELETE RESTRICT,

    FOREIGN KEY (holding_id, kind) REFERENCES holdings(id, kind) ON DELETE RESTRICT,

    -- HU1: custody = AtRest if and only if custody_since is null.
    CHECK ((custody = 'AtRest') = (custody_since IS NULL)),
    -- HU2: a known displacement is only meaningful while Out.
    CHECK (displaced_to_id IS NULL OR custody = 'Out')
);

CREATE TABLE bulk_holdings (
    holding_id INTEGER PRIMARY KEY,
    kind       TEXT    NOT NULL CHECK (kind = 'Bulk'),
    -- Milli-units. H6: zero is legal — depletion is a normal end state.
    quantity   INTEGER NOT NULL CHECK (quantity >= 0),
    unit_basis TEXT    NOT NULL CHECK (unit_basis IN ('Content','Package')),

    FOREIGN KEY (holding_id, kind) REFERENCES holdings(id, kind) ON DELETE RESTRICT
);

-- +goose Down
DROP TABLE bulk_holdings;
DROP TABLE unique_holdings;
DROP INDEX idx_holdings_active;
DROP INDEX idx_holdings_location;
DROP INDEX idx_holdings_item;
DROP TABLE holdings;
