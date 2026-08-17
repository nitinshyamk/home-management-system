# Home Management System — Conceptual Schema (V1)

> Status: draft 2.3, complete, 2026-08-16. Derived from [`domain-model.md`](./domain-model.md), which
> remains authoritative on *concepts*. This document is authoritative on *structure*.
> Still technology-free — SQL appears only as notation for constraints that are genuinely
> declarative; no table layouts, no types, no queries.
>
> Next phase is implementation; §8 is its inbox. Draft 2.3 folds in findings from the v01
> implementation plan — see §0.4.

## 0. Reading rules

**Type vocabulary.** `Identity` (opaque, stable, never reused), `Text`, `Decimal`, `Integer`, `Date`, `Timestamp`, `Enum{…}`, `Ref<Entity>`. A trailing `?` marks optional.

**Variants are modeled as discriminated unions**, not as nullable fields on a wide record. An entity with variants has a base carrying common attributes plus one table per variant keyed by the same identity — see §3.2 for the mechanics and what they buy.

**Invariants are absolute**, and each is tagged with how it is upheld:

- **`structural`** — the representation makes violation impossible. Nothing to enforce.
- **`declarative`** — a constraint the storage layer can express directly.
- **`transactional`** — upheld by an operation's atomicity.
- **`checked`** — requires an explicit test or job; the representation permits violation.

The goal is to move rules up that list. A `checked` invariant is a bug you can still write.

### 0.1 What changed from draft 1

| Change | Reason |
|---|---|
| Item and Holding split into base + variants | nullable fields were encoding variants (§3.2) |
| `Lost` moved from lifecycle to custody | it has a follow-up path (`Found`), so it isn't terminal (§1.5.1) |
| `location` renamed `stowed_location` | it meant "where it is" for Bulk and "where it belongs" for Unique; one better name dissolves the ambiguity (§1.5) |
| `status` enum → `retired_at?` | the reason already lives in the `Gone` event; don't duplicate the ledger (§1.5) |
| Sibling name uniqueness dropped | never an integrity rule — a usability guard in invariant costume (§3.10) |
| Ledger gained Location and Item subjects | the boundary is what changes a Holding, not "physical vs descriptive" (§3.5) |
| `Counted` split into `Counted` / `Verified` | it was the one event type meaning two different things (§1.6) |
| `Checkpoint` → `ReplayCheckpoint`, narrowed | it claimed to snapshot a Holding while capturing one field (§1.7) |
| **Retracted:** draft 1 §7.3 "the ledger has two subject types" | correct given draft 1's event list; §3.5 makes that event list wrong instead. Superseded, not resolved. |

### 0.2 Changed in draft 2.1

| Change | Reason |
|---|---|
| `NodeRenamed` removed | renaming a place changes no Holding's position. It failed the §3.5 boundary; it survived only to support narrative, which §3.7 now handles better |
| Path snapshots on events removed | they were load-bearing only while tree operations sat *outside* the ledger. Once `NodeReparented`/`NodeArchived` are events, historical paths are derivable — and a cache stored inside the authoritative log is a contradiction (§3.4) |
| Boundary restated as *existence, containment, content, typing — never labels* | the old "physical vs descriptive" phrasing didn't explain why `Location.name` was in and `Item.name` was out. It shouldn't have been |
| Event decomposition resolved: base + 13 payload tables | explicit structure forces remapping at compile time; the alternative trades that for hidden coherency between stored rows and domain logic |
| `ReplayCheckpoint.projection` explicitly serializable | it is rebuildable from the ledger, so it earns a lower integrity investment (§3.4) |
| §3.4 promoted to *the ledger is authoritative*, with consequences enumerated | the principle was implicit across three sections and is what makes their apparently-opposite choices consistent |

### 0.4 Changed in draft 2.3

Found by the v01 implementation plan, which forced the question *what establishes each ledger-derived attribute's initial value?* — a question every earlier draft skipped by reasoning only about transitions.

| Change | Reason |
|---|---|
| **`HoldingCreated` added** — 24 types, still 13 shapes | **correctness bug.** Replay could not establish `stowed_location`; every never-moved Holding would have been flagged by `H10` (§3.11) |
| **`unit_basis` reclassified immutable** | no event changes it — `Opened` creates a new Holding rather than converting one (§3.3) |
| **`Promote`/`Demote` emit up to three events** | **history loss.** Promotion destroyed `content_unit` and `package_size` with no record, breaking backward closure (§3.11, §5.1) |
| **Creation ownership stated** — `H11`, `L4`, new §3.11 | replayed entities are created by the ledger; audited entities directly. Also resolves an identity ordering problem |
| **`H8` demoted to `transactional`** | the variant split moved `unit_basis` off the base table, so no single-table constraint spans its key (§3.8) |
| **Identity clarified as an input, not an output** — new §3.12 | "replay" named two operations. Only projection replay is needed; the ledger is explicitly **not a backup** |
| **Creation-event rule restated as *verified*, not *replayed*** | backward closure can supply an origin but cannot verify — it starts from the value under test (§3.11) |
| **`Promote`/`Demote` replace Holdings rather than mutating them** | mutating would change `kind` and destroy `unit_basis`, both immutable (§3.12) |

**§3.5's boundary is unchanged and survives all of it.** `HoldingCreated` is *existence*; the extra `Promote` events are *typing*. Both were always inside the boundary — the event *enumeration* was wrong, not the principle.

### 0.3 Changed in draft 2.2

*Draft 2.4 refinement:* §3.11's justification for `HoldingCreated` was rewritten. The original argument — `stowed_location` has no natural zero — is true but weak, since backward closure would supply an origin. The real reason is that **verification requires a reconstruction independent of the state being verified**, and backward closure begins from the value under test. The rule now reads *verified* rather than *replayed*, which re-derives the Item exemption instead of asserting it.

§4 onward reworked against §1–§3. Beyond mechanical updates, four new gaps surfaced — `on_hand` is not one formula (§7.4), verifying a Unique Holding had no operation (§7.5), reorganization was silently unaccountable (§7.6), and Item archival was unmodelled (§7.7). §5.4 states what the Location ledger is actually for, which turns out not to be what §3.7 implied.

---

## 1. Entities

### 1.1 Category

Classification. A recursive tree, and a **pure descriptive overlay** — no Holding attribute references it, and it has no ledger (§3.5).

| Attribute | Type | Notes |
|---|---|---|
| `id` | Identity | |
| `parent` | `Ref<Category>?` | null ⟹ root |
| `name` | Text | |
| `description` | Text? | |
| `created_at` | Timestamp | |
| `archived_at` | Timestamp? | soft delete |

| # | Invariant | Upheld by |
|---|---|---|
| **C1** | A Category may not be its own ancestor. | `checked` |
| **C2** | Exactly zero or one parent. | `structural` — attribute cardinality |

### 1.2 Location

Physical place. A recursive tree, and — unlike Category — **a model of physical reality that Holdings reference and the ledger tracks** (§3.1, §3.5).

Same attributes as Category. Invariants **L1**, **L2** mirror **C1**, **C2**.

| # | Invariant | Upheld by |
|---|---|---|
| **L3** | An archived Location remains resolvable forever; it is never hard-deleted. | `declarative` — no delete path exists |
| **L4** | Every Location has a `NodeCreated` event. | `transactional` — the ledger owns creation (§3.11) |

`archived_at` non-null removes a node from pickers and from `rollup`, and nothing else. Its parent pointer is retained: it is part of the historical record.

### 1.3 Unit

Reference data, not user-managed.

| Attribute | Type | Notes |
|---|---|---|
| `code` | Identity | `count`, `g`, `kg`, `ml`, `l`, `m`, `cm` |
| `dimension` | Enum{Count, Mass, Volume, Length} | |
| `to_base_factor` | Decimal | `kg` → 1000 relative to `g` |

| # | Invariant | Upheld by |
|---|---|---|
| **U1** | Conversion across dimensions is undefined and must be rejected, never coerced. | `checked` |
| **U2** | Exactly one Unit per dimension has `to_base_factor = 1`. | `declarative` |

### 1.4 Item

A *kind* of thing. Base plus two variants.

#### Item (base)

| Attribute | Type | Notes |
|---|---|---|
| `id` | Identity | |
| `kind` | Enum{Unique, Bulk} | authoritative discriminator |
| `name` | Text | |
| `category` | `Ref<Category>` | required; **any node**, leaf or not |
| `notes` | Text? | |
| `placement_confirmed_at` | Timestamp? | silences the classification nudge |
| `created_at` | Timestamp | |
| `archived_at` | Timestamp? | soft delete |

