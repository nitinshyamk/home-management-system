-- Reference-data reads. Owned by the query path (plan section 1.1): no writes.

-- name: ListUnits :many
SELECT code, dimension, to_base_factor
FROM units
ORDER BY dimension, to_base_factor;

-- name: GetUnit :one
SELECT code, dimension, to_base_factor
FROM units
WHERE code = ?;

-- name: GetSchemaGeneration :one
SELECT generation FROM schema_info WHERE id = 1;

-- Promotion asks whether an item's unit counts individual things. Grams of
-- rice cannot become individually tracked units of anything, so the dimension
-- decides whether the operation is meaningful at all.

-- name: GetUnitDimension :one
SELECT dimension FROM units WHERE code = ?;
