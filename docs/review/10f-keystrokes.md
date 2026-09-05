# 10f — Keystroke actions and batches

**Status:** built; awaiting sign-off
**Gate:** human.

Written before the substage is built.

---

## The claim this substage has to make true

> Every operation is reachable **by keystroke and by command**, and the two
> produce the same `Command`.

If they diverge, the command line is a second interface rather than the same one
— and the whole argument for `M-x`, CSV, and the agent speaking one language
collapses, because the fastest path through the interface would not speak it.

## And the way they get there is deliberately different

A keystroke on a selected row **already holds an identifier**. It builds the
`Command` directly and never goes through `Bind`.

That is not an optimisation. Serialising a row to a name and fuzzy-matching it
back is lossy in the worst way: an ambiguous name could resolve to a *different*
row than the one under the cursor. `c` on the second of three Ancho Chiles must
consume from the second one, always.

What a keystroke does **not** skip is the value parsers. `c` prompts for a
quantity and that text goes through the same `ParseAmount` a CSV cell does,
because `100` and `100g` and `2bag` must mean the same thing wherever they are
written.

---

## What to run

```bash
make reseed && ./bin/hms
```

## The script

| # | Keys | What to look at |
|---|---|---|
| 1 | `4`, `C-n`, `c` | Consume. A quantity field opens **at the row**. |
| 2 | type `10`, `enter` | It happens. Does the feedback name the row you were on? |
| 3 | `c`, `esc` | Abandoned. Nothing happened. |
| 4 | `m` | Move. A destination field with autocomplete. |
| 5 | type `gar`, `tab`, `enter` | It moves. |
| 6 | navigate to the cable, `t` | Toggle custody. It is `Out` now. |
| 7 | `t` again | And back. **One key, not two** — the state decides. |
| 8 | `#`, type `5`, `enter` | Count. Does it say what it recorded *and* what it corrected? |
| 9 | `C-k` | Retire. It asks. Is the reason it gives the right one? |
| 10 | `M-w` on a row, navigate elsewhere, `C-y` | Copy and put. Same as `m`, different idiom. |
| 11 | `space` ×3, `c`, `10`, `enter` | Three rows. Does it say three, and act on three? |
| 12 | `c` on a Unique row | Refused, in a sentence. |
| 13 | From every state above, `esc` | One mode at a time, always. |

---

## What "good" means here

1. **The keystroke and the line agree.** Steps 1–2 against `:consume 10`. Same
   result, same feedback. A test asserts the `Command`s are identical; a person
   checks that using them feels like using one thing.

2. **A prompt opens at the row, like everything else.** Step 1. Third time this
   answer has been given — the editor, the creation panel, and now this — and
   it is the same answer because it is the same reason.

3. **`t` is one key.** Step 7. Checkout if at rest, return if out. Two keys
   would mean remembering which state a thing is in before you can act, which
   is what looking at the screen was supposed to be for.

4. **A count says both numbers.** Step 8. Recording what you saw and correcting
   the ledger are two events, and a count that silently overwrites destroys the
   only evidence that the two ever disagreed.

5. **`C-k` asks, and the reason is not the write path.** Step 9. My first
   version did not ask, on the grounds that retiring is a *recording* and
   recordings are reversible by a compensating event. That was wrong: `Gone` is
   the single lifecycle terminal and nothing in the fold ever clears
   `RetiredAt`. The history survives and the Holding does not, which is
   permanent in the only sense a person cares about.

   So the confirmation is driven by the EVENTS rather than by the write path —
   a plan that emits a `Gone` asks, whatever produced it. Judge whether "no way
   back / the history stays; the holding does not" is the right thing to say.

6. **A refusal is about the thing, not the keystroke.** Step 12. "You cannot
   consume a cable" beats "invalid operation".

7. **Three rows means three.** Step 11. Already true for the `M-x` line; the
   keystrokes must not have their own answer.

---

## What is deliberately NOT being judged yet

- **The plan screen.** 11b. A batch here reports what it did; reviewing it
  *before* it happens is 11b's job.
- **`E`, the full panel.** The inline field covers the fields worth editing;
  a full panel earns its place when there is something it can do that the
  line cannot.

## Sign-off

- [ ] Reviewed by:
- [ ] Date:
- [ ] Verdict: accept / rework
- [ ] Notes:
