# Home Management System — Interface Design (V2)

> Status: **proposed**, 2026-09-19. Supersedes nothing yet; the V1 interface in
> `internal/tui` is what ships today.
>
> This document is about **form**, not function. The domain model, the conceptual
> schema, the command grammar and the ledger are unchanged and out of scope.
> Where this document and [`domain-model.md`](./domain-model.md) disagree, the
> domain model wins.
>
> An interactive prototype of everything below — the seed house, the lens flip,
> the move, the import drawer, the walk — is `ui-proposal.html` in this
> directory, and is published at
> <https://claude.ai/artifact/2tJmhxe4JaNw2UW1EbCRBr>.

## 0. The complaint, and the one cause under it

Six symptoms were reported: navigating between categories and locations, moving
things around, adding and deleting categories, viewing holdings
hierarchically, the import feeling unlinked from everything else, and (by
omission) verifying.

They have one cause. **The interface is organised around what the database
stores — a tab per entity — and no task in this system lives inside one
entity.** A holding is an item-of-a-kind, in a place, in some amount. The V1
interface shows the kind, or the place, or the amount, on separate screens, and
loses your cursor between them. So every real task becomes a tour of the tabs
with the subject off-screen for most of it.

The evidence is in the code's own comments, which are honest about it:

| Where | What it says |
|---|---|
| `internal/tui/views.go` | five `viewSpec`s, one per table |
| `internal/tui/model.go` | folds survive a view switch, "unlike the cursor, which does not" |
| `internal/tui/carry.go` | moving is "picking a thing up in one view and putting it down in another" |
| `internal/tui/carry.go` | …and moving a *place* is not that gesture: "a place is moved with `M-x reparent location`" |
| `internal/tui/model.go` | "importing swaps the whole screen for the plan review" |
| `internal/app/controller.go` | a tree showing contents holds two kinds of row, and "a row whose kind is assumed rather than known is a silent write" |

And §4.5 of the domain model — walk a location, enter observed quantities, emit
`Counted` and an explicit `Adjusted` — specifies a flow that has **no interface
at all**. `hms verify` is a command-line integrity check; the Integrity tab is
prose; counting is `#`, one row at a time, in a flat table sorted by something
other than where you are standing.

## 1. What is kept

This is a chassis swap, and it is only worth doing because the engine is good.

- **`internal/command`.** One vocabulary serving the `:` line, a CSV header and a
  JSON Lines key set. This is what makes an agent-written import possible at
  all. Not one field changes.
- **Plan → Describe → Apply.** Nothing writes without a readable preview. The new
  drawer is a better *renderer* for this cycle, not a replacement for it.
- **Refusals that name the fix.** *"only 100 g of 'Ancho Chile' here, and 5000 g
  was asked for — it does not come in packages to open"* keeps its wording and
  gains a better home: attached to the row it is about.
- **The widgets.** `tree`, `table`, `line`, `complete`, `editor` are assembled
  wrongly, not built wrongly.
- **The import folder and the handoff.** Three directories with three authors, no
  default planner, and a degradation to "you write the plan file" is the right
  design and the right privacy posture. Only the entry and the exit move.
- **The nudge discipline** of §1.4. The attention line is one line, specific, and
  dismissible.
- **The emacs motion and line-editing layer.** `C-n` `C-p` `C-f` `C-b`, readline
  in every field, `esc` and `C-g` both leaving exactly one mode.

## 2. Three moves

### 2.1 One tree, two lenses

