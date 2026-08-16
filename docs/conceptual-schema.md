# Home Management System — Conceptual Schema (V1)

> Status: draft, 2026-08-15. Derived from [`domain-model.md`](./domain-model.md), which remains
> authoritative on *concepts*. This document is authoritative on *structure* — entities, attributes,
> relations, and invariants. Still technology-free: no SQL, no Go types, no table layouts.
> §7 records three gaps this phase found in the domain model and how they were resolved.

## 0. Reading rules

**Type vocabulary.** `Identity` (opaque, stable, never reused), `Text`, `Decimal`, `Integer`, `Date`, `Timestamp`, `Enum{…}`, `Ref<Entity>`. A trailing `?` marks optional.

**Two kinds of attribute.** *Stored* attributes are written by operations. *Derived* attributes are computed on read and appear in §4 — per Principle 1 of the domain model, they must never be persisted as independent state, because a stored copy can disagree with its source.

**Invariants are absolute.** Every invariant below must hold after every completed operation. Operations in §5 are atomic: all of their effects land, or none do.

---

## 1. Entities

### 1.1 Category

Classification. A recursive tree with no behavior of its own.

| Attribute | Type | Notes |
|---|---|---|
| `id` | Identity | |
| `parent` | `Ref<Category>?` | null ⟹ root |
| `name` | Text | |
| `description` | Text? | |
| `created_at` | Timestamp | |

**Invariants**
- **C1 — Acyclic.** A Category may not be its own ancestor.
- **C2 — Sibling uniqueness.** `(parent, name)` is unique. Roots are compared with `parent = null` treated as a single group, so root names are unique among roots.
- **C3 — Single parent.** Exactly zero or one parent. Enforced by the attribute's cardinality.

### 1.2 Location

Physical place. **Structurally identical to Category, deliberately a distinct entity** — see §3.1 for why they are not unified.

| Attribute | Type | Notes |
|---|---|---|
| `id` | Identity | |
| `parent` | `Ref<Location>?` | null ⟹ root |
| `name` | Text | |
| `description` | Text? | |
| `created_at` | Timestamp | |

**Invariants** — L1, L2, L3 mirror C1, C2, C3 exactly.

### 1.3 Unit

A reference set, not user-managed data.

| Attribute | Type | Notes |
|---|---|---|
| `code` | Identity | `count`, `g`, `kg`, `ml`, `l`, `m`, `cm` |
| `dimension` | Enum{Count, Mass, Volume, Length} | |
| `to_base_factor` | Decimal | `kg` → 1000 relative to `g` |

**Invariants**
- **U1 — Conversion is intra-dimensional.** A conversion between units of different `dimension` is undefined and must be rejected, never coerced.
- **U2 — One base per dimension.** Exactly one Unit per dimension has `to_base_factor = 1`.

### 1.4 Item

A *kind* of thing. Owns the tracking policy.

| Attribute | Type | Notes |
|---|---|---|
| `id` | Identity | |
| `name` | Text | |
| `category` | `Ref<Category>` | required; **any node**, leaf or not |
| `kind` | Enum{Unique, Bulk} | |
| `content_unit` | `Ref<Unit>` | the unit the thing is fundamentally measured in |
| `package_size` | Decimal? | quantity of `content_unit` in one package; e.g. `2000` for a 2 kg bag |
| `placement_confirmed_at` | Timestamp? | see §7.1 — silences the classification nudge |
| `notes` | Text? | |
| `created_at` | Timestamp | |
| `archived_at` | Timestamp? | soft-retire; never hard-deleted (§6.3) |

**Invariants**
- **I1 — Unique implies degenerate measurement.** `kind = Unique` ⟹ `content_unit.dimension = Count` and `package_size` is null.
- **I2 — Package size lives in content units.** When `package_size` is non-null it is expressed in `content_unit` and must be `> 0`.
- **I3 — Exactly one classification.** An Item belongs to exactly one Category. Multi-classification is out of scope.

### 1.5 Holding

**The central entity.** A tracked physical presence of an Item at a Location. It is also the batch — see domain model §2.3.

