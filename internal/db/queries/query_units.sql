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
