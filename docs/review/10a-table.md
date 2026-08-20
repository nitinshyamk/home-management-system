# 10a — The table widget

**Status:** awaiting review
**Gate:** human. Tests decide whether it is correct; a person decides whether it
is *readable* and *fast*, and no assertion reaches either.

Written before the substage was built. A criterion invented after seeing the
result is not a criterion — it is a description.

---

## Why this one is reviewed first, and carefully

The table's density decisions are inherited by everything after it: the trees
(10b), every view, and the import plan screen (11b), which reuses this widget
unchanged. If 11b needs it reworked, the two flows have started to diverge, and
that is the signal to fix the shared piece rather than fork it.

So the question here is not "does it work". It is **"do I want to look at
thirty of these rows"**.

---

## What to run

```bash
make seed && ./bin/hms
```

The sample house is pinned to contain the cases that break layouts, and they are
the whole reason to look:

| Case | Where it shows |
|---|---|
| A 64-character item name | `Thunderbolt 4 to Dual DisplayPort 1.4 Adapter (Space Grey, 0.8m)` |
| A five-deep location path | `Garage > Metal Shelving Unit > Bay 3 > Blue Crate > Small Parts Tray` |
| Both on one row | the adapter is stowed in the Small Parts Tray |
| One item in three places | Ancho Chile |
| Something checked out | the USB-C cable |
| An empty classification | Electronics |

---

## The script

| # | Keys | What to look at |
|---|---|---|
| 1 | `4` | The Holdings table. Read it for five seconds and stop. **How many rows did you take in?** |
| 2 | `j` `j` `j` | Does the cursor read as a position, or as a highlight you have to hunt for? |
| 3 | `G` then `gg` | Does the jump land somewhere you can orient from immediately? |
| 4 | `l` `l` | Column focus. Is it obvious which column is active, without it shouting? |
| 5 | `s` | Sort by the focused column. Does the header say which way it is sorted? |
| 6 | `space` `j` `space` `j` `space` | Three rows selected. Can you tell at a glance which three, from anywhere on the screen? |
| 7 | `esc` | Selection clears. Nothing else changes. |
| 8 | `3` | The Items table. Same shape, different columns — does it feel like the same widget? |
| 9 | Resize to ~60 columns, then ~200 | What gets dropped first, and is it the right thing? |
| 10 | `4`, navigate to the adapter | The long name and the deep path on one row. Where does the truncation fall? |

---

## What "good" means here

Judge these, in this order. The first three are the ones that matter.

1. **Density without crowding.** Twenty rows should be legible at once. If the
   answer to step 1 is "three or four", the row height or the spacing is wrong,
   and every screen after this one inherits it.

2. **The cursor is a position, not a decoration.** At step 2 the eye should
   land on it without searching. Reverse video across a whole row is loud;
   a gutter mark alone can be too quiet. This is the single most-repeated visual
   in the application.

3. **Truncation falls in the right place.** At step 10, a truncated name must
   still be *identifiable*. `Thunderbolt 4 to Dual Displ…` is useful;
   `Thunderb…` is not. If the deep path and the long name fight for the same
   space, which one yields — and is that the right call?

4. **Selection is visible from anywhere.** At step 6, three selected rows should
   be countable without moving the cursor back over them. Multi-select plus a
   command *is* a batch, so getting this wrong means applying an operation to a
   set you misread.

5. **Sort direction is stated, not implied.** A table that is sorted and does not
   say so is a table that is quietly lying about what "first" means.

6. **Narrow terminals degrade rather than break.** At step 9 the table should
   shed columns in priority order and stay readable. Wrapping is the failure
   mode to watch for: one overflowing line shifts every row below it.

---

## What is deliberately NOT being judged yet

- **Colour.** Nothing here uses it beyond dim/bold. Colour is a later pass, and
  a layout that only works in colour is a layout that does not work.
- **Editing, actions, the omnibox.** 10d and after.
- **The tree views.** 10b, and they are judged separately because folding is a
  different problem.

---

## Sign-off

A human-gated substage is not done when its tests pass — that is what makes it
*ready for review*.

- [ ] Reviewed by:
- [ ] Date:
- [ ] Verdict: accept / rework
- [ ] Notes:

**The golden frames are captured after sign-off**, not before, so the regression
alarm is anchored to a layout a person approved rather than to the first one
that compiled.
