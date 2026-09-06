-- ANNOTATION: revises directly-mutable attributes on rows that already exist.
-- No INSERT -- annotation never brings anything into being.
--
-- Category's entire mutable surface is annotation, including its parent: it has
-- no ledger at all, being a descriptive overlay rather than a model of physical
-- reality (schema section 3.1).

-- name: UpdateCategoryName :exec
UPDATE categories SET name = ? WHERE id = ?;

-- name: UpdateCategoryDescription :exec
UPDATE categories SET description = ? WHERE id = ?;

-- name: UpdateCategoryParent :exec
UPDATE categories SET parent_id = ? WHERE id = ?;

-- name: SetCategoryArchived :exec
UPDATE categories SET archived_at = ? WHERE id = ?;

-- Archive-with-resolution. Lift is the default because it matches how
-- reorganization actually proceeds.

-- name: LiftCategoryChildren :exec
UPDATE categories SET parent_id = ? WHERE parent_id = ?;

-- name: LiftCategoryItems :exec
UPDATE items SET category_id = ? WHERE category_id = ?;

-- name: CountLiveCategoryChildren :one
SELECT count(*) FROM categories WHERE parent_id = ? AND archived_at IS NULL;

-- name: CountLiveCategoryItems :one
SELECT count(*) FROM items WHERE category_id = ? AND archived_at IS NULL;

-- Cycle guard for C1. Walks ancestors of the prospective new parent and asks
-- whether the node being moved is among them. Depth-capped so a corrupt tree
-- fails loudly instead of looping forever.

-- name: CategoryAncestorIDs :many
WITH RECURSIVE chain(id, parent_id) AS (
    SELECT categories.id, categories.parent_id FROM categories WHERE categories.id = ?
    UNION ALL
    SELECT c.id, c.parent_id FROM categories c JOIN chain ON c.id = chain.parent_id
)
SELECT id FROM chain LIMIT 256;

-- name: GetCategoryParent :one
SELECT parent_id FROM categories WHERE id = ?;

-- name: CategoryExists :one
SELECT EXISTS(SELECT 1 FROM categories WHERE id = ?);

-- name: CategoryIsArchived :one
--
-- Archiving something already archived is refused rather than repeated: the
-- second write would overwrite the timestamp recording when it actually
-- happened, and report success for having done nothing. Locations have refused
-- this since they were built; categories did not, which is the drift that comes
-- of one rule written twice.
SELECT archived_at IS NOT NULL FROM categories WHERE id = ?;
