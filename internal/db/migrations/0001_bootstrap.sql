-- Bootstrap: infrastructure only.
--
-- This migration deliberately contains no domain concepts. Its purpose is to
-- prove the migration wiring works — embedded FS, goose dialect, up and down —
-- independently of the data model, which arrives in 0002 onward.
--
-- schema_info records which generation of the domain schema this database
-- carries, so a future binary can refuse to open a database it does not
-- understand rather than migrating it blindly.

-- +goose Up
CREATE TABLE schema_info (
    id         INTEGER PRIMARY KEY CHECK (id = 1),
    generation INTEGER NOT NULL,
    created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO schema_info (id, generation) VALUES (1, 1);

-- +goose Down
DROP TABLE schema_info;