> Carries `UNIQUE (id, kind)` — not because `id` alone isn't unique, but so variant tables can key against the pair. See §3.2.

#### 1.4.1 UniqueItem

| Attribute | Type | Notes |
|---|---|---|
| `item_id` | `Ref<Item>` | primary key |
| `kind` | Enum | fixed to `Unique`; part of the composite foreign key |

**No other attributes.** That is a finding, not an oversight: everything that would have lived here — `Condition`, warranty — was cut from the domain model. `Unique` is the *degenerate* variant, not a peer of `Bulk`. The table is retained anyway, because it makes "exactly one variant row" a uniform rule and because this is where any durable-specific property will land the moment one is added back.

#### 1.4.2 BulkItem

| Attribute | Type | Notes |
|---|---|---|
| `item_id` | `Ref<Item>` | primary key |
| `kind` | Enum | fixed to `Bulk`; part of the composite foreign key |
| `content_unit` | `Ref<Unit>` | what the thing is fundamentally measured in |
| `package_size` | Decimal? | content units per package; `2000` for a 2 kg bag |

| # | Invariant | Upheld by |
|---|---|---|
| **I1** | A `Unique` Item has no unit and no package size. | `structural` — `UniqueItem` has no such attributes |
| **I2** | A variant row's kind matches its Item's kind. | `structural` — composite FK on `(item_id, kind)` (§3.2) |
| **I3** | Every Item has **at least one** variant row. | `checked` — not declaratively expressible |
| **I4** | `package_size > 0` when present. | `declarative` |
| **I5** | An Item belongs to exactly one Category. | `structural` — attribute cardinality |

### 1.5 Holding

**The central entity.** A tracked physical presence of an Item at a Location, and simultaneously the batch.

#### Holding (base)

| Attribute | Type | Class | Notes |
|---|---|---|---|
| `id` | Identity | immutable | |
| `item` | `Ref<Item>` | immutable | |
| `kind` | Enum{Unique, Bulk} | immutable | mirrors `item.kind` |
| `stowed_location` | `Ref<Location>` | ledger-derived | where it is kept |
| `retired_at` | Timestamp? | ledger-derived | null ⟹ active. Reason lives in the `Gone` event |
| `expires_on` | Date? | directly mutable | |
| `snoozed_until` | Timestamp? | directly mutable | |
| `created_at` | Timestamp | immutable | **doubles as "opened at"** |

The *Class* column is load-bearing and is defined in §3.3. It is what makes `ReplayCheckpoint` well-formed and what bounds the ledger's claims.

> **`stowed_location`, not `location`.** For a `Unique` Holding it is *where the thing belongs*; for `Bulk` it is *where the stuff is*. Those read as different concepts under the old name. "Where it is kept" is one concept and is true of both — and it makes `displaced_to` legible as *the deviation from stowed*, representable only on the variant that can deviate.
>
> Carries `FK (item, kind) → Item (id, kind)`, which makes kind-correspondence structural rather than checked.

#### 1.5.1 UniqueHolding

| Attribute | Type | Class | Notes |
|---|---|---|---|
| `holding_id` | `Ref<Holding>` | | primary key |
| `kind` | Enum | | fixed to `Unique` |
| `label` | Text? | directly mutable | distinguishes one unit from its siblings |
| `custody` | Enum{AtRest, Out, Lost} | ledger-derived | |
| `custody_since` | Timestamp? | ledger-derived | |
| `displaced_to` | `Ref<Location>?` | ledger-derived | where it actually is; null ⟹ whereabouts unknown |

No `quantity`. No `unit_basis`. A `Unique` Holding is one thing.

**Custody is conceptually a sum type:**

```
Custody = AtRest
        | Out  { since, displaced_to? }
        | Lost { since }
```

| # | Invariant | Upheld by |
|---|---|---|
| **HU1** | `custody = AtRest` ⟺ `custody_since` is null. | `declarative` |
| **HU2** | `displaced_to` non-null ⟹ `custody = Out`. | `declarative` |

> **A deliberate stopping point.** These two invariants could be made structural by splitting custody into its own variant tables. We don't: `since` is shared by two of the three variants, so the split would produce two tables differing by a single nullable column — cost without benefit. §3.2 explains the general test for when a split pays.

**`Lost` is a custody state, not a lifecycle terminal.** The domain model justified keeping `Gone` and `Lost` distinct on the grounds that *Lost has a follow-up path and Gone doesn't*. A state with a follow-up path (`Found`) is by definition not terminal. Moving it here leaves `Gone` as the single lifecycle terminal, which is more faithful to that argument than draft 1 was.

`missing` remains derived: `custody = Out` with `displaced_to` null. It is transient ignorance; `Lost` is a conclusion.

#### 1.5.2 BulkHolding

| Attribute | Type | Class | Notes |
|---|---|---|---|
| `holding_id` | `Ref<Holding>` | | primary key |
| `kind` | Enum | | fixed to `Bulk` |
| `quantity` | Decimal | ledger-derived | |
| `unit_basis` | Enum{Content, Package} | **immutable** | which unit `quantity` counts in (§3.8) |

No custody. No label. Taking twenty zip ties to the garage is a `Move`, not a checkout.

| # | Invariant | Upheld by |
|---|---|---|
| **H1** | A `Unique` Holding has no quantity and no basis. | `structural` — `UniqueHolding` lacks them |
| **H2** | A `Bulk` Holding has no custody and no label. | `structural` — `BulkHolding` lacks them |
| **H3** | `holding.kind = holding.item.kind`. | `structural` — composite FK |
| **H4** | A variant row's kind matches its Holding's kind. | `structural` — composite FK |
| **H5** | Every Holding has at least one variant row. | `checked` |
| **H6** | `quantity ≥ 0`. | `declarative` |
| **H7** | `unit_basis = Package` ⟹ the Item's `package_size` is non-null. | `checked` — cross-entity (§3.9) |
| **H8** | No two **active** Holdings share `(item, stowed_location, unit_basis, expires_on)`; two null expiries compare equal. | `transactional` — **not** declarative; see §3.8 |
| **H9** | `retired_at` non-null ⟹ excluded from every rollup; no further ledger events accepted. | `checked` |
| **H10** | Ledger-derived attributes equal what replay produces. | `checked` — this is the nightly job (§3.6) |
| **H11** | Every Holding has a `HoldingCreated` event. | `transactional` — the ledger owns creation (§3.11) |

`unit_basis` is **immutable**, not ledger-derived: no event changes it, because `Opened` creates a *new* Holding with a different basis rather than converting one. It is read off the row and never replayed.

Draft 1's `H1`, `H2`, `H3` were all shape rules and are now structural. `H3`/`H4` above are new and also structural. Only `H5`, `H7`, `H9`, `H10` require checking, and each is checking something genuinely cross-cutting rather than a shape.

### 1.6 Event

The ledger. Append-only: no updates, no deletes. Corrections are compensating events.

**Three subjects**, per the boundary in §3.5: **Holding**, **Location**, and **Item** — the last restricted to the three properties that *type* a Holding rather than label an Item.

| Attribute | Type | Notes |
|---|---|---|
| `id` | Identity | |
| `sequence` | Integer | total order, monotonic |
| `subject_kind` | Enum{Holding, Location, Item} | |
| `subject_id` | Identity | |
| `type` | Enum — 24 types below | |
| `occurred_at` | Timestamp | when it happened in the world |
| `recorded_at` | Timestamp | when it was entered |
| `note` | Text? | |
| *payload* | — | held in one of 13 shape tables, keyed by `id` (see below) |

| # | Invariant | Upheld by |
|---|---|---|
| **E1** | Never updated, never deleted. | `declarative` — no such path exists |
| **E2** | `sequence` is unique and monotonic. | `declarative` |
| **E3** | Replay orders by `sequence`, never by `occurred_at`; retrospective entry is legal. | `structural` |
| **E4** | Every referenced entity is resolvable forever — nothing the ledger references is hard-deleted. | `declarative` |
| **E5** | Payload matches type. | `structural` — each type maps to exactly one shape table |
| **E6** | Events record **resolved values, never formulas** (§3.7). | `checked` |
| **E7** | No payload attribute is derivable from other ledger state. | `checked` — see §3.4 |

#### Types by subject

**Holding — 17.** `HoldingCreated`, `Acquired`, `Moved`, `Rehomed`, `Consumed`, `Discarded`, `Opened`, `Split`, `Merged`, `Adjusted`, `CheckedOut`, `Returned`, `MarkedLost`, `Found`, `Counted`, `Verified`, `Gone`.

