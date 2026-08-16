# Home Management System — Domain Model (V1)

> Status: approved 2026-08-15. This is the canonical domain reference. It is deliberately
> pre-technical — taxonomy, ontology, state machines, and flows. No schema, no packages, no UI.
> Changes to the concepts here should be made in this document first.

## Context

This system replaces `home-inventory-system/`, which grew piecemeal and whose data model reflects that. There, one table (`item_types`) served as both the taxonomy *and* the container for items; `items` carried `quantity`/`unit_type` inline with no notion of batches, expiry, custody, or history. A leaf-only invariant was enforced by SQLite triggers, one of which had to be dropped (`00003_drop_update_item_trigger.sql`) because reorganization fought the constraint.

The new system keeps the same tech stack (Go, Bubbletea TUI, SQLite, SQLC, Goose) but rebuilds the product from scratch, targeting a superset of the old functionality.

Scope is a single-user home: cables, clothes and boxes in storage, thumbtacks, stamps, spices, medicines, pantry staples.

### The governing constraint

**Adoption is the bottleneck, not expressiveness.** A model nobody enters data into is worth zero regardless of how well it captures reality. Every concept below has been filtered by one question: *does this ever force the user to answer something they don't care about?* Concepts that are derived, or default correctly, or only materialize when the user does the thing that needs them, are free and stay. Everything else was cut, and the cuts are recorded in §5 so they can be reversed knowingly.

### Decisions

| Decision | Choice |
|---|---|
| Items at non-leaf categories and locations | Allowed. UI nudges, never constraints. |
| People / multi-user | None. Single user. |
| History | Full event ledger; quantity derived and checkpointed. |
| Expiry | One optional date on a Holding. Nothing more. |
| Custody | Two states: `AtRest` / `Out`. |
| Containers | Deferred — covered 90% by Location re-parenting. |

---

## 1. Design Principles

These do most of the work of keeping the surface small. They are listed first because several concepts below only survive because of them.

**1. Derive, don't store.** Anything computable is not a field and not a decision. `Depleted` is quantity == 0. `Missing` is `Out` with no known location. Whether a bag is opened is implied by its unit. When a bag was opened is its Holding's creation timestamp. Each of these was a stored state in an earlier draft and is now free.

**2. Never block capture.** Filing at a coarse category or location is legal. Consuming from an unopened package opens it implicitly. Any moment where the system says "you can't do that yet" is a moment the user stops entering data.

**3. The ontology is invisible.** The model has a 2×2 underneath; the user sees three buttons and never reads the word "fungible."

**4. Nudges must be specific and dismissible.** A generic, undismissable warning list becomes wallpaper — and then it is worse than no nudge, because it trains the user to ignore the UI. Every nudge names the items and offers the fix.

---

## 2. The Ontology

### 2.1 Five concepts, three layers

```
TYPE LAYER            Category  ──classifies──▶  Item
 (what a thing is)     (tree)                    (a kind: "Basmati Rice")
                                                     │
                                                     ▼
INSTANCE LAYER                                    Holding
 (what I have, where)                    (this rice, here, this much)
                                                     │
                                                     ▼
SPATIAL LAYER                                     Location
 (where things are)                                (tree)

HISTORY LAYER          Event ── immutable record of every change above
```

- **Category** — recursive tree. Pure classification. "Spices → Dried Peppers."
- **Location** — recursive tree. Physical places. "Kitchen → Spice Cabinet → Shelf 2." Subtrees can be re-parented.
- **Item** — a *kind* of thing. Classified into exactly one Category. Owns the tracking policy.
- **Holding** — a tracked physical presence of an Item at a Location.
- **Event** — an immutable record of something that happened to a Holding.

**Why the Item/Holding split carries the model.** One Item, many Holdings: rice in the pantry and rice in the garage; the sealed bags and the open one. Search resolves an *Item* and lists its Holdings with location paths. It is also what makes non-leaf classification safe — two piles filed at different depths pointing at the same Item reconcile at the Item level regardless of where they were classified.

### 2.2 Item kinds

The honest underlying model is two independent axes — **Identity** (each unit tracked separately, or interchangeable) and **Consumability** (does use destroy it) — giving four cells:

|  | **Identified** | **Pooled** |
|---|---|---|
| **Durable** | HDMI cable, laptop, coat, empty computer box | thumbtacks, zip ties, binder clips |
| **Consumable** | the opened bag of rice, an open bottle of medicine | stamps, printer paper, sealed cans |

**At the product surface this reduces to two kinds.** Trace what the consumability axis was actually driving, and each driver turns out to be gone or shared:

| Driver | Status |
|---|---|
| `Seal` state | Derived from unit + Holding creation time (§2.4) |
| `Open` operation | Kept, but implicit — never a user decision (§4.1) |
| `Depleted` | Derived (qty == 0), and **not consumable-specific** — running out of thumbtacks is the same event as running out of rice |
| expiry | Optional field on any Holding, not exclusive to consumables |

What is left of "consumable" is package size — and a box of 500 thumbtacks is a package too — plus whether the unit is grams or counts, which is just the unit picker. A pooled durable and a consumable end up with identical operations (`Receive, Consume, Move, Count, Discard`), identical states, and identical fields.

The deeper reason: **durable vs consumable is a classification fact, and there is already a classification system.** `Spices`, `Medicines`, and `Pantry` are consumable branches; `Cables` and `Tools` are durable ones. Any report needing the distinction reads it off the Category tree rather than a duplicated Item field. The boundary is fuzzy anyway — batteries, glue, and sunscreen have shelf lives while behaving like durables — which is usually a sign it should not be a hard enum.

What *does* drive behavior, and survives untouched, is custody: home vs. current location and the checkout cycle, which apply only to identified things, whose quantity is structurally 1 and whose history is therefore addressable per unit.

*Cost of this collapse:* `Bulk` spans a wide range and the unit picker carries more weight. If behavior later genuinely needs the split — e.g. suggesting expiry dates only for food — it returns as one nullable field, derivable from category in the meantime.

| Kind | Quantity | Custody | Examples |
|---|---|---|---|
| **`Unique`** | always 1 | yes | cable, laptop, coat, empty computer box |
| **`Bulk`** | a number + unit | no | thumbtacks, stamps, rice, medicine, printer paper |

Presented as three buttons, because a preset that also picks the unit is friendlier than an abstract toggle:

| Button | Sets |
|---|---|
| "One of a kind" | `Unique` |
| "A pile I count" | `Bulk`, unit = Count |
| "Something I measure" | `Bulk`, unit = mass/volume picker |

Within `Bulk`, identified-vs-pooled is **per-Holding and derived, never chosen**: a Holding measured in packages is unopened stock; one measured in content units is the opened package. See §2.4.

**Fungibility is a tracking decision, not a property of the object.** Thumbtacks *could* be tracked individually; nobody does. You might track the one good 100 W USB-C cable and pool the other five. So `Unique` ⇄ `Bulk` must be changeable after the fact — see Promote/Demote in §3.4.

### 2.3 Batches, and why expiry works without violating fungibility

Expiry attaches to a **batch**, not to a kind and not to an undifferentiated pool. Six cans bought together expiring 2027-03 are one batch of quantity 6; units within a batch are interchangeable, batches are distinguishable.

**Batch is not a sixth concept — a Holding *is* the batch.** Two Holdings of the same Item at the same Location with different expiry dates are simply two Holdings. This is what lets pooled items carry expiry at Holding granularity without contradiction, and it is why a single optional `expires_on` date is all expiry costs.

The merge rule falls out: two Holdings may merge **iff** same Item, same Location, `Bulk`, and identical expiry.

### 2.4 Sealed and opened, without a seal state

Real state: three sealed 2 kg bags plus one open bag with 800 g left.

| Holding | Quantity | Unit |
|---|---|---|
| A | 3 | packages |
| B | 800 | grams |

**The seal state is already implied by the unit** — for an Item with a package size, a package-unit Holding is unopened stock and a content-unit Holding is opened stock. "Opened at" is Holding B's creation timestamp, since B came into existence at the moment of opening. Both free.

`Open` is therefore not a state change but a **unit-converting split**: remove 1 from A, add 2000 g to B. And per Principle 2 it is never a step the user takes deliberately — see §4.1.

### 2.5 Units

A Unit belongs to a **Dimension**: `Count`, `Mass`, `Volume`, `Length`.

- Conversion within a dimension is free (g ↔ kg).
- Operations must be dimension-consistent — consuming 100 ml from a mass-tracked Holding is rejected, not coerced.
- Cross-dimension conversion (density) is out of scope.
- **Package size** is the sanctioned per-Item bridge from `Count` to another dimension: "1 bag = 2000 g."

`Length` here is a *quantity* dimension (a spool of wire), distinct from length as a *description* ("2 m HDMI cable"). A 2 m cable is one `Count` whose length lives in its name or notes. Conflating these is a common trap.

### 2.6 Custody, without people

With no Person concept, checking something out means: **a deliberate displacement with intent to return.** That requires every `Unique` Holding to carry two locations — **home** (where it belongs) and **current** (where it is) — with `AtRest ⟺ current == home`.

| State | Meaning |
|---|---|
| `AtRest` | at its home location |
| `Out` | taken from home; optional current location, timestamp required |

Two states, not four. Checkout is one keystroke with no follow-up question. **`Missing` is derived**: it is `Out` with no current location — which is already what you get when you check something out in a hurry.

This unlocks the most valuable query in a home-management system: *what is out of place, and for how long?* A cable out for three weeks is not "in use" — it has been lost or silently relocated, and the system should ask which.

*Extension point:* if People are ever added, `Out` grows an optional counterparty and becomes a loan. Nothing else changes.

### 2.7 Categories, locations, and the nudge that replaces the constraint

Items classify and locate at **any node**. Rollup queries sum descendants, so "what's in the Kitchen?" and "show me all Spices" work regardless of depth. Creating a subcategory under a populated parent is free and migrates nothing.

The organizational pressure the leaf rule used to provide comes from one **specific, dismissible** nudge:

> *"8 items sit at `Spices`; `Dried Peppers` exists — do any belong there?"* → batch-move picker

It fires only when a plausible sibling exists, i.e. only when you have already created a subcategory and are in an organizing mood. It never fires on a category with no children. This is deliberately narrower than a "you filed this too shallow" nag, which is the version that becomes wallpaper.

---

## 3. State Transitions

### 3.1 `Unique` Holding — cable, laptop, coat, empty box

```
   AtRest ──── CheckOut ────▶ Out (where?, since)
      ▲                          │
      └──────── Return ──────────┘
      │
      └──── Found ──── (Lost)

   any state ──▶ Gone   (deliberate, terminal)
   any state ──▶ Lost   (unintentional, recoverable via Found)
```

| Transition | Effect |
|---|---|
| `CheckOut` | `Out`, timestamp recorded; current location optional |
| `Return` | `AtRest`, current := home |
| `Rehome` | changes home without moving it — "this lives in the garage now" |
| `Gone` | terminal, no follow-up. Record persists in history |
| `Lost` | terminal-ish, stays searchable, reversible by `Found` |

`Gone` vs `Lost` earns its keep on **behavior, not taxonomy** — Lost has a follow-up path, Gone does not. Finer reasons (donated/sold/returned) only matter for value reporting, which is out of scope.

**Nudge:** an `Out` Holding older than N days prompts *"still using this, or did it move?"* offering **Return**, **Rehome**, **Lost**. This is where separating home from current pays off; without the nudge, checkout is write-only and the data rots.

### 3.2 `Bulk` Holding — rice, thumbtacks, stamps, medicine

```
   Receive (+q) ──▶ OnHand ──▶ Consume (−q)
                      │  ▲          │
                 Move │  │ Count    │ q → 0
                      ▼  │          ▼
                   OnHand @ elsewhere    Depleted (derived, terminal)

   OnHand ──▶ Discard (−q, spoiled/expired/broken)
```

No custody, no seal, no condition. `Depleted` is derived from quantity == 0, not stored — and it is the moment the restock prompt fires, when the user has the empty bag in hand.

For Items with a package size, `Open` splits a package-unit Holding into a content-unit one (§2.4), triggered implicitly by `Consume` (§4.1).

### 3.3 Nothing else has states

Categories and Locations have no lifecycle — they are created, renamed, re-parented, and deleted (with contents resolved to the parent). Items have no lifecycle; only Holdings do. Keeping state machines confined to one concept is a large part of why the surface stays small.

### 3.4 Changing tracking mode

| Operation | Semantics |
|---|---|
| **Promote** (`Bulk` → `Unique`) | One Holding of quantity N becomes N Holdings of quantity 1, each inheriting expiry and auto-labelled (`USB-C Cable #1…#N`), renameable. Safe and cheap. |
| **Demote** (`Unique` → `Bulk`) | N Holdings collapse into one of quantity N. Requires same Location and compatible expiry. **Lossy going forward** — history is retained but no future event can address a single unit. Warn. |

Promote is the common case and worth making easy: you pool "USB-C cables ×6," then discover one is the good 100 W one.

### 3.5 The event ledger

Current quantity is derived: a periodic checkpoint plus subsequent events.

| Event | Records |
|---|---|
| `Acquired` | +qty, source, price, date, expiry |
| `Moved` | Holding, from → to Location |
| `Consumed` | −qty, normal use |
| `Discarded` | −qty with reason |
| `Opened` | system-generated split of a package |
| `CheckedOut` / `Returned` / `Rehomed` | custody |
| `MarkedLost` / `Found` | custody |
| `Counted` | an observed quantity |
| `Adjusted` | the delta a Count implied, discrepancy explicit |
| `Split` / `Merged` | system-generated topology changes |
| `Reclassified` | Item's Category changed |
| `Promoted` / `Demoted` | tracking mode changed |
| `Gone` | terminal disposition |

**`Counted` and `Adjusted` are deliberately separate.** A physical count that disagrees with the ledger never silently overwrites it — it emits both the observation and an explicit adjustment carrying the discrepancy. That is what makes the ledger trustworthy enough to derive consumption rates from later.

---

## 4. User Flows

### 4.1 Consume 100 g of rice

The flow the whole `Bulk` model exists to serve, and the clearest test of Principle 2.

1. Item `Basmati Rice` → Holdings: `3 packages @ Pantry Shelf 1`, `800 g @ Pantry Shelf 1`.
2. `Use 100 g` → applies to the content-unit Holding. Now 700 g.
3. **If no opened Holding exists, the system opens one silently** — two events, one toast: *"Opened a bag. 1900 g open · 2 sealed."* No confirmation, no separate "open" action in the UI.
4. At quantity 0 → derived `Depleted`. If no sealed stock remains, prompt to restock right there.

Always display both framings: **"800 g open · 3 sealed (6 kg) · 6.8 kg total."** Users think in both, and package size makes either computable.

### 4.2 Use a cable and put it back

1. `USB-C to HDMI Cable`, `Unique`, home = `Office → Desk → Drawer 2`.
2. `CheckOut` → `Out`, timestamped. One keystroke, no questions.
3. `Return` → `AtRest`.
4. If never returned: the out-of-place queue surfaces it after N days with **Return / Rehome / Lost**.

### 4.3 Find something

Search resolves an **Item**, then lists every Holding with its full path:

```
Ancho Chile   120 g          Kitchen → Spice Cabinet → Shelf 2
Ancho Chile   1 package      Garage → Overflow Shelf
```

Two piles, one answer. This is where the Item/Holding split is most visible to the user.

### 4.4 Bulk capture, refine later

The flow the non-leaf decision exists to enable.

1. In the garage, add 20 things fast. Category defaults to the coarsest node you are confident about (`Electronics`); location to the coarsest real place (`Garage`).
2. Nothing blocks. No `Misc` node gets invented.
3. Later, if and only if a plausible sibling category exists, the specific nudge from §2.7 offers a batch move.

### 4.5 Count and reconcile

1. Pick a Location, walk its Holdings, enter observed quantities.
2. `Counted` for each, `Adjusted` for every discrepancy with the delta explicit.
3. Not-found `Unique` Holdings become `Out` with no location (= derived `Missing`), never deleted.

Pooled durables will never reconcile exactly. `Count` is the primary correction mechanism for them, not an exception path.