| Attribute | Type | Notes |
|---|---|---|
| `id` | Identity | |
| `item` | `Ref<Item>` | |
| `location` | `Ref<Location>` | where it lives. For `Unique`, this is *home* |
| `displaced_to` | `Ref<Location>?` | `Unique` only; where it actually is while `Out`. null ⟹ unknown |
| `quantity` | Decimal | `≥ 0` |
| `unit_basis` | Enum{Content, Package} | which unit `quantity` is counted in |
| `expires_on` | Date? | the batch's expiry |
| `label` | Text? | `Unique` only; distinguishes one unit from its siblings |
| `custody` | Enum{AtRest, Out}? | `Unique` only; null for `Bulk` |
| `custody_since` | Timestamp? | required while `Out` |
| `status` | Enum{Active, Gone, Lost} | |
| `snoozed_until` | Timestamp? | see §7.1 — silences expiry and out-of-place nudges |
| `created_at` | Timestamp | **doubles as "opened at"** (§4, D4) |

**Invariants**
- **H1 — Unique is singular.** `item.kind = Unique` ⟹ `quantity = 1` ∧ `unit_basis = Content`.
- **H2 — Bulk has no custody.** `item.kind = Bulk` ⟹ `custody`, `custody_since`, `displaced_to`, and `label` are all null.
- **H3 — Out is timestamped.** `custody = Out` ⟹ `custody_since` is non-null. `custody = AtRest` ⟹ `custody_since` and `displaced_to` are both null.
- **H4 — Package basis requires a package.** `unit_basis = Package` ⟹ `item.package_size` is non-null.
- **H5 — Non-negative.** `quantity ≥ 0`.
- **H6 — Canonical form.** No two Holdings may share `(item, location, unit_basis, expires_on)` while `status = Active`. Two null expiries count as equal for this comparison. *This is the invariant that prevents the silent-duplication failure mode; receiving stock merges into an existing Holding rather than creating a second one.*
- **H7 — Terminal is terminal.** `status ≠ Active` ⟹ the Holding is excluded from every on-hand rollup and may not be the subject of any further quantity- or custody-changing event.
- **H8 — Ledger agreement.** `quantity` is a materialized projection of the ledger. It must equal the value derived by replay (§4, D1). Divergence is a defect, detectable by replay, and never repaired silently — see `Counted` / `Adjusted` in §5.

### 1.6 Event

The immutable ledger. **Append-only: no updates, no deletes.** Corrections are compensating events.

| Attribute | Type | Notes |
|---|---|---|
| `id` | Identity | |
| `sequence` | Integer | total order; monotonic |
| `subject_kind` | Enum{Holding, Item} | see §7.3 |
| `subject` | `Ref<Holding>` \| `Ref<Item>` | |
| `type` | Enum — see below | |
| `occurred_at` | Timestamp | when it happened in the world |
| `recorded_at` | Timestamp | when it was entered |
| `quantity_delta` | Decimal? | signed; present on quantity-affecting events |
| `from_location` | `Ref<Location>?` | |
| `to_location` | `Ref<Location>?` | |
| `observed_quantity` | Decimal? | `Counted` only |
| `reason` | Text? | `Discarded`, `Gone` |
| `source` | Text? | `Acquired` |
| `price` | Decimal? | `Acquired` |
| `note` | Text? | |

**Holding events:** `Acquired`, `Moved`, `Consumed`, `Discarded`, `Opened`, `CheckedOut`, `Returned`, `Rehomed`, `MarkedLost`, `Found`, `Counted`, `Adjusted`, `Split`, `Merged`, `Gone`.

**Item events:** `Reclassified`, `Renamed`, `Promoted`, `Demoted`.

**Invariants**
- **E1 — Immutable.** No Event is ever updated or deleted.
- **E2 — Totally ordered.** `sequence` is unique and monotonically increasing.
- **E3 — Retrospective entry is legal.** `occurred_at ≤ recorded_at` is expected but `occurred_at` may precede any earlier event's `occurred_at`. Replay orders by `sequence`, not `occurred_at`.
- **E4 — Subject exists.** The referenced Holding or Item is never hard-deleted (§6).
- **E5 — Payload matches type.** Each type permits a fixed subset of the optional attributes; others must be null.

### 1.7 Checkpoint

A performance aid with semantic weight: it defines what "derived" costs.

| Attribute | Type | Notes |
|---|---|---|
| `holding` | `Ref<Holding>` | |
| `through_sequence` | Integer | last Event included |
| `quantity` | Decimal | |
| `as_of` | Timestamp | |

**Invariants**
- **K1 — Replayable.** `quantity` equals the sum of `quantity_delta` over all Events for this Holding with `sequence ≤ through_sequence`.
- **K2 — Advisory only.** Deleting every Checkpoint changes performance, never results.

---

## 2. Relations

```
  Category ──┐ parent (0..1, self)          Location ──┐ parent (0..1, self)
     ▲       │                                 ▲       │
     └───────┘                                 └───────┘
     │ 1                                       │ 1        │ 0..1
     │                                         │          │
     │ N  classifies                        N  │ location │ displaced_to
     │                                         │          │
   Item ─────────────────── 1 ───N──────────► Holding ◄───┘
     │ 1                                       │ 1
     │ N  content_unit                         │ N
     ▼                                         ▼
   Unit                                   Checkpoint

   Event ──── subject ────► Holding  (15 types)
         └─── subject ────► Item     (4 types)
```

| From | To | Cardinality | Delete behavior |
|---|---|---|---|
| Category | Category (parent) | `N → 0..1` | resolve to parent (§6.1) |
| Location | Location (parent) | `N → 0..1` | resolve to parent (§6.1) |
| Item | Category | `N → 1` | blocked while Items reference it |
| Item | Unit | `N → 1` | reference data, never deleted |
| Holding | Item | `N → 1` | Item archived, never deleted (§6.3) |
| Holding | Location (`location`) | `N → 1` | blocked or resolved (§6.2) |
| Holding | Location (`displaced_to`) | `N → 0..1` | cleared on delete |
| Event | Holding \| Item | `N → 1` | subject never deleted |
| Checkpoint | Holding | `N → 1` | cascade |

---

## 3. Structural decisions

### 3.1 Category and Location are not unified

They have identical shape (`id`, `parent`, `name`, `description`) and identical invariants (acyclic, sibling-unique, single-parent). Merging them into one `TreeNode` entity with a discriminator would share one traversal implementation.

**Rejected.** That is exactly the conflation that produced `item_types` in the old system — one structure serving two semantics, which then accreted rules belonging to only one of them. The shape is shared; the semantics are not. A Category never holds anything physically; a Location never classifies anything.

**What *is* shared** is the tree *behavior*, and it should be factored as one abstraction with a single implementation, parameterized by entity:

`create` · `rename` · `re-parent` · `delete-with-resolution` · `ancestors(node)` · `descendants(node)` · `path(node)` · `depth(node)` · `rollup(node, metric)`

Invariants C1–C3 and L1–L3 are one rule set applied twice. Depth is derived from the parent chain, never stored — the old system stored it and had to maintain it on every re-parent.

### 3.2 Quantity is expressed in one of exactly two bases

A Holding's `quantity` is counted either in the Item's `content_unit` or in whole packages. This is the structural form of domain model §2.4 — it is what makes "sealed vs. opened" derivable rather than stored.

| Holding | `quantity` | `unit_basis` | Derived reading |
|---|---|---|---|
| A | 3 | `Package` | 3 unopened 2 kg bags |
| B | 800 | `Content` | the opened bag, 800 g left |

Constraining the basis to two values — rather than letting a Holding name any Unit — removes an entire class of validation, because the only legal units are the Item's own `content_unit` and its package.

### 3.3 Identity is permanent

Identity survives renaming, re-parenting, promotion, demotion, and retirement, and is never reused. The ledger holds references indefinitely, so a reused identity would silently rewrite history.

---

## 4. Derived values — never stored

Per Principle 1. Each of these was a stored field in an earlier draft.