> **`HoldingCreated` is a correctness requirement, not narrative.** Replay folds from an *empty* projection, and `stowed_location` is part of that projection. A Holding created on a shelf and never moved has no event establishing where it is, so replay yields a zero value and `H10` flags it — every never-moved Holding in the house. `Acquired` does not cover the case: its payload has no location, and `Unique` Holdings have no `Acquired` at all. See §3.11.

**Location — 4.** `NodeCreated`, `NodeReparented`, `NodeArchived`, `NodeRestored`.

**Item — 3.** `ItemKindChanged`, `ItemUnitChanged`, `ItemPackageSizeChanged`.

> **No `NodeRenamed`.** Renaming a place changes no Holding's position — `stowed_location` still references the same node. It fails the §3.5 boundary, and the narrative it would have served is better handled by rendering history with present-day labels (§3.7).

#### Events partition by kind; they are never interpreted by kind

`CheckedOut` always means `custody := Out`. It could never have been emitted against a `Bulk` Holding, so replay never needs to consult `Item.kind` to know what an event *means*. This keeps arithmetic verification cheap and independent per Holding; `ItemKindChanged` is needed to *explain and validate* history, which is a weaker and separable requirement.

`Counted` was the one type that broke this — an observed *quantity* on Bulk, an observed *presence* on Unique. Splitting it into `Counted{observed_quantity}` (Bulk) and `Verified{present}` (Unique) makes the partition total.

#### Decomposition: base plus 13 payload shapes

24 types reduce to 13 payload shapes. The grouping is semantic — events share a shape because they do the same kind of thing, never because their columns happened to line up.

| # | Shape | Types | Payload |
|---|---|---|---|
| 1 | Quantity | Consumed, Discarded, Opened, Split, Merged, Adjusted | `delta`, `reason?` |
| 2 | Acquisition | Acquired | `delta`, `source?`, `price?` |
| 3 | Placement | **HoldingCreated**, Moved, Rehomed | `from_location`, `to_location` — creation is a placement from nowhere, so `from_location` is null |
| 4 | Custody | CheckedOut, Returned, MarkedLost, Found | `to_custody`, `displaced_to?` |
| 5 | Observation | Counted | `observed_quantity` |
| 6 | Presence | Verified | `present` |
| 7 | Terminal | Gone | `reason?` |
| 8 | NodeCreated | NodeCreated | `parent?` |
| 9 | NodeReparented | NodeReparented | `from_parent?`, `to_parent?` |
| 10 | NodeLifecycle | NodeArchived, NodeRestored | `resolution?` |
| 11 | KindChanged | ItemKindChanged | `from_kind`, `to_kind` |
| 12 | UnitChanged | ItemUnitChanged | `from_unit`, `to_unit` |
| 13 | PackageSizeChanged | ItemPackageSizeChanged | `from_size?`, `to_size?` |

**Why explicit tables rather than one flat row or a serialized payload.** The ledger is the authoritative store (§3.4), so it earns the highest integrity investment in the system. Thirteen tables force an explicit remapping — caught at compile time — whenever a shape changes. The alternatives trade that for a coherency contract between stored rows and domain logic that nothing checks and nothing surfaces when it breaks.

The counter-argument, recorded because it is genuinely strong: events are append-only, so each row is written once from a typed domain union and never mutated, which gives nullable-muddle a far smaller blast radius here than on Holding where update paths can produce illegal states. It loses to the point above because *append-only is exactly what makes errors permanent* — there is no later write to correct a malformed row, only a compensating event that leaves the malformed one in place forever.

**Shapes 11–13 are deliberately not merged.** A single `Definition{property, from_value, to_value}` would be one table instead of three, but `from_kind` is an `Enum`, `from_unit` is a `Ref<Unit>`, and `from_size` is a `Decimal?`. Collapsing them means stringifying, which reintroduces precisely the untyped payload this decomposition exists to avoid.

**Shapes 5–8 carry a single type each.** That is fine and expected — a shape table with one member is still a typed contract, and shapes 5 and 6 exist *because* `Counted` was split (above) rather than left meaning two things.

`NodeCreated` carries only `parent`. It records the node's coming-into-existence, which tree replay needs; the node's name is a label and lives in current state (§3.5).

### 1.7 ReplayCheckpoint

| Attribute | Type | Notes |
|---|---|---|
| `holding` | `Ref<Holding>` | |
| `through_sequence` | Integer | last Event included |
| `as_of` | Timestamp | |
| `projection` | opaque | the ledger-derived attributes as of that sequence |

| # | Invariant | Upheld by |
|---|---|---|
| **K1** | `projection` equals replay of all events for this Holding with `sequence ≤ through_sequence`. | `checked` |
| **K2** | Advisory only. Deleting every checkpoint changes performance, never results. | `structural` |
| **K3** | Covers **only** ledger-derived attributes — never immutable or directly-mutable ones. | `structural` — by definition of `projection` |

Draft 1 called this a checkpoint of a *Holding* while capturing one field, which was incoherent — a Holding has attributes in all three classes and replay reconstructs only one class. `K3` is the fix, and §3.3 is what makes it statable.

**`projection` is opaque by design — a serialized value, not a set of columns.** Two reasons, and the second is the general one:

1. It is never queried by field. It exists only to resume a replay, and the reader always wants all of it.
2. **It is rebuildable from the ledger.** If its format ever changes, you discard every checkpoint and regenerate — there is no migration, because there is nothing here that isn't already implied by the events. Per §3.4, integrity investment tracks authoritativeness: the ledger gets 13 explicit tables; a derived artifact gets a blob.

That is why this entity and Event resolve in opposite directions despite both holding structured data. The distinction is not "how complex is the payload" but "what is lost if it is wrong." A malformed checkpoint costs a rebuild. A malformed event is permanent.

The nightly integrity job is what makes this entity pay: replay from the last checkpoint, diff against stored state, write a fresh checkpoint. Replay cost is bounded to one day of events rather than all history, and a failure localizes to a single Holding.

---

## 2. Relations

```
   Category ──┐ parent (0..1, self)        Location ──┐ parent (0..1, self)
      ▲       │  descriptive overlay          ▲       │  physical, ledger-tracked
      └───────┘  no ledger                    └───────┘
      │ 1                                     │ 1          │ 0..1
      │ N  classifies                      N  │ stowed_    │ displaced_to
      │                                       │  location  │  (Unique only)
      │                                       │            │
    Item ═════════ 1 ════════ N ═══════════► Holding ◄──────┘
      ║  (id, kind)          (item, kind)      ║  composite FK makes
      ║                                        ║  kind-correspondence structural
   ┌──╨──────────┐                        ┌────╨─────────────┐
   │             │                        │                  │
UniqueItem   BulkItem                UniqueHolding     BulkHolding
  (empty)    content_unit             label?            quantity
             package_size?            custody           unit_basis
                  │                        │
                  ▼                        ▼
                Unit                 ReplayCheckpoint

   Event ──subject──► Holding   (17 types)
         ├─subject──► Location  ( 5 types)
         └─subject──► Item      ( 3 types, structural properties only)
```

| From | To | Cardinality | On delete |
|---|---|---|---|
| Category | Category (parent) | N → 0..1 | archive + lift children |
| Location | Location (parent) | N → 0..1 | archive + lift; emits events (§3.5) |
| Item | Category | N → 1 | blocked while referenced |
| BulkItem | Unit | N → 1 | reference data, never deleted |
| UniqueItem \| BulkItem | Item | 1 → 1 | cascade with the Item |
| Holding | Item | N → 1 | Item archived, never deleted |
| Holding | Location (`stowed_location`) | N → 1 | archive + lift |
| UniqueHolding | Location (`displaced_to`) | N → 0..1 | cleared |
| UniqueHolding \| BulkHolding | Holding | 1 → 1 | cascade with the Holding |
| Event | Holding \| Location \| Item | N → 1 | never — E4 |
| ReplayCheckpoint | Holding | N → 1 | cascade |

---

## 3. Structural decisions

### 3.1 Category and Location are not unified

They have near-identical shape and identical tree invariants. Unifying them into one `TreeNode` with a discriminator would share one traversal implementation.

**Rejected, and the reason is stronger than "it went badly last time."** The two have different ontological status:

- **Location models physical reality.** Holdings reference it, its structure governs where things are, and its evolution is a fact about the world.
- **Category is a descriptive overlay we impose.** No Holding attribute references it. Nothing about a Holding changes if the whole category tree is rebuilt.

**The ledger boundary falls precisely between them** (§3.5). A unified `TreeNode` would have had half of itself inside the ledger and half outside — which is the clearest possible demonstration that they are not one thing.

