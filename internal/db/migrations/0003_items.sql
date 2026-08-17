-- Item: base plus two variants (schema §1.4, §3.2).
--
-- The discriminator participates in the foreign key. `items` carries
-- UNIQUE (id, kind) not because id alone is not unique, but so each variant
-- table can key against the pair — which makes it structurally impossible for a
-- bulk_items row to attach to a Unique item. That is invariant I2, enforced by
-- referential integrity rather than by convention.
--
-- unique_items has no attributes beyond its key. That is a finding, not an
-- oversight: everything that would have lived there (Condition, warranty) was
-- cut from the domain model. Unique is the degenerate variant, not a peer of
-- Bulk. The table is kept so "exactly one variant row" stays a uniform rule.

-- +goose Up

CREATE TABLE items (
    id                     INTEGER PRIMARY KEY AUTOINCREMENT,
    kind                   TEXT    NOT NULL CHECK (kind IN ('Unique','Bulk')),
    name                   TEXT    NOT NULL,
    category_id            INTEGER NOT NULL REFERENCES categories(id) ON DELETE RESTRICT,
    notes                  TEXT,
    placement_confirmed_at TEXT,
    created_at             TEXT    NOT NULL DEFAULT (datetime('now')),
    archived_at            TEXT,

    UNIQUE (id, kind)
);

CREATE INDEX idx_items_category ON items(category_id);

CREATE TABLE unique_items (
    item_id INTEGER PRIMARY KEY,
    kind    TEXT NOT NULL CHECK (kind = 'Unique'),

    FOREIGN KEY (item_id, kind) REFERENCES items(id, kind) ON DELETE RESTRICT
);

CREATE TABLE bulk_items (
    item_id      INTEGER PRIMARY KEY,
    kind         TEXT    NOT NULL CHECK (kind = 'Bulk'),
    content_unit TEXT    NOT NULL REFERENCES units(code) ON DELETE RESTRICT,
    -- Milli-units of content_unit per package. NULL means the item has no
    -- package concept at all. I4: positive when present.
    package_size INTEGER CHECK (package_size IS NULL OR package_size > 0),

    FOREIGN KEY (item_id, kind) REFERENCES items(id, kind) ON DELETE RESTRICT
);

-- +goose Down
DROP TABLE bulk_items;
DROP TABLE unique_items;
DROP INDEX idx_items_category;
DROP TABLE items;