| # | Derived value | Definition |
|---|---|---|
| **D1** | `quantity` (authoritative) | latest Checkpoint + Σ `quantity_delta` of subsequent Events. Holding.`quantity` is a projection of this (H8) |
| **D2** | `depleted` | `quantity = 0` |
| **D3** | `missing` | `custody = Out` ∧ `displaced_to` is null |
| **D4** | `opened` / `opened_at` | `item.package_size` non-null ∧ `unit_basis = Content` / the Holding's `created_at` |
| **D5** | `effective_location` | `displaced_to ?? location` |
| **D6** | `content_quantity` | `unit_basis = Package ? quantity × item.package_size : quantity` |
| **D7** | `on_hand(item)` | Σ `content_quantity` over Active Holdings of that Item |
| **D8** | `days_out` | `now − custody_since` |
| **D9** | `depth(node)` | length of the parent chain |
| **D10** | `path(node)` | ordered ancestor chain, root-first |
| **D11** | `rollup(node)` | metric summed over `descendants(node) ∪ {node}` — the query that makes non-leaf placement work |
| **D12** | `out_of_place` | Active `Unique` Holdings where `custody = Out`, ordered by `days_out` |
| **D13** | `expiring_soon(window)` | Active Holdings where `expires_on ≤ now + window` ∧ (`snoozed_until` is null ∨ `snoozed_until < now`) |
| **D14** | `classification_nudge` | Categories with ≥1 child **and** ≥1 Item classified directly at them, where that Item has `placement_confirmed_at` null |

D11 is load-bearing: it is the single derivation that lets items sit at any node without breaking any report.

---

## 5. Operations

Each is atomic. Listed with the entities it touches and the Events it emits.

| Operation | Touches | Emits | Notes |
|---|---|---|---|
| `AddItem` | Item | — | Item creation is not a ledger event; only physical facts are |
| `Receive(item, location, qty, basis, expiry?)` | Holding | `Acquired` | **Merges into an existing Holding when H6 would otherwise be violated**, rather than creating a second |
| `Move(holding, to)` | Holding | `Moved` | May trigger a merge at the destination (H6); then also emits `Merged` |
| `Consume(item, qty)` | Holding × 1–2 | `Opened`?, `Consumed` | **If no `Content`-basis Holding exists and `package_size` is non-null, implicitly opens one first** — one user action, two events (domain model §4.1) |
| `Open(holding)` | Holding × 2 | `Opened`, `Split` | Rarely called directly; normally implied by `Consume` |
| `Discard(holding, qty, reason)` | Holding | `Discarded` | |
| `CheckOut(holding, to?)` | Holding | `CheckedOut` | `Unique` only. Sets `custody = Out`, `custody_since = now` |
| `Return(holding)` | Holding | `Returned` | `custody = AtRest`, clears `displaced_to` and `custody_since` |
| `Rehome(holding, location)` | Holding | `Rehomed` | Changes `location` without moving the object |
| `MarkLost(holding)` / `Found(holding, location?)` | Holding | `MarkedLost` / `Found` | `Found` may re-home |
| `Count(location)` | Holding × N | `Counted`, `Adjusted`? | **Always emits `Counted`; emits `Adjusted` only on discrepancy.** Never overwrites silently (H8) |
| `Retire(holding, reason)` | Holding | `Gone` | `status = Gone`. Record persists |
| `Promote(item)` | Item, Holding × 1→N | `Promoted`, `Split` × N | One Holding of qty N becomes N of qty 1, each inheriting `expires_on`, auto-labelled |
| `Demote(item)` | Item, Holding × N→1 | `Demoted`, `Merged` | Requires same `location` and `expires_on` across all N. **Lossy going forward** — warn |
| `Reclassify(item, category)` | Item | `Reclassified` | |
| `CreateNode` / `Rename` / `Reparent` / `DeleteNode` | Category \| Location | — | Tree operations; not ledger events |
| `ConfirmPlacement(item)` | Item | — | Sets `placement_confirmed_at`; silences D14 |
| `Snooze(holding, until)` | Holding | — | Sets `snoozed_until`; silences D12 and D13 |

**Cross-cutting invariant — O1:** any operation that would violate H6 must merge rather than duplicate, and must emit `Merged` when it does.

**Cross-cutting invariant — O2:** `Promote` and `Demote` change `Item.kind` and restructure all of that Item's Active Holdings in one atomic unit. A partially-promoted Item violates H1 or H2 and must never be observable.

---

## 6. Deletion and referential rules

### 6.1 Category and Location nodes
Deleting a node with children or contents requires **resolution, not cascade**. The user picks:
- **Lift** — reattach children and contents to the deleted node's parent (blocked at root unless empty).
- **Move** — reattach to a chosen node.
- **Block** — refuse while non-empty.

Default is Lift, matching how reorganization actually proceeds. Re-parenting must not create a cycle (C1/L1) or a sibling-name collision (C2/L2).

### 6.2 Locations with Holdings
Same as 6.1. A `displaced_to` reference is cleared rather than blocking, since it records a transient fact.