### 4.6 Expiry check

One query, one list, sorted by date: what expires soon. Actions per row: `Discard`, `Use`, `Snooze`, `Edit date`.

---

## 5. Cut, and Why It's Safe to Reverse

Recorded so these can be reintroduced knowingly rather than rediscovered.

| Cut | Reason | Cost of reversing later |
|---|---|---|
| **`Condition`** (New/Worn/Broken) | Nothing in the model reads it; no good default; a field at capture with no payoff. | Nil — a leaf field nothing depends on. |
| **`Seal`** as a state | Fully derivable from unit + Holding creation time (§2.4). The *behavior* is kept. | N/A — it was never needed as state. |
| **Category attributes** | A whole schema-inheritance system for V1. | Additive. **Note the collateral damage:** it was going to power a specific tidy-up nudge; §2.7's sibling-based nudge replaces it and is cheaper. |
| **Items as containers** | Real product complexity — a Location tree mixing places and possessions, cascade-move semantics, non-empty-retire resolution. | **90% already covered** by re-parenting Location subtrees; a tote is just a Location. Reversing means adding a link from Item to Location, no reshaping. |
| **`Wanted` / `Ordered`** | A category error, not just scope: a Holding is a *physical presence*, and something you want does not physically exist. Phantom rows would pollute every list. | If a shopping list appears, it is a separate list keyed by Item — cleaner than a lifecycle state anyway. |
| **Disposition reasons** (donated/sold/returned) | Only serve value/insurance reporting, which is out of scope. | Additive detail on the `Gone` event. |
| **Attention framework** (warranty, return window, service due) | Five date kinds where one suffices. Return windows shoehorn into `expires_on`. | Additive; the queue generalizes when a second date kind earns its place. |
| **Custody states** `InUse` / `OutOfHome` / `Missing` | For one person, InUse vs OutOfHome drives no different behavior. `Missing` is derivable. | Additive. |
| **People / lending** | Out of scope by decision. | `Out` grows a counterparty. |
| **Kits & composites** | Needs Holding-contains-Holding, distinct from containment-as-location. | New relation, no reshaping. |
| **Cross-dimension conversion** | Package size covers the practical cases. | Additive. |
| **Barcodes, photos, receipts** | Capture *mechanisms*, not model concepts. | Attach to Item and the `Acquired` event. |

---

## 6. Acceptance Walkthroughs

The model is correct when each of these resolves **without adding a concept and without asking the user a question they do not care about**. These double as the acceptance criteria for the eventual implementation.

1. **Rice** — 3 sealed 2 kg bags plus 800 g open; use 100 g; see package and mass totals. *(§4.1)*
2. **Rice, first use** — same, but no bag is open yet. Must take exactly one user action. *(§4.1 step 3)*
3. **Cable** — check out in one keystroke, fail to return, get nudged, rehome it. *(§4.2)*
4. **Empty computer box** — a tracked possession, in the attic, containing nothing, representable as such. *(§2.2)*
5. **Thumbtacks** — `Bulk`, never precisely reconciled, corrected by Count. *(§4.5)*
6. **Stamps** — `Bulk`, no per-unit identity, no expiry, decrements on use. *(§3.2)*
7. **Medicine** — `Bulk` with an expiry date; appears in the expiry list. *(§4.6)*
8. **Six USB-C cables** — `Bulk` today; promote one to `Unique` when it turns out to be the good one. *(§3.4)*
9. **Spices → Dried Peppers** — create under a populated parent with zero migration, then get the specific sibling nudge. *(§2.7)*
10. **Storage tote** — re-parent the Location from Attic to Garage; contents follow. *(§5, containers row)*
11. **Winter coat** — marked `Gone`; leaves active views, remains in history.

Any walkthrough that requires a sixth concept means the model is wrong, not that the flow is exotic. Any walkthrough that requires more than two user actions means Principle 2 was violated.

---

## 7. Roadmap

1. ~~Domain model~~ — this document.
2. **Conceptual schema** — entities, relations, invariants. Still technology-free.
3. **Technical design** — SQLite schema + Goose migrations, SQLC queries, domain package, service commands/queries, Bubbletea TUI.
