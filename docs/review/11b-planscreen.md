# 11b — The plan screen

**Status:** built; awaiting sign-off
**Gate:** human.

Written before the substage is built.

---

## Why this is the highest-stakes screen in the system

Everywhere else, one thing happens and you watch it happen. Here **a file you
did not write proposes a batch of changes**, and the only thing standing between
it and the house is whether this screen told you the truth about what it was
going to do.

A receipt that quietly makes a second `Turmeric` is worse than one that stops
and asks. A row that silently attached to the wrong item is worse than a row
that failed.

## The three row states are not a UI invention

They are the shape of `BindResult`, which is binary:

```
ready ⟺ Command != nil
```

A row that became a `Command` needs no confirmation because there is nothing
left to decide. A row that did not carries exactly the issues explaining why.
So the states come straight from the resolver's outcomes:

| Outcome | State | Why |
|---|---|---|
| every reference `Exact` | **ready** | nothing left to decide |
| would create something | **needs confirmation** | creation is never silent |
| `Suggested` | **needs confirmation** | offered, never applied |
| `Ambiguous`, `Missing`, bad value | **blocked** | only a person can settle it |

## All-or-nothing

`A` stays unavailable until every row is ready or dropped, and then the whole
plan applies in **one transaction**. A half-applied receipt is the worst
outcome: some of it happened, you do not know which, and the file no longer
describes the house.

---

## What to run

```bash
make reseed
./bin/hms --db-path=hms.db import testdata/receipt-messy.csv
```

## The script

| # | Keys | What to look at |
|---|---|---|
| 1 | — | The plan opens. **Can you tell ready from blocked without reading?** |
| 2 | read the counts | "3 ready, 2 need confirming, 1 blocked". Does it add up to the file? |
| 3 | `A` | Refused, and it says why. |
| 4 | `j` onto the suggested row | The issue is stated in full: *did you mean Turmeric?* |
| 5 | `tab` | Accept the suggestion. The row goes ready. |
| 6 | `j` onto the row that would create | The creation panel opens — **the same one `o` opens**. |
| 7 | `enter` | The permanent-fields confirmation, the same one everywhere else. |
| 8 | `j` onto a blocked row, `e` | Edit it in place, fix it, `enter`. A field that names something completes. |
| 9 | `j` onto the last blocked row, `d` | Drop it. Does the count change? |
| 10 | `A` | Now it applies. **One transaction.** |
| 11 | check the Items view | Two rows named one new item; there is **one** Turmeric. |
| 12 | `q` from a plan with unresolved rows | Nothing was applied. |

---

## What "good" means here

1. **The three states are legible before they are read.** Step 1. If you have to
   read the issue text to know a row is blocked, the screen has failed at the
   only thing it is for.

2. **The counts are the file.** Step 2. ready + confirmable + blocked + dropped
   must equal the rows in the file, always. A row that vanished from the
   arithmetic is a row that will surprise someone.

   *(The fixture yields 3/2/1 rather than the plan's 3/1/2, because the
   walkthrough needs both a suggestion row and a creating row and there are only
   so many ways to be blocked. The shape is what matters.)*

3. **`A` is refused with a reason.** Step 3. "2 rows are blocked" beats a key
   that does nothing.

4. **Creation reuses the panel unchanged.** Steps 6–7. If it needed reworking
   for this screen, the interactive and bulk flows have started to diverge —
   and that is the signal to fix the shared piece, not to fork it.

5. **Two rows naming one new item create it once.** Step 11. This is the one
   the whole design is arranged around: a receipt cannot produce two Turmerics.

6. **All-or-nothing is visible, not just true.** Step 10. The screen should say
   it is applying one transaction, and step 12 should leave nothing behind.

7. **The table is the table.** The rows use 10a's widget with 10a's density,
   banding, and cursor. A second table would be a second product.

---

## What is deliberately NOT being judged yet

- **`hms schema` and `--dry-run`.** 11c.
- **Provenance** — whether an import records that it *was* an import. Open in
  the plan, and `E7` applies: if it is derivable from the events, it does not
  belong in them.

## Sign-off

- [ ] Reviewed by:
- [ ] Date:
- [ ] Verdict: accept / rework
- [ ] Notes:
