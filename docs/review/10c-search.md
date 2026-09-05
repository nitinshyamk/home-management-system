# 10c — Search

**Status:** provisionally accepted (2026-08-20). Golden frames captured.
**Gate:** human.

Written before the substage is built.

---

## The one mistake to avoid

`C-s` and `M-g` are two different things that feel identical for exactly one
keystroke and then diverge completely.

| Key | Scope | Purpose |
|---|---|---|
| `C-s` | **this view** | *Filter.* Narrows the rows in front of you. `M-n` / `M-p` step through what is left |
| `M-g` | **everything** | *Jump.* Fuzzy across Categories, Locations, Items, and Holdings at once; `Enter` goes there |

Conflating them is the failure mode. If a reviewer has to pause to work out
which one they are in, the substage has not passed, however well either works on
its own.

## Facets and text share one input

A token containing `:` is a facet; everything else is fuzzy text.

```
/loc:kitchen state:out rice
   └── facets ────────┘ └─ fuzzy over the row
```

---

## What to run

```bash
make reseed && ./bin/hms
```

## The script

| # | Keys | What to look at |
|---|---|---|
| 1 | `4` then `C-s` | The filter opens. **Is it obvious you are filtering rather than jumping?** |
| 2 | type `ancho` | Rows narrow as you type. Does the count say what happened? |
| 3 | `enter` | The filter stays applied and the cursor returns to the list. Is that clear? |
| 4 | `M-n` `M-n` `M-n` | Step through the matches. Does it wrap, and is the wrap obvious? |
| 5 | `esc` | The filter clears. Everything comes back. |
| 6 | `C-s` then `loc:garage` | A facet alone. Does it read as a *field* rather than as text you typed wrong? |
| 7 | `C-s` then `loc:garage ancho` | Facet and text together, one line. |
| 8 | `C-s` then `zzzz` | No matches. Does the empty state say *why* it is empty? |
| 9 | `M-g` | The jump palette. **Is it visibly a different thing from step 1?** |
| 10 | type `shelf` | Results from more than one kind at once. Is the kind of each result legible? |
| 11 | `enter` | It goes there — the right view, cursor on the right row. |
| 12 | `M-g`, `esc` | Cancelling leaves you exactly where you were. |

---

## What "good" means here

1. **Filtering and jumping look different at a glance.** Step 1 against step 9.
   Different prompt, different position, or different framing — but different
   before you have read a word.

2. **The filter says what it did.** A narrowed list that does not say it is
   narrowed is a list that lies about what you own. Step 2 and step 8 are the
   two halves: how many, and *why none*.

3. **A facet reads as a field.** At step 6, `loc:garage` should look like
   structure, not like a typo. If it renders as plain text the grammar is
   invisible and nobody will discover it.

4. **A jump result says what kind of thing it is.** At step 10, `Shelf 1` as a
   Location and `Shelf 1` as part of a Holding path must be distinguishable
   before `Enter` is pressed, or the jump lands somewhere surprising.

5. **Escape is never destructive.** Step 12. Cancelling a jump must not move
   the cursor, change the view, or clear a filter that was already applied.

6. **Density survives.** The omnibox costs a line. If it costs two, or if it
   pushes the table into scrolling when it did not before, that is worth
   knowing at 60 columns as well as 100.

---

## What is deliberately NOT being judged yet

- **`M-x` commands.** 10d. `C-s` and `M-g` only.
- **Autocomplete inside the input.** 10d, where it is needed for editing.
- **Acting on the filtered set.** 10f.


## Review — verdict

> This looks fine for now. There's some more UI shifts we'll make, but for the
> most part it looks good.

**Provisionally accepted.** Golden frames captured, deliberately, even though
more shifts are expected: 10d edits these same screens, and the point of a
golden here is to catch the changes nobody intended. A deliberate shift means
re-capturing, which is one command and a legible diff.

The narrower question 10d actually depended on — whether the prompt vocabulary,
the escape semantics, and the vertical line budget were settled enough to build
on — was answered yes.

## Sign-off

- [x] Reviewed by:Nitin
- [x] Verdict: accept
- [x] Notes:
