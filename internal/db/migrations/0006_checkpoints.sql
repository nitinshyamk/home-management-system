-- ReplayCheckpoint (schema section 1.7).
--
-- projection is a serialized value, not a set of columns, and that is a
-- deliberate inversion of how the ledger is treated. Integrity investment
-- tracks authoritativeness (section 3.4): the ledger gets 13 explicit payload tables
-- because a malformed event is permanent; a checkpoint is rebuildable from the
-- ledger, so a malformed one costs a rebuild. The question is never "how
-- complex is the payload" but "what is lost if it is wrong".
--
-- It covers ONLY ledger-derived attributes (K3) -- never immutable or
-- directly-mutable ones, which replay does not reconstruct and must not claim
-- to. Deliberately NOT protected by immutability triggers: K2 says deleting
-- every checkpoint changes performance, never results.

-- +goose Up

CREATE TABLE replay_checkpoints (
    holding_id       INTEGER PRIMARY KEY REFERENCES holdings(id) ON DELETE RESTRICT,
    through_sequence INTEGER NOT NULL REFERENCES events(id) ON DELETE RESTRICT,
    as_of            TEXT    NOT NULL DEFAULT (datetime('now')),
    projection       TEXT    NOT NULL
);

-- +goose Down
DROP TABLE replay_checkpoints;
