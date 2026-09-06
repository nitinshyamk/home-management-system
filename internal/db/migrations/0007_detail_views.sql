-- Display projections, defined once.
--
-- Three queries needed the same eighteen-column join of a Holding with its
-- Item, its Location and both variant tables, and each carried its own copy of
-- it, differing only in a WHERE and an ORDER BY. sqlc generates a row type per
-- query, so three identical SELECTs became three field-identical Go structs,
-- which internal/query bridged by converting one to another:
--
--     hydrateHolding(holdingRow(sqlc.ListHoldingsWithDetailRow(row)))
--
-- That compiles only while the three happen to agree on field order and type.
-- Reordering a column in one of the three SELECTs would either stop compiling
-- or, worse, keep compiling with two columns swapped.
--
-- A view gives the projection one definition. sqlc models it as a table, so the
-- queries selecting from it share a single generated row type and the
-- conversions have nothing left to do.
--
-- Views hold no data and enforce nothing, so there is no RESTRICT question here
-- and nothing to migrate: Down drops them.

-- +goose Up

-- Both variant tables are joined so the Go layer can hydrate the right one and
-- surface the checked half of H5: "at least one variant row" cannot be
-- expressed declaratively, so a kind with no matching row must fail loudly at
-- hydration rather than as a nil dereference downstream.
CREATE VIEW holdings_with_detail AS
SELECT
    h.id, h.item_id, h.kind, h.stowed_location_id,
    h.expires_on, h.snoozed_until, h.retired_at, h.created_at,
    i.name AS item_name,
    l.name AS location_name,
    b.quantity, b.unit_basis,
    bi.content_unit, bi.package_size,
    u.label, u.custody, u.custody_since, u.displaced_to_id
FROM holdings h
JOIN items i ON i.id = h.item_id
JOIN locations l ON l.id = h.stowed_location_id
LEFT JOIN bulk_holdings b   ON b.holding_id = h.id
LEFT JOIN bulk_items bi     ON bi.item_id = h.item_id
LEFT JOIN unique_holdings u ON u.holding_id = h.id;

-- Both variant tables are joined for the same reason, against I3: the primary
-- key gives "at most one variant row" declaratively and nothing declarative can
-- require at least one, so a kind with no matching row surfaces at hydration.
CREATE VIEW items_with_variant AS
SELECT
    i.id, i.kind, i.name, i.category_id, i.notes,
    i.placement_confirmed_at, i.created_at, i.archived_at,
    u.item_id AS unique_variant_id,
    b.item_id AS bulk_variant_id,
    b.content_unit, b.package_size
FROM items i
LEFT JOIN unique_items u ON u.item_id = i.id
LEFT JOIN bulk_items   b ON b.item_id = i.id;

-- +goose Down

DROP VIEW items_with_variant;
DROP VIEW holdings_with_detail;