**What is shared is tree *behavior***, and it should be one abstraction implemented once, parameterized by entity: `create` · `rename` · `re-parent` · `archive-with-resolution` · `ancestors` · `descendants` · `path` · `depth` · `rollup`. Depth is derived from the parent chain, never stored — draft 1's predecessor stored it and had to maintain it on every re-parent.

### 3.2 Variants: base plus variant tables, keyed by shared identity

Item and Holding are discriminated unions. Each is a base table carrying common attributes and the discriminator, plus one table per variant keyed by the **same identity**.

**Identity is preserved trivially.** `Promote` deletes one variant row and inserts the other against the same `item_id`. Every ledger reference is untouched. (An earlier draft claimed this was a complication; it isn't. That claim described *concrete*-table inheritance — no base table, independent key sequences per variant — which nobody proposed and which really would destroy identity on promote.)

**The discriminator participates in the foreign key**, which converts correspondence rules from checked to structural:

```
Item          UNIQUE (id, kind)
BulkItem      kind fixed to 'Bulk'
              FK (item_id, kind) → Item (id, kind)
Holding       FK (item, kind) → Item (id, kind)
```

A `BulkItem` row *cannot* attach to a `Unique` Item — not by convention, by referential integrity. Likewise a `Unique` Holding cannot reference a `Bulk` Item. That is `I2`, `H3`, and `H4`, all structural.

What this does **not** give you is "every base row has at least one variant row" (`I3`, `H5`), which is not declaratively expressible. It needs a deferred constraint, a trigger, or a consistency test.

**When a split pays.** Split when the variants differ in *which attributes exist*. Don't split when they differ only in *whether one attribute is populated* — that produces tables differing by a single nullable column, which is cost without benefit. Custody (§1.5.1) is on the far side of that line and is deliberately left as an enum plus two co-varying nullables.

**The general test.** A nullable attribute is fine when it represents genuinely optional *data*: `expires_on` null means "this doesn't expire." It is a smell when it encodes *a variant*: `custody` null meaning "this row is secretly a different type."

### 3.3 Three classes of attribute

The missing piece from draft 1, and what makes both `ReplayCheckpoint` and the ledger's claims well-formed.

| Class | Meaning | Examples |
|---|---|---|
| **Immutable** | Set at creation, never changes. | `id`, `item`, `kind`, `created_at`, **`unit_basis`** |
| **Ledger-derived** | Every change goes through an event; replay reconstructs it. | `quantity`, `custody`, `custody_since`, `displaced_to`, `stowed_location`, `retired_at` |
| **Directly mutable** | Edited in place. Not in the ledger; replay cannot reconstruct it. | `expires_on`, `label`, `snoozed_until`, `name`, `notes`, `category`, `placement_confirmed_at` |

**`unit_basis` sits in the first class, not the second** — a correction found by asking what establishes each ledger-derived attribute's *initial* value (§3.11). Nothing changes it, so nothing needs to replay it.

The third class is not a gap. `snoozed_until` is UI state. `expires_on` is a correction to *what you know* about a bag of rice, not a thing that happened to it. `label` is a name. None belong in a ledger and none are reconstructible from one — which is fine, provided nothing claims otherwise. `K3` is that claim being correctly bounded.

### 3.4 The ledger is authoritative

The single decision that most other decisions in this document descend from. Stated plainly so the descendants can be checked against it:

> **The ledger is the authoritative record. Everything else in the system is either derived from it, or explicitly outside its scope.**

Five consequences, which otherwise read as unrelated or even contradictory calls:

**1. The ledger earns the highest integrity investment in the system.** Events get 13 explicit payload tables (§1.6), not a serialized blob, because a malformed event is *permanent* — append-only means there is no later write to fix it, only a compensating event that leaves the bad row in place forever.

**2. Integrity investment is proportional to authoritativeness.** The corollary, and why `ReplayCheckpoint.projection` is a blob (§1.7) while Event is thirteen tables. The question is never "how complex is this payload" but "what is lost if it is wrong." A malformed checkpoint costs a rebuild. Read models and caches sit at the same low-investment end for the same reason.

**3. Nothing derivable belongs in the ledger.** `E7`. Redundancy inside the authoritative store is the worst place for it — you cannot repair it from anywhere, and it can silently contradict the events beside it. This is what retired the path snapshots: once tree structure became events, historical paths were derivable, and a cache stored inside the log is a contradiction.

**4. Stored ledger-derived attributes are projections, not duplicates.** They may be read directly, because the ledger wins by definition and divergence has a defined repair. Developed in §3.6.

**5. Scope must be drawn explicitly, not by intuition.** Which is the next section.

### 3.5 The ledger boundary

> **The ledger records existence, containment, content, and typing. Never labels, never knowledge, never UI state.**

An earlier phrasing — "physical reality versus descriptive overlay" — failed on its first hard case: it could not explain why `Location.name` would be recorded when `Item.name` was not. It shouldn't have been. This phrasing covers all four entities uniformly:

| Entity | In the ledger | Out |
|---|---|---|
| **Holding** | created / retired *(existence)*, `stowed_location`, `custody` *(containment)*, `quantity` *(content)* | `label`, `expires_on`, `snoozed_until` |
| **Location** | created / archived / restored *(existence)*, reparented *(containment)* | `name` |
| **Item** | `kind`, `content_unit`, `package_size` *(typing)* | `name`, `category`, `notes` |
| **Category** | nothing — it has no existence, containment, or typing relationship to any Holding | everything |

Applied:

| Change | In ledger? | Why |
|---|---|---|
| Consume 100 g | ✓ Holding | content |
| Check out a cable | ✓ Holding | containment |
| Re-parent a tote | ✓ Location | containment — the place itself moved, so its contents did |
| Archive a shelf, lift contents | ✓ **both** | existence **and** N holdings' containment |
| Promote an Item | ✓ Item + Holding | `kind` types every Holding; the restructuring is `Split` |
| Change an Item's `content_unit` | ✓ Item | typing — every Holding's quantity is expressed in it |
| Change an Item's `package_size` | ✓ Item | typing — gates `unit_basis` and defines content quantity |
| **Rename a shelf** | **✗** | label. `stowed_location` still references the same node; nothing about any Holding changed |
| Rename or reclassify an Item | ✗ | label |
| Create or rename a Category | ✗ | no relationship to any Holding; no ledger at all |
| Set `expires_on`, snooze a nudge | ✗ | knowledge and UI state |

**`kind`, `content_unit`, and `package_size` are in because they *type* a Holding rather than label an Item.** They determine which variant it is, which attributes it has, which events are legal against it, and how its stored number is to be read. `name` and `category` determine none of those — delete them and every Holding is unaffected.

**Delete-with-lift is not an exception.** The rule is: *an operation writes one event per fact it changes.* Re-parenting changes only tree structure, so it writes one Location event and no Holding events — `stowed_location` references a node, not a path, so nothing about the Holdings changed. Archiving with lift genuinely changes both, so it writes both. Per-Holding replay stays independent, which is what keeps the nightly job cheap and its failures localizable.

### 3.6 Quantity is a projection, not a duplicate

Draft 1 said both that quantity was authoritative from replay and that the stored value was authoritative. Those describe different systems. The resolution:

**Ledger-derived attributes are stored and are read directly. The ledger is authoritative in principle, and that principle is testable.** Two rules:

- **Completeness.** Every change to a ledger-derived attribute appends its event **in the same transaction**. Neither is permitted without the other.
- **Consistency.** Replaying a Holding's events reproduces its stored ledger-derived attributes. This is `H10` — a test and a repair procedure, **never a read path**.

Replaying on every read would be absurd for a TUI over SQLite. But a ledger that *can't* reproduce state is just an audit log, and then `Counted`/`Adjusted` are theatre and consumption rates are guesswork. Storing the projection while keeping replay exact is what preserves both.

**This does not violate "derive, don't store."** The line that principle is actually drawing:

> A **projection** may be stored: divergence has a defined winner and a defined repair.
> A **duplicate** may not: divergence has no defined winner.

The ledger wins over stored quantity, always, by definition. A stored `depleted` boolean disagreeing with `quantity` has no defined winner — that is the thing to forbid.

**Verification strategy this implies:** a three-way property test. Apply a random operation sequence to the real system and to a naive in-memory model, assert they agree, then replay the ledger from zero and assert that agrees too. Divergence in any pair localizes the defect immediately.

### 3.7 Events record resolved values, never formulas

`E6`. The failure it prevents: a mutable definition retroactively rewriting the meaning of history.

Rice at `package_size = 2000`, with Holdings of `3 packages` and `800 g`. You edit it to `1000` because the bags were actually 1 kg. Every historical `Opened` event that credited *"one package's worth"* now means something different than it did when written — and the ledger silently says something it never said.

The fix: `Opened` records `−1 package` and `+2000 g` as **explicit deltas**, not a reference to the rule that produced them. Replay is then exact regardless of what `package_size` later says. Recording a *past* value is not a duplicate under §3.6's test — it has no other source and therefore nothing to diverge from.

Combined with §3.5's inclusion of `ItemPackageSizeChanged`, you get both halves: history that is exact, and definitional changes that are auditable. Either alone leaves a hole.

#### What this rule does *not* license

An earlier draft extended it to storing resolved location *path strings* on every event referencing a node, so that history survived renames and re-parents. **That was correct only while tree operations sat outside the ledger.** Once `NodeCreated` / `NodeReparented` / `NodeArchived` became events, historical tree shape is derivable, and the snapshot became a cache stored inside the authoritative log — forbidden by `E7`.

The distinction is worth keeping straight, because the two cases look identical and aren't:

| | Recoverable from the ledger? | Verdict |
|---|---|---|
| Resolved `+2000 g` on `Opened` | **No** — `package_size` at that moment is not otherwise recorded | store it |
| Resolved path on `Moved` | **Yes** — replay the tree to that sequence | don't |

**History renders with present-day labels**, and that is the better behavior for this product, not merely an acceptable cost. If `Shelf 2` was renamed `Spice Shelf`, then *"Moved to Spice Shelf"* tells you where to look today; *"Moved to Shelf 2"* is archaeologically faithful and practically useless. Structural accuracy — the right node, the right containment at the time — is preserved either way.

### 3.8 Quantity has exactly two bases

A `BulkHolding`'s `quantity` counts either the Item's `content_unit` or whole packages.

| Holding | `quantity` | `unit_basis` | Reads as |
|---|---|---|---|
| A | 3 | `Package` | 3 unopened 2 kg bags |
| B | 800 | `Content` | the opened bag, 800 g left |

This is the structural form of "sealed vs. opened" — what makes that state derivable rather than stored. Constraining the basis to two values, rather than letting a Holding name any Unit, removes an entire class of validation: the only legal units are the Item's own content unit and its package.

**`H8` cannot be declarative, and the variant split is why.** It keys on `(item, stowed_location, unit_basis, expires_on)` — but §3.2 moved `unit_basis` onto `BulkHolding` while the other three live on the base. No single-table uniqueness constraint spans two tables, so the rule drops to `transactional`, upheld by the merge logic that `O1` already required. The null-equality problem — two null expiries must compare *equal*, which most engines refuse in a uniqueness constraint — becomes moot along with it.

This is the one place two good decisions genuinely traded against each other: eliminating nullable-variant muddle cost `H8` a declarative enforcement it would otherwise have had. Worth recording as a cost paid rather than a problem solved.

### 3.9 Where structural purity stops

`H7` — `unit_basis = Package` requires the Item to have a `package_size` — could be made structural by splitting one level deeper:

```
BulkItem
  ├── SimpleBulkItem     content_unit                 (thumbtacks, stamps)
  └── PackagedBulkItem   content_unit, package_size   (rice, printer paper)
```

**Not taken.** Recording a package size is an ordinary edit, and this would turn it into a row migration across variant tables. `H7` is one clearly-stated cross-entity rule; the split trades it for friction on a common operation. Two levels is the stopping point.

### 3.10 Identity is permanent; names are not constrained

**Identity** survives renaming, re-parenting, promotion, demotion, and archival, and is never reused. The ledger holds references indefinitely, so a reused identity would silently rewrite history.

**Sibling name uniqueness is deliberately absent.** Draft 1 had it as `C2`/`L2`. It was never an integrity rule:

- Identity is by id, never by name.
- `H8` keys on location *id*.
- Rollup, replay, and merge are all id-based.
- `path()` becomes ambiguous as a *display string* and remains exact as an id chain.

Soft delete forces the issue anyway — archiving `Shelf 1` and later creating a new `Shelf 1` under the same parent would violate it for no reason, requiring an awkward partial constraint. And real homes have two drawers called "junk drawer"; forcing `Junk Drawer (2)` is worse than allowing the duplicate.

**Demoted to product guidance:** a non-blocking warning — *"there's already a 'Shelf 1' here — add another?"* — which belongs in the UI, not the domain model.

### 3.11 Creation, and what establishes an initial value

Every section above reasons about *transitions* and assumes a starting state exists. Asking the complementary question — **what establishes each ledger-derived attribute's initial value?** — found two defects and one misclassification, so it belongs in the document as a standing check.

**Mechanically, `HoldingCreated` exists for exactly one attribute.** Walk the projection and ask what each attribute starts from: `quantity` starts at zero, `custody` at `AtRest`, and `custody_since`, `displaced_to`, and `retired_at` at null — all real zero values needing no payload. `unit_basis` needs nothing because it is immutable (§3.3). Only **`stowed_location`** has no meaningful empty value. So `HoldingCreated{stowed_location}` is the whole requirement, and it fits the existing **Placement** shape with `from_location` null — creation is a placement from nowhere. Seventeen Holding types, still thirteen shapes.

**But the load-bearing reason is independence, not missing zeros.** `stowed_location` *does* close backwards, exactly as Item's `content_unit` does: created at 7 and moved to 9 leaves `Moved{from: 7, to: 9}`, and a Holding never moved has its origin sitting in the current column with zero events. Backward closure is available. It simply cannot **verify** anything:

```
forward    ∅ → HoldingCreated{7} → Moved{7→9} → 9    vs stored 9   ✓ independent
backward   stored 9 → walk back → 7 → forward → 9    vs stored 9   ✗ circular
```

Backward closure begins from the value under test, so corruption in the stored column propagates into the derived origin and back out unchanged — `H10` would pass on corrupt data. Hence:

> **A creation event is required exactly when an entity's state is *verified*, because verification demands a reconstruction independent of the state being verified.** Entities that are only *audited* need none: audit reads backwards from current state and makes no independence claim.

That re-derives the Item answer rather than asserting it — nothing verifies Items, so backward closure is adequate there.

*Alternative considered and rejected:* an immutable `birth_location` column would also give an independent seed, with no event and no `H11`. Rejected because Location already records creation as an event (asymmetry for no reason), because §3.5 puts **existence** in the ledger, because a second location column beside `stowed_location` meaning something subtly different is exactly the muddle §3.2 exists to remove, and because the event carries `occurred_at` for free while the column would need a companion timestamp.

**Which decides who owns creation:**

> Entities whose ledger-derived state is **verified** — Holding, Location — are created **by the ledger**, through a creation event.
> Entities whose ledger-derived state is only **audited** — Item — are created **directly**.

The line is drawn by what *verification* needs, and it also resolves an identity problem: with allocated-on-insert identifiers the row must exist before an event can reference it, so a direct insert plus a separate ledger append would split one creation across two owners inside a single transaction. Putting creation in the ledger removes the split. `H11` and `L4` are the resulting invariants.

**Backward closure — and its precondition.** Items need no creation event because structural history reconstructs *backwards*: current value, plus each change event's `from_value`, and the earliest `from_value` is the birth value. That argument is only valid **while nothing is destroyed**, and `Promote` destroys the `BulkItem` row — taking `content_unit` and `package_size` with it, unrecoverably.

So `Promote` and `Demote` emit **up to three events**, not one: `ItemKindChanged`, plus `ItemUnitChanged{g → null}` and `ItemPackageSizeChanged{2000 → null}` for the discarded definition, reversed on `Demote`. That is `O1`'s "one event per fact changed" applied honestly. It needs no new type and no new shape, and it makes both sides of those shapes nullable — genuine optionality, since a `Unique` Item has no unit.

Forward replay from a creation event is *total*. Backward closure is *conditional*. Where both are available, prefer the former; where only the latter is, state the precondition rather than assuming it.

### 3.12 Identity is an input to the ledger, not an output

"Replay" names two different operations, and conflating them is what makes creation look paradoxical — creation allocates a new identifier, so how can it be replayed?

| | What it does | Do we need it? |
|---|---|---|
| **Projection replay** | For a *known* entity, fold its events to reconstruct its ledger-derived attributes. The identifier is the **filter key** — an input, never a folded value. Nothing is allocated. | **Yes.** This is all `H10`, `Verify`, and the nightly job do. |
| **World reconstruction** | Rebuild the database from empty by replaying everything. Requires identity to come *out of* the ledger. | **No.** |

So identity is allocated once, by the storage layer, and the ledger references it. Ordering inside the creating transaction is unproblematic: insert the row, obtain the identifier, append the creation event referencing it, commit. No observer sees an identifier without its creation event — which is `H11` and `L4`.

**The consequence, stated plainly because "event ledger" implies otherwise:**

> **The ledger is not a backup.** It cannot rebuild the database. It reconstructs the ledger-derived attributes of entities that already exist, and nothing more.

That is not a compromise; it is §3.6's projection model showing up again. This is *state with an authoritative ledger*, not event sourcing, and full reconstruction was never part of the bargain.

**Which makes `E4` load-bearing beyond referential tidiness.** Identity and every *immutable* attribute — `item`, `kind`, `unit_basis` — live on the row, not in the ledger. An orphaned event is therefore **uninterpretable**. `E4` is what makes the ledger mean anything at all.

The tempting fix — putting birth facts into `HoldingCreated`'s payload so events are self-describing — is **forbidden by `E7`**: those facts are derivable from the row. Relying on the row is safe precisely because `E4` guarantees it survives. The two rules cover each other, and neither works alone.

**Corollary: `Promote` replaces Holdings, it does not mutate them.** Mutating in place would change `kind` and destroy `unit_basis`, both immutable. So `Promote` retires the `Bulk` Holding and creates N `Unique` ones, each with its own identity and its own `HoldingCreated`. §5.1's `Split × N` implies this; the mutate reading would silently break `unit_basis` immutability and leave `H11` with a hole.

---

## 4. Derived values — never stored

Per §3.6, a derived value is one with a **defined winner** — recompute it and you get the truth. None of these are persisted as independent state.

The variant split (§3.2) changes the shape of this section in one important way: **several derivations now dispatch on kind rather than applying uniformly.** Those are marked. A derivation that reads `quantity` simply does not apply to a `UniqueHolding`, which has none.

### 4.1 The replay derivation

| # | Value | Definition |
|---|---|---|
| **D1** | `ledger_state(holding)` | the last `ReplayCheckpoint.projection` for that Holding, plus every Event with a greater `sequence`, applied in `sequence` order |

`D1` is authoritative for every attribute in the *ledger-derived* class (§3.3) — `stowed_location`, `retired_at`, and per variant either `quantity` or `custody`/`custody_since`/`displaced_to`. Not `unit_basis`, which is immutable and read off the row. It is **not** a read path (§3.6); it is what `H10` and the nightly integrity job compare against.

Everything below is computed from current state, not from replay.

### 4.2 Holding derivations

| # | Value | Applies to | Definition |
|---|---|---|---|
| **D2** | `active` | both | `retired_at` is null |
| **D3** | `depleted` | **Bulk only** | `quantity = 0` |
| **D4** | `content_quantity` | **Bulk only** | `unit_basis = Package ? quantity × item.package_size : quantity` |
| **D5** | `opened` | **Bulk only** | `item.package_size` is non-null ∧ `unit_basis = Content` |
| **D6** | `opened_at` | **Bulk only** | `created_at`, meaningful when `opened` |
| **D7** | `missing` | **Unique only** | `custody = Out` ∧ `displaced_to` is null |
| **D8** | `days_out` | **Unique only** | `now − custody_since` |
| **D9** | `effective_location` | both | Unique: `displaced_to ?? stowed_location`. Bulk: `stowed_location` |

`D3` and `D7` are worth reading together: **depletion is the Bulk end-of-life and has no Unique analogue** — a cable is `Gone`, never depleted. Conversely `missing` has no Bulk analogue, since an unaccounted-for quantity is a `Count` discrepancy, not a lost object. The variant split makes both facts structural rather than conventions.

### 4.3 Item derivations

| # | Value | Definition |
|---|---|---|
| **D10** | `on_hand(item)` | **dispatches on kind.** Unique: count of active `UniqueHolding`s. Bulk: Σ `content_quantity` over active `BulkHolding`s |
| **D11** | `stock_summary(item)` | Bulk with a package size: the `(open, sealed, total)` triple the domain model requires — "800 g open · 3 sealed (6 kg) · 6.8 kg total" |

`D10` is the clearest case of §3.2's split reaching §4. Draft 1 had a single formula, which quietly assumed every Holding had a quantity. It doesn't.

### 4.4 Tree derivations

Defined once and parameterized by entity (§3.1), so each applies to both Category and Location.

| # | Value | Definition |
|---|---|---|
| **D12** | `depth(node)` | length of the parent chain |
| **D13** | `path(node)` | ordered ancestor chain, root-first. Renders with **present-day** names (§3.7) |
| **D14** | `ancestors(node)` / `descendants(node)` | transitive closure through `parent` |
| **D15** | `rollup(node, metric)` | metric summed over `descendants(node) ∪ {node}`, excluding archived nodes |
| **D16** | `tree_at(sequence)` | *(Location only)* replay `NodeCreated` / `NodeReparented` / `NodeArchived` / `NodeRestored` to a sequence point |

**D15 is load-bearing.** It is the single derivation that lets Items sit at any node without breaking any report — the whole non-leaf decision rests on it.

**D16 is available but unused in V1.** Recorded because the Location ledger makes it derivable, and because it is the formal answer to "what did the tree look like in March." No V1 flow asks that; see §5.4 for what the Location ledger is actually for.

### 4.5 Report and nudge derivations

| # | Value | Definition |
|---|---|---|
| **D17** | `out_of_place` | active `UniqueHolding`s with `custody = Out`, ordered by `days_out` |
| **D18** | `lost` | active `UniqueHolding`s with `custody = Lost` — distinct from `missing` (`D7`), which is transient ignorance |
| **D19** | `expiring_soon(window)` | active Holdings with `expires_on ≤ now + window`, where `snoozed_until` is null or past |
| **D20** | `classification_nudge` | for each Category with ≥1 child: Items classified directly at it whose `placement_confirmed_at` is null. Fires **only** where a plausible sibling exists (domain model §2.7) |
| **D21** | `restock_candidates` | Items where `on_hand = 0`. No stored threshold — see §7.2 |

---

## 5. Operations

Every operation is atomic. Listed with the entities it writes and the events it appends.

Four cross-cutting rules govern all of them:

| # | Rule | Upheld by |
|---|---|---|
| **O1** | Any write that would violate `H8` **merges into the existing Holding** rather than creating a second, and appends `Merged`. | `transactional` |
| **O2** | `Promote` / `Demote` change `Item.kind`, swap the Item's variant row, and restructure every active Holding **in one unit**. A partially-promoted Item violates `H1` or `H2` and must never be observable. | `transactional` |
| **O3** | Every write to a ledger-derived attribute appends its Event **in the same transaction**. Neither is permitted without the other. This is §3.6's completeness rule. | `transactional` |
| **O4** | Inserting a base row and its variant row is one transaction (`I3`, `H5`). | `transactional` |

### 5.1 Item operations

| Operation | Writes | Events |
|---|---|---|
| `AddItem(kind, …)` | `Item` + one variant row | — |
| `RenameItem` · `ReclassifyItem` · `EditNotes` | `Item` | — |
| `ConfirmPlacement` | `Item.placement_confirmed_at` | — |
| `ArchiveItem` | `Item.archived_at` | — |
| `ChangeItemUnit` | `BulkItem.content_unit` | `ItemUnitChanged` |
| `SetPackageSize` | `BulkItem.package_size` | `ItemPackageSizeChanged` |
| `Promote` | `Item.kind`, variant swap, **retires 1 Holding and creates N** | `ItemKindChanged`, **`ItemUnitChanged`**, **`ItemPackageSizeChanged`**, `Split` × N, **`HoldingCreated` × N**, `Gone` |
| `Demote` | `Item.kind`, variant swap, **retires N Holdings and creates 1** | `ItemKindChanged`, **`ItemUnitChanged`**, **`ItemPackageSizeChanged`**, `Merged`, **`HoldingCreated`**, `Gone` × N |

**`AddItem` appends no event, and there is no `ItemCreated`.** The change events carry `from_value`, so the chain closes backwards: an Item's structural history is its *current* value plus every change event read in reverse, and the earliest event's `from_value` is the creation value. Adding a creation event would record something already implied — forbidden by `E7`.

**This holds only because `Promote`/`Demote` now record the definition they discard** (§3.11). Backward closure is valid while nothing is destroyed; promotion destroys the `BulkItem` row, so without the unit and package-size events the chain breaks and the Item's history becomes unreconstructible.

**Neither direction mutates a Holding in place** (§3.12): `kind` and `unit_basis` are immutable, so the old Holdings are retired and new ones created. **`Promote` is the common direction** and should be cheap: one Holding of quantity N becomes N Holdings of quantity 1, each inheriting `expires_on` and auto-labelled. `Demote` requires matching `stowed_location` and `expires_on` across all N, and is lossy going forward — history is retained, but no future event can address a single unit. Warn explicitly.

### 5.2 Holding operations

| Operation | Writes | Events |
|---|---|---|
| `CreateHolding(item, location, basis)` | `Holding` + one variant row | `HoldingCreated` |
| `Receive(item, location, qty, basis, expiry?)` | `Holding` + `BulkHolding`, or merges | `HoldingCreated`?, `Acquired`, `Merged`? |
| `Move(holding, to)` | `stowed_location` | `Moved`, `Merged`? |
| `Consume(item, qty)` | 1–2 `BulkHolding`s | `Split`?, `Opened`?, `Consumed` |
| `Open(holding)` | 2 `BulkHolding`s | `Split`, `Opened` |
| `Discard(holding, qty, reason)` | `quantity` | `Discarded` |
| `CheckOut(holding, to?)` | `custody`, `custody_since`, `displaced_to` | `CheckedOut` |
| `Return(holding)` | clears `custody_since`, `displaced_to` | `Returned` |
| `Rehome(holding, location)` | `stowed_location` | `Rehomed` |
| `MarkLost(holding)` / `Found(holding, location?)` | `custody` | `MarkedLost` / `Found` |
| `Count(location)` | `quantity` on N `BulkHolding`s | `Counted` × N, `Adjusted`? |
| `Verify(location)` | `custody` on N `UniqueHolding`s | `Verified` × N, `MarkedLost`? |
| `Retire(holding, reason)` | `retired_at` | `Gone` |
| `SetExpiry` · `Relabel` · `Snooze` | directly-mutable attributes | — |

**`Consume` implicitly opens.** If no `Content`-basis Holding exists and the Item has a `package_size`, the operation splits one off first — three events, **one user action**, per the domain model's Principle 02. The events are `Split{−1 package}` on the sealed Holding, `Opened{+package_size}` on the content Holding, and `Consumed{−qty}`. Each records a **resolved** delta (§3.7), so a later `SetPackageSize` cannot rewrite what they meant.

**`Count` and `Verify` are the split of draft 1's single `Count`.** `Count` observes a quantity on Bulk Holdings; `Verify` observes presence on Unique ones. Both always append their observation event, and append a correction only on discrepancy — `Adjusted` for a quantity mismatch, `MarkedLost` for a Unique Holding that isn't there. **Neither ever overwrites silently**, which is what makes the ledger trustworthy enough to derive consumption rates from.

**`SetExpiry`, `Relabel`, and `Snooze` append nothing.** They write directly-mutable attributes (§3.3), which replay does not reconstruct and does not claim to.

### 5.3 Category operations

| Operation | Writes | Events |
|---|---|---|
| `CreateCategory` · `RenameCategory` · `ReparentCategory` · `ArchiveCategory` | `Category` | — |

**The whole table is empty on the right.** Category has no ledger at all (§3.5). This asymmetry with §5.4 is the clearest demonstration that Category and Location are not the same entity wearing different names.

### 5.4 Location operations

| Operation | Writes | Events |
|---|---|---|
| `CreateNode` | `Location` | `NodeCreated` |
| `RenameNode` | `Location.name` | — |
| `ReparentNode` | `Location.parent` | `NodeReparented` |
| `ArchiveNode(resolution)` | `Location.archived_at`, N Holdings' `stowed_location` | `NodeArchived`, `Moved` × N |
| `RestoreNode` | `Location.archived_at` | `NodeRestored` |

**What the Location ledger is actually for: completeness of explanation.**

Not historical rendering — §3.7 established that history reads better with present-day labels. The requirement is that **the ledger can account for every apparent movement.** Re-parent `Storage Tote #3` from Attic to Garage, and the winter coat now shows as being in the Garage. Its `stowed_location` never changed and no `Moved` event exists for it — so without `NodeReparented`, something moved and *nothing in the system records why*.

That is the test, and it draws the line cleanly:

| Operation | Can it make a Holding appear to have moved? | Event |
|---|---|---|
| `RenameNode` | no — same place, new label | — |
| `ReparentNode` | **yes** — the place itself moved | `NodeReparented` |
| `ArchiveNode` | **yes** — contents were lifted elsewhere | `NodeArchived` + `Moved` × N |
| `CreateNode` | no, but replay needs the node to exist | `NodeCreated` |

**`ArchiveNode` is not an exception to anything.** It writes two kinds of event because it changes two kinds of fact: the tree's structure, and N Holdings' containment. Per-Holding replay therefore stays independent — which is what keeps the nightly job cheap and its failures localizable to a single Holding.

`resolution` is one of **Lift** (reattach children and contents to the parent), **Move** (reattach to a chosen node), or **Block** (refuse while non-empty). Default is Lift, which matches how reorganization actually proceeds.

---

## 6. Deletion and referential rules

### 6.1 Nothing the ledger references is ever hard-deleted

`E4`. Applies to **Location**, **Item**, and **Holding**. Archival (`archived_at`, `retired_at`) removes a row from active views, pickers, and `rollup` — and does nothing else. Parent pointers and all attributes are retained; they are part of the historical record.

**Category is soft-deleted too, though nothing forces it.** No Event references a Category, so it could be hard-deleted safely. Uniformity of the tree abstraction (§3.1) is worth more than the saved column, and the day a `Reclassified` event is wanted, the choice will already be right.

### 6.2 Tree nodes require resolution, not cascade

Archiving a node with children or contents asks for a `resolution` — **Lift**, **Move**, or **Block** (§5.4). Re-parenting must not create a cycle (`C1`, `L1`). Sibling name collisions are permitted (§3.10).

### 6.3 Variant rows cascade with their base

`UniqueItem` / `BulkItem` and `UniqueHolding` / `BulkHolding` have no independent lifecycle. They are created and archived with their base row, and swapped only by `Promote` / `Demote` under `O2`.

### 6.4 Events are never deleted or updated

`E1`. Corrections are compensating events. This is what makes `D1` meaningful, and it is why §3.4 assigns the ledger the highest integrity investment in the system: there is no later write that can repair a malformed row.

### 6.5 ReplayCheckpoints are freely deletable

`K2`. The exact inverse of §6.4, and the contrast is the point: delete every checkpoint and you lose performance, never information. It is the clearest illustration of §3.4's proportionality rule.

### 6.6 Creation is owned by whoever must verify it

The mirror of §6.1's deletion rules. Holdings and Locations are created **by the ledger**, through `HoldingCreated` and `NodeCreated`; Items are created directly. §3.11 gives the reasoning — verification needs an origin independent of the state under test — and `H11`/`L4` the invariants.

The practical consequence for any implementation: there is no code path that inserts a Holding or Location row outside the ledger, which makes `H11` and `L4` hold by construction rather than by discipline.

`HoldingCreated` is kept as a **distinct event type** rather than encoded as a null-from `Moved`, even though the two share the Placement payload byte for byte. The cost of the distinction is one enum value — no table, no shape, no struct. What it buys: creation is *existence* and a move is *containment*, two different categories under §3.5, and keeping them distinct puts the "creation occurs exactly once, first" rule at a dispatch point the exhaustiveness machinery can see, rather than inside a null check within `case Moved`.

---

## 7. Gaps found in the domain model

The point of a schema pass is to surface what prose let slide.

### 7.1 "Dismissible" nudges had nowhere to store dismissals — resolved

Principle 04 requires every nudge to be dismissible and the expiry flow lists `Snooze` as an action, but the domain model defined no place to record either. Without one a dismissed nudge returns on next load, which is exactly the wallpaper failure the principle exists to prevent.

Resolved with two narrow attributes rather than a general registry: `Item.placement_confirmed_at` silences `D20`; `Holding.snoozed_until` covers **both** `D17` and `D19`, which is why one attribute serves two nudges. A general `Dismissal(subject, nudge_kind, until)` entity is the reversal path if a third nudge with different semantics appears.

### 7.2 The restock prompt had no threshold — resolved, with a caveat

Two flows fire a "prompt to restock," but par levels were cut alongside `Wanted`/`Ordered`, leaving the prompt with nothing to test against.

Scoped to the derived depletion transition only (`D21`): it fires when `on_hand` reaches 0, needing no stored threshold. `Item.par_level` is purely additive — it changes the trigger from `= 0` to `< par_level` and nothing else. Worth flagging that *"warn me before I run out"* is the more useful behavior, so this is likely the first thing added back.

### 7.3 ~~The ledger has two subject types~~ — **retracted**

Correct given draft 1's event list. §3.5 makes that event list wrong instead: `Renamed` and `Reclassified` are labels and never belonged in the ledger, so the finding is superseded rather than resolved. Retained as a retraction because it was published as a finding. The ledger has **three** subjects, on entirely different grounds.

### 7.4 `on_hand` is not one formula — new

The domain model's "total on hand" and its stock displays assume a single quantity to sum. `Unique` Holdings have no quantity; on-hand for them is a **count of Holdings**. `D10` dispatches on kind.

Harmless once seen, and it is the clearest example of the variant split (§3.2) propagating into derivations. It also means any UI that shows "how much do I have" needs two renderings, not one with a unit label swapped.

### 7.5 Verifying a Unique Holding had no operation — new

The domain model's count flow says *"not-found `Unique` Holdings become Missing."* But `Count` observes a quantity, and a `Unique` Holding has none — the flow silently assumed one operation covering two semantics. Splitting `Counted` / `Verified` (§1.6) forces a matching split at the operation level, and `Verify` (§5.2) is a genuinely new operation the domain model never named.

### 7.6 Reorganization was silently unaccountable — new

The domain model treats the Location tree as freely editable and the ledger as the record of how things got where they are. Those are inconsistent: re-parenting a container moves its contents with no Holding event, so the coat appears in the Garage with nothing explaining the move.

Resolved by the Location ledger (§5.4) under an explicit requirement the domain model never stated: **every apparent movement must be accountable.** That requirement, not "physical vs. descriptive," is what decides which tree operations are events.

### 7.7 Item archival was unmodelled — new

The domain model has `Gone` for Holdings and says Items are "archived," but never distinguishes retiring a *kind* from retiring a *thing*. They are different: `ArchiveItem` is a label-level action appending no event (§5.1); `Retire` is a physical fact appending `Gone` (§5.2). Archiving an Item with active Holdings should be blocked, since the Holdings would become unreachable through any active view.

---

### 7.8 Nothing established initial values — new, found in *this* document

Unlike §7.1–§7.7, this one is a defect in the conceptual schema rather than the domain model, surfaced by planning the implementation.

Every section reasoned about *transitions* and assumed a starting state existed. Nothing asked **what establishes each ledger-derived attribute's initial value.** Asking it produced one correctness bug (`stowed_location` unrecoverable without `HoldingCreated`), one history loss (`Promote` destroying the Item definition), and one misclassification (`unit_basis` was never ledger-derived at all).

Resolved in §3.11, which is written as a standing check rather than a fix, because the question generalizes: *transition rules are only half a specification.*

### 7.9 "Replay" meant two things — new, found in *this* document

The docs used one word for *projection replay* (fold a known entity's events) and *world reconstruction* (rebuild the database from nothing). Only the first is needed, but nothing said so, which made creation look paradoxical: if replay reconstructs state, how can it reconstruct an allocated identifier?

Resolved in §3.12: identity is an input to the ledger, not an output. The ledger is explicitly **not a backup**. That also promotes `E4` from referential tidiness to a load-bearing rule — immutable attributes live on the row, so an orphaned event is uninterpretable — and surfaces that `Promote` must replace Holdings rather than mutate them.

### 7.10 Backward closure was asserted, not conditioned — new, found in *this* document

Draft 2.2's §5.1 justified having no `ItemCreated` by claiming structural history "closes backwards" from current state plus each change event's `from_value`. True — but only while nothing is destroyed, and `Promote` destroys the `BulkItem` row.

The argument was stated unconditionally when it had a precondition. It is now stated with one, and the precondition is enforced by the three-event `Promote` (§3.11).

The general form is worth keeping: **forward replay from a creation event is total; backward closure is conditional.** Where both are available, prefer the former. Where only the latter is, state the precondition rather than assuming it.

---

## 8. Open questions for the technical phase

Deliberately unsettled. Each is an implementation decision the conceptual model constrains but does not determine.

| # | Question | Constrained by |
|---|---|---|
| 1 | ~~**Null-equality in `H8`**~~ — **closed.** `H8` cannot be declarative at all: `unit_basis` lives on the variant table while its other keys live on the base, so no single-table constraint spans it. It is `transactional`, upheld by merge logic. | §3.8 |
| 2 | **Enforcing "at least one variant row."** Not declaratively expressible. Deferred constraint, trigger, or consistency test. | `I3`, `H5` are the only `checked` shape rules left — §3.2 |
| 3 | **Event assembly.** Reading an event with its payload is a 13-way dispatch. One left-join per shape, a union view, or per-shape repositories. | 13 shape tables — §1.6 |
| 4 | **Decimal representation.** Float storage accumulates error across replay, and `H10` compares for equality. Fixed-point or integer minor units. | `H10`, `D1` |
| 5 | **`sequence` scope.** Global, or per-subject. Affects `E2`, replay cost, and whether cross-subject ordering is meaningful. | `E2`, `E3` |
| 6 | **Tree traversal.** Recursive CTEs, materialized paths, or closure tables. | `D12`–`D16` define what must be answerable, not how |
| 7 | **Checkpoint cadence and `projection` format.** Both free choices, because checkpoints are rebuildable. | `K2`, §3.4 |

Note the shape of this list: **every item is a mechanism question, not a semantic one.** Where an earlier draft left semantics open — what replay is scoped to, what the ledger records — those are now settled in §3 and appear here only as constraints.

---

## 9. Verification

### 9.1 Structural acceptance

The schema is correct when every acceptance walkthrough in domain model §6 traces through §5's operations **without introducing an entity, attribute, or event type not listed here**. Two that exercise the most structure:

**Walkthrough 02 — rice, first use.** `Consume(rice, 100 g)` finds no `Content`-basis Holding, so it emits `Split{−1 package}` on the sealed `BulkHolding`, `Opened{+2000 g}` on a newly created one, then `Consumed{−100 g}`. Three events, one user action. `H4`, `H7`, and `H8` hold throughout, and every delta is resolved (§3.7) so a later `SetPackageSize` cannot rewrite them.

**Walkthrough 00 — the Holding that never moves.** Create a Holding on a shelf and do nothing else. Replay it from zero and assert it equals stored state. The cheapest possible test, and the one that catches a missing `HoldingCreated` immediately — which is precisely how that defect was found. Every acceptance list should open with the do-nothing case.

**Walkthrough 08 — promote one of six cables.** `Promote` writes `ItemKindChanged`, swaps `BulkItem` for `UniqueItem`, and replaces one Holding of quantity 6 with six of quantity 1 — each a base row plus a `UniqueHolding` row, each labelled, each inheriting `expires_on`. `H1` holds for all six *only* if the whole thing is atomic. That is `O2`'s reason for existing.

### 9.2 Ledger consistency — the three-way property test

The strongest available check, and the one that keeps §3.6 honest rather than aspirational:

1. Apply a random sequence of §5 operations to the **real system**.
2. Apply the same sequence to a **naive in-memory model** with no ledger and no variants.
3. Assert the two agree on every derived value in §4.
4. **Replay the ledger from zero** (`D1`) and assert it agrees too.

Divergence in any pair localizes the defect immediately: 1↔2 is an operation bug, 1↔4 is a completeness bug (`O3` violated — a write without its event), 2↔4 is a replay bug.

### 9.3 The nightly integrity job

The operational form of `H10`, and what makes `ReplayCheckpoint` pay for itself.

For each Holding: replay from its last checkpoint, diff against stored ledger-derived attributes, report discrepancies, write a fresh checkpoint. Replay cost is bounded to one day of events rather than all history, and a failure localizes to one Holding.

**A discrepancy is a defect report, never a repair.** Silently correcting stored state would destroy the only signal that completeness (`O3`) was violated somewhere — which is precisely the failure the ledger exists to make visible.

### 9.4 Invariant coverage

Each invariant should be verifiable by its tag:

| Tag | How it is verified |
|---|---|
| `structural` | by inspection — no test needed, and none possible |
| `declarative` | by the storage layer rejecting the write; one negative test per constraint |
| `transactional` | by a failure-injection test asserting no partial state is observable |
| `checked` | by an explicit test or job — the only tag requiring ongoing vigilance |

The `checked` set is the real maintenance surface, and it is deliberately small: `C1`/`L1` (acyclicity), `U1` (dimension consistency), `I3`/`H5` (variant row presence), `H7` (package basis), `H9` (retired exclusion), `H10` (ledger consistency), `K1` (checkpoint accuracy), `E6`/`E7` (resolved, non-derivable payloads).

Nine rules, none of them shape rules. Draft 1 had shape rules in this list; §3.2 removed them.

Draft 2.3 left this set **unchanged**: `H8` moved from `declarative` to `transactional`, and the new `H11`/`L4` are `transactional` by construction (§6.6). Findings that add rules without adding checked rules are the good kind.
