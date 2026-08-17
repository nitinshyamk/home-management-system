-- ORIGINATION for Item. Base and variant row go in one transaction (O4).
--
-- Note that `kind` is a LITERAL in each variant insert rather than a parameter.
-- That is what makes a mismatched variant inexpressible through production code
-- -- and precisely why these queries cannot prove the database rejects one. That
-- proof lives in internal/db/probe.

-- name: InsertItem :execlastid
INSERT INTO items (kind, name, category_id, notes) VALUES (?, ?, ?, ?);

-- name: InsertUniqueItemVariant :exec
INSERT INTO unique_items (item_id, kind) VALUES (?, 'Unique');

-- name: InsertBulkItemVariant :exec
INSERT INTO bulk_items (item_id, kind, content_unit, package_size)
VALUES (?, 'Bulk', ?, ?);