### 6.3 Items and Holdings are never hard-deleted
The ledger holds permanent references (E4). Items are archived (`archived_at`); Holdings become `Gone` or `Lost`. Both leave all active views and rollups (H7) while remaining resolvable from history.

### 6.4 Events are never deleted
Corrections are compensating events. This is what makes D1 meaningful.

---

## 7. Gaps this phase found in the domain model

The point of a schema pass is to surface what prose let slide. Three things.

### 7.1 "Dismissible" nudges had nowhere to store dismissals

Principle 4 requires every nudge to be dismissible, and §4.6 lists `Snooze` as an action — but the model defined no place to record either. Without one, a dismissed nudge returns on next load, which is precisely the wallpaper failure the principle exists to prevent.

**Resolved with two narrow attributes rather than a general dismissal registry:**
- `Item.placement_confirmed_at` — "yes, this really does belong at `Spices`." Silences D14 for that Item.
- `Holding.snoozed_until` — covers both the out-of-place nudge (D12) and the expiry nudge (D13), which is why one field serves two.

A general `Dismissal(subject, nudge_kind, until)` entity is the reversal path if a third nudge with different semantics appears. Two fields are cheaper until then.

### 7.2 The restock prompt had no threshold

Domain model §4.1 and §3.2 both fire a "prompt to restock," but par levels were cut alongside `Wanted`/`Ordered`, leaving the prompt with nothing to test against.

**Resolved by scoping the prompt to the derived depletion transition only:** it fires when `on_hand(item)` reaches 0, which needs no stored threshold. No par level in V1.

`Item.par_level` + `Item.restock_to` is the obvious V1.1 addition and is purely additive — it changes the trigger condition from `= 0` to `< par_level` and nothing else. Worth noting that "warn me *before* I run out" is the more useful behavior, so this will likely be the first thing added back.

### 7.3 The ledger has two subject types, not one

Domain model §3.5 presents one event table, but `Reclassified`, `Renamed`, `Promoted`, and `Demoted` are facts about an *Item*, not about any single Holding. Forcing them onto a Holding would be arbitrary for `Reclassified` and impossible for `Promoted`, which restructures many Holdings at once.

**Resolved by giving Event a polymorphic subject** (`subject_kind` + `subject`). Fifteen Holding types, four Item types. Whether this becomes one table with a discriminator or two tables is a technical-design question, deliberately left open — but the *conceptual* fact is that the ledger records changes to two different kinds of subject.

---

## 8. Open questions for the technical phase

These are implementation decisions the conceptual model deliberately does not settle:

1. **Null-equality in H6.** Two null `expires_on` values must compare equal for the canonical-form constraint. Most databases do not treat nulls that way in unique indexes; this needs an explicit strategy (sentinel date, generated column, or partial index).
2. **Checkpoint cadence.** K2 says checkpoints are advisory. How often they are written is a tuning decision.
3. **Event storage shape.** One table with a discriminator and many nullable columns, versus separate tables, versus a typed payload blob. §7.3 fixes the semantics, not the layout.
4. **Tree traversal.** Recursive CTEs (as in the old system), materialized paths, or closure tables. D9–D11 define what must be answerable, not how.
5. **Decimal representation.** Quantities are `Decimal` conceptually; float storage will accumulate error across ledger replay (D1). Needs a fixed-point or integer-minor-unit decision.
6. **Ordering of `sequence`.** Whether it is global or per-Holding affects E2 and replay cost.

---

## 9. Verification

The schema is correct when every acceptance walkthrough in domain model §6 can be traced through §5's operations without introducing an entity, attribute, or event type not listed here.

Two spot checks that exercise the most structure:

**Rice, first use** *(walkthrough 02)* — `Consume(rice, 100g)` finds no `Content`-basis Holding, so it: creates one (`Opened`), decrements the `Package`-basis Holding by 1 (`Split`), credits 2000 g, then subtracts 100 (`Consumed`). Three events, one user action, H4 and H6 intact throughout.

**Promote one of six cables** *(walkthrough 08)* — `Promote(usb_c_cable)` flips `Item.kind` to `Unique` and atomically replaces one Holding of `quantity = 6` with six of `quantity = 1`, each labelled and inheriting `expires_on`. H1 holds for all six only if the operation is atomic — this is O2's reason for existing.