Categories and Locations stop being tabs and become **lenses on one rail** — one
widget, one keymap, one fold model. `\` flips between `BY PLACE` and `BY KIND`
and **keeps what you are looking at**: standing on an ancho chile in the garage,
flip, and you are standing on `Spices › Dried Peppers`.

Beside the rail is the **contents pane**, which always shows what is actually in
the selected node, rolled up over the subtree, with a here-only toggle. The flat
Holdings tab stops existing — it was the contents pane with the root selected
and the rail thrown away.

Below both: an **inspector line** saying what is under the cursor in words, with
both framings of the numbers the model insists on (§4.1: *"800 g open · 3 sealed
· 6.8 kg total"*), and a **verb bar** showing the four or five verbs that mean
something right now rather than a fixed three-item reminder.

### 2.2 One review drawer

An import, a batch of structural edits, and a reconciliation from a count are
all *a set of proposed commands shown against the present state*. There is one
mechanism for that already (`PlanAll` → `Describe` → `ApplyPlan`) and three
interfaces to it.

Collapse them into a drawer that opens **below the house rather than instead of
it**, so that:

- each row carries its own before → after (`3 bags + 800 g → 3 bags + 900 g`);
- moving the cursor through the rows **moves the rail and the contents pane
  behind them**, which is the "resolving against the present state" the
  full-screen takeover makes structurally impossible;
- ambiguity settles in place, one key: *did you mean Cumin?* `[y]` `[n] new`;
- staged imports **display** as one list with dividers while still **applying** in
  passes, because a file's consequences are one thing to understand; and
- `a` applies the ready rows and leaves the rest, instead of refusing the whole
  file because one row said "lots".

**Everything transient is a drawer.** The act palette, the move picker, the
organise diff, the import plan, the confirmation. The one deliberate exception is
the walk (§2.4), and it is an exception because when you are counting a shelf you
are not browsing a house.

### 2.3 Verbs you can see

Thirty-five commands behind six keymap contexts is a vocabulary, not an
interface. `space` opens the actions **legal for the thing under the cursor**,
each annotated with the state it would change:

```
ACT ON  Ancho Chile · holding                        type to filter · esc back
  u   use some                          80 g here
  #   count what is there               ledger says 80 g
  m   move it                           to another place
  e   set an expiry                     none set
  d   discard some                      spoiled, expired, broken
  K   retire this holding               permanent · history stays
```

Illegal actions are **absent** rather than refused after you commit to them.
This is what makes it safe to drop most of the single-letter bindings without
losing power.

### 2.4 The four tasks

**Moving.** `m` opens the destination picker as a drawer with what you are
carrying pinned at the top and the rail still on screen. Type to filter by full
path; the tree keys are unchanged because it is the same tree. It takes a
multi-selection, and it takes *places* — moving a crate into another bay is the
identical gesture as moving the chile into the crate, because it is the
identical idea. `carry.go` and the typed `reparent location` path both retire.

**Adding and deleting categories.** `O` puts the rail in **organise mode**, where
structural edits are **staged, not committed**. `n` creates a child and keeps
typing, `r` renames, `d` marks for deletion *and says in the row where the
contents will land*, `m` re-parents. The drawer accumulates the diff and one `a`
applies the lot as a single transaction. The domain model already says this
reorganisation is free and migrates nothing (§2.7, §4.4) — the interface should
let you try a shape before you own it.

**Importing.** Starts inside the app (`i`). See §2.2.

**Verifying.** `V` on any node starts a **walk** of its subtree: one holding at a
time, set large, with the ledger's claim under it and three keys — it's right,
it's this much instead, it's not here. Not-found `Unique` holdings become `Out`
with no current location rather than being deleted, as §4.5 requires. The
reconciliation lands in the review drawer showing `Counted` and `Adjusted` as
the separate rows they are, which is the point of separating them.

Checkout is unchanged: `t`, one keystroke, no questions.

## 3. Keymap delta

The motion and line layers do not move. The verb layer does.

| To do this | now | proposed |
|---|---|---|
| See a place and its contents | `2`, then `v` | — (the default screen) |
| Switch category ↔ location | `1` / `2` | `\` |
| Move a holding | `M-w` · `2` · nav · `C-y` | `m` |
| Move a place or category | `M-x reparent location` | `m` |
| Act on the thing under the cursor | recall 1 of 35 | `space` |
| Select rows | `space` / `C-@` | `x` (and `C-@`) |
| Open / drill in | `enter` (history) | `enter` |
| Reorganise a tree | `o` · `e` · `C-k` · `M-x` | `O` |
| Import | quit · `hms import` | `i` |
| Count / verify a place | — | `V` |
| Undo the last change | — | `z` |
| Use, count, custody, rename | `c` `#` `t` `e` | `u` `#` `t` `r` |

## 4. Phases

Each is shippable on its own. The headless renderer and golden frames in
`internal/tui/testing` carry the whole thing: every phase lands as a set of
frames readable in a diff, which is how V1 was built and is the reason replacing
it is tractable.

| # | Phase | Touches |
|---|---|---|
| 1 | **The shell** — rail + contents + inspector + verb bar, replacing tabs 1–4; lens flip carrying the selection; subtree rollup with a here-only toggle. No new domain work. | new `tui/shell`, `rail`, `contents`, `inspector`; delete `views.go`, `panel.go`, `browse.go`; `app: + Contents(kind, id, deep)` |
| 2 | **The drawer** — generalise `planview` into a bottom drawer that never takes the screen; per-row effects; cursor follows into the house; port the import onto it; apply-the-ready-rows. | `tui/planview` → `tui/drawer`; `tui/import.go`; `cmd/hms/import.go`; `Plan` gains per-command before/after |
| 3 | **The act palette** — `space` lists the verbs legal for the subject, annotated. | new `tui/palette`; `app: + Actions(subject) []Offer`; `command/spec.go` read, not changed |
| 4 | **Move as one gesture** — holdings to places, items to categories, subtrees to new parents; multi-selection in one transaction. | delete `tui/carry.go`; reuse `tui/complete`; commands unchanged |
| 5 | **Organise mode** — staged structural edits accumulating into the drawer, applied as one batch. | new `tui/organise`; `PlanAll` for tree ops; new archlint rule (§5) |
| 6 | **The walk** — verify and count over a subtree; `Counted` + explicit `Adjusted`; missing `Unique` → `Out`. | new `tui/walk`; `app: + Walk(locID) []HoldingRow` |
| 7 | **Attention, and undo** — the one-line banner (out too long, expiring, ledger disagrees, shallow filing with a plausible sibling), expanding to a queue; then undo. | `app: Nudges → Attention`; new `Plan.Inverse()`; no schema change |

## 5. Where this could be wrong

**Undo does not mean undo.** The ledger is append-only and that is load-bearing.
`z` can only mean "apply the inverse command", which is clean for `Move`,
`Consume`, `Receive`, `Rehome` and the custody pair, and is **not possible** for
`Retire`, `Gone`, `Demote` or anything the model calls terminal. So undo is
offered *per action* and absent where it cannot be honoured. An undo that
silently no-ops on the scariest operation is worse than no undo, so the toast
says `z undo` only when it is true.

**Two panes need width.** V1 degrades to 60 columns by dropping tab labels; a
rail plus a contents pane cannot. Below roughly 76 columns the rail should
collapse to a breadcrumb with the contents pane full width, and `←` should
re-open it as an overlay. That is a fallback to design, not a resize rule.

**`space` is currently select.** Re-binding it is the change most likely to annoy
on day one. `C-@` still selects and `x` joins it. If the trade is wrong the
palette can take `.` and nothing else in the design moves.

**Staged structural edits create a second source of truth.** Organise mode holds
a proposed tree the database has not seen. That is fine while it lives strictly
in the UI and renders as a diff over freshly loaded rows, but it is the one place
here where "one owner per fact" could be quietly broken. It wants an archlint
rule of its own: nothing outside `tui/organise` may hold an unapplied tree.

**The walk assumes you brought the terminal to the shelf.** It is designed for a
laptop on the garage floor. If the real ergonomics are "phone in hand, laptop
upstairs", the walk is the wrong shape, and the right answer is that it prints a
checklist and reads back a filled one — which is, notably, an import. Worth
settling before phase 6 rather than during it.
