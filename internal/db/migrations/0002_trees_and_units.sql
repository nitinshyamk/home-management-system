-- Trees and reference data.
--
-- Category and Location have near-identical shape and deliberately remain
-- distinct entities (schema §3.1): Location models physical reality that
-- Holdings reference and the ledger tracks, Category is a descriptive overlay
-- with no ledger at all. The ledger boundary falls between them.
--
-- Sibling name uniqueness is deliberately ABSENT (schema §3.10). It was never an
-- integrity rule — identity is by id, and every join, rollup, and merge is
-- id-based. Real homes have two drawers called "junk drawer".

-- +goose Up

CREATE TABLE categories (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    parent_id   INTEGER REFERENCES categories(id) ON DELETE RESTRICT,
    name        TEXT NOT NULL,
    description TEXT,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    archived_at TEXT
);

CREATE INDEX idx_categories_parent ON categories(parent_id);

CREATE TABLE locations (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    parent_id   INTEGER REFERENCES locations(id) ON DELETE RESTRICT,
    name        TEXT NOT NULL,
    description TEXT,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    archived_at TEXT
);

CREATE INDEX idx_locations_parent ON locations(parent_id);

-- Reference data. to_base_factor is an integer because the base unit of each
-- dimension is its smallest: g, ml, cm, count. Choosing the largest instead
-- would make cm -> m a fraction, and quantities are exact integers by design.
CREATE TABLE units (
    code           TEXT PRIMARY KEY,
    dimension      TEXT    NOT NULL CHECK (dimension IN ('Count','Mass','Volume','Length')),
    to_base_factor INTEGER NOT NULL CHECK (to_base_factor > 0)
);

INSERT INTO units (code, dimension, to_base_factor) VALUES
    ('count', 'Count',     1),
    ('g',     'Mass',      1),
    ('kg',    'Mass',   1000),
    ('ml',    'Volume',    1),
    ('l',     'Volume', 1000),
    ('cm',    'Length',    1),
    ('m',     'Length',  100);

-- +goose Down
DROP TABLE units;
DROP INDEX idx_locations_parent;
DROP TABLE locations;
DROP INDEX idx_categories_parent;
DROP TABLE categories;
