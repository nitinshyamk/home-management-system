-- ORIGINATION: brings entities into existence and writes their immutable birth
-- facts. No UPDATE, no DELETE -- an origination error is permanent, and the only
-- remedy is retiring the entity. archlint enforces the absence.

-- name: InsertCategory :execlastid
INSERT INTO categories (parent_id, name, description) VALUES (?, ?, ?);
