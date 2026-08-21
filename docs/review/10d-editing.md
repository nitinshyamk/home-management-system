# 10d — Inline editing and the command line

**Status:** not built
**Gate:** human.

Written before the substage is built.

---

## Why this one is different

**This is the first place the interface writes anything.** Everything before it
was a way of looking; from here a keystroke can change the house.

That changes what the review is for. 10a–10c were judged on whether they read
well. This one is judged on whether it is possible to change something *without
meaning to* — and on whether, having changed it, you can tell what happened.

## The old UI's defect, which this substage is most at risk of

> Editing *replaced* browsing. Modes nested. `esc` did not reliably get you out.

Ten app states, four entities, 5,300 lines. The specific failure was
`StatePickingCategory` reachable from inside item editing, so `esc` sometimes
left one mode and sometimes two, and which was which depended on how you got
there.

The rule that replaces it: **`esc` always leaves exactly one mode, never two.**
There are already three escapes before this substage adds any:

| Where | What `esc` does |
|---|---|
| the omnibox line | cancel, leaving any applied filter alone |
| the list | clear an applied filter |
| the table | clear a selection |

10d adds the field editor and the confirmation. Five is not too many *if the
order is obvious*; it is far too many if it is not.

---

## What to run

```bash
make reseed && ./bin/hms
```

## The script

| # | Keys | What to look at |
|---|---|---|
| 1 | `2`, `j`, `e` | Rename a Location in place. **Does the list move?** It must not. |
| 2 | type ` Two`, `enter` | The name changes. Is it obvious it was saved rather than abandoned? |
| 3 | `e`, edit, `esc` | Abandoned. The old name is back and nothing else changed. |
| 4 | `4`, `:` | The command line. Is it a third thing, or does it read as the filter? |
| 5 | type `consume 100g` | Acting on the selected row without naming it. Does it say what it will do? |
| 6 | `enter` | It runs. **Does the feedback say what actually happened**, in the terms of the receipt? |
| 7 | `:` `consume 1kg` on an item with less than that | A refusal. Is it a sentence, or a stack trace? |
| 8 | `:new item Turmeric counting measured unit g category Spices` | Creation. The permanent-fields confirmation appears. |
| 8a | `:new item Cardamom counting measured unit g` | Now leave the category off. Does it name the missing *field*, or report a constraint? |
| 9 | read it | Does it say what cannot be changed later, in words worth reading? |
| 10 | `esc` | Nothing was created. |
| 11 | `space` `space` `space`, `:consume 10` | Three rows selected. Does it act on **three**, and say so? |
| 12 | From every state above, mash `esc` | You reach the plain list, one mode at a time, always. |

---

## What "good" means here

1. **`esc` leaves exactly one mode.** Step 12 is the whole test. If mashing
   `esc` from a confirmation inside a command line inside a filtered view ever
   skips a level or gets stuck, this substage fails whatever else works.

2. **Editing does not move the list.** Step 1. The row being edited stays where
   it is, and the rows around it stay where they are. Losing your place is the
   thing that made the old UI unusable for its actual job.

3. **Nothing permanent happens without a sentence you could disagree with.**
   Step 9. "kind = Bulk, unit = g" is a fact; "changing these later replaces
   every holding" is the reason to care. The confirmation is proportional to
   permanence, and creation is the one error this system cannot undo.

4. **Feedback is in the terms of the receipt.** Step 6. "used 100 g of Basmati
   Rice — opened a bag" tells you what happened. "Split, Opened, Consumed" is
   the ledger talking to itself.

5. **A refusal is a sentence.** Step 7. The operations layer already produces
   these; the question is whether the interface passes them through or buries
   them.

6. **A batch says how many, and acts on that many.** Step 11. Acting on the
   cursor row while three are selected is the worst available outcome: it does
   something, it does not do what was asked, and it says nothing about the
   difference.

7. **A refusal is red, and wraps.** Steps 7 and 8a. Everything else here is
   deliberately quiet, which is what makes one loud thing readable — and
   truncating an error is the worst thing to truncate, because the part that
   says what to do about it is at the end.

---

## What is deliberately NOT being judged yet

- **The plan screen.** 11b. A `:` command acts now; batches get their review
  screen later.
- **Keystroke actions** (`c`, `m`, `t`, `#`). 10f.
- **Creation panel layout.** 10e — here the confirmation is only what `:new`
  produces.

## Sign-off

- [ ] Reviewed by:
- [ ] Date:
- [ ] Verdict: accept / rework
- [ ] Notes:
