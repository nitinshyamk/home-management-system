# 10e — Creation and confirmation

**Status:** built; autocomplete added after review; awaiting sign-off
**Gate:** human.

Written before the substage is built.

---

## Why this panel is settled here and not later

**The import plan screen (11b) reuses it unchanged.** A receipt row that would
create a new Item opens *this* panel, with *this* confirmation. If 11b needs it
reworked, the interactive and bulk flows have started to diverge into different
products — and that is the signal to fix the shared piece rather than fork it.

So the question is not only "is this good for typing at" but **"would this still
be right with a receipt behind it"**.

## The one error this system cannot undo

An annotation error is a typo you fix. A recording error is caught by `H10` and
reversed by a compensating event. **An origination error cannot be fixed** — a
wrong `kind` or `content_unit` is remedied only by making a new Item and
archiving the old one, which is what `Promote` does and why it replaces every
Holding.

So creation confirms, and the confirmation is the only friction in the
application. Everything else stays frictionless *because* this does not.

## Three presets, never two booleans

Nobody reads the word "fungible" (domain model §2.2). The choice is:

- **one of a kind** — each one tracked individually, with its own custody
- **a pile I count** — whole things, interchangeable
- **something I measure** — grams, millilitres

`counting` decides the variant and can never be revised.

---

## What to run

```bash
make reseed && ./bin/hms
```

## The script

| # | Keys | What to look at |
|---|---|---|
| 1 | `3`, `o` | The creation panel opens. **Is it in the list, like the editor, or somewhere else?** |
| 2 | read it | Before typing anything — is it obvious what it will make and what it needs? |
| 2a | type `Cum` | It says *Spices > Cumin already exists*. A warning, not an offer: `TAB` will not take it. **Does it read as a fact rather than a refusal?** |
| 3 | clear it, type `Turmeric`, `tab` | Onto `counting`. Does focus move somewhere you expected? |
| 4 | `C-f` / `C-b` or arrows | Choose between the three presets. Is the choice legible without reading all three? |
| 5 | pick *something I measure*, `tab` | The unit and package fields appear. Do they appear *because* of the choice? **And the unit dropdown is already open** — arriving at a field shows what it accepts. |
| 6 | `C-n` `C-n` `C-n`, `tab` | Walk the seven units and take one. **Does `TAB` take what is highlighted, or what is first?** |
| 6a | `tab`, `2000`, `tab` | Onto `category`, whose dropdown opens with every category. |
| 7 | type `spi` | It narrows as you type. Does it suggest without applying? |
| 7a | `esc`, then keep typing | The list goes and does not come back. **Does `esc` read as "not this field", or as a flicker?** |
| 7b | `S-TAB` | The other way out of a list; with no list up it steps back a field. |
| 7c | `ctrl+u` on a parent field | Clears it. The parent arrives pre-filled with the cursor's node, which needs a way out that is not fifteen backspaces |
| 8 | `enter` | The permanent-fields confirmation. Same one 10d produced? |
| 9 | `esc` | Back to the panel, with what you typed still there. **Not back to the list.** |
| 10 | `esc` again | Back to the list, nothing created. |
| 11 | `o` again | Empty, not half-filled with the last attempt. |
| 12 | `2`, `o` | A Location. Same panel, fewer fields, no `counting`. |
| 13 | `1`, `o` | A Category. Same again. |

---

## What "good" means here

1. **The panel is in the list.** Step 1. Same answer as the editor in 10d: a
   creation form that replaces the view costs you the context that tells you
   whether the thing already exists.

2. **The three presets read as a choice, not a form field.** Step 4. If it looks
   like a text input with three legal values, people will type into it.

3. **`esc` leaves exactly one mode.** Steps 7a, 9 and 10. The dropdown is inside
   the field, which is inside the panel, which is inside the list. Three
   escapes, one at a time — and none of them may discard what was typed.

4. **The measured fields appear because of the choice.** Step 5. A `unit` box
   that is always visible and sometimes meaningless is the nullable-column
   muddle this project spent months eliminating, drawn on a screen.

5. **The confirmation is the same one the `M-x` line produces.** Step 8. Two
   confirmations that differ would mean two ideas of what is permanent.

6. **A second `o` starts clean.** Step 11. A panel that remembers an abandoned
   attempt will eventually create it.

7. **A suggestion is offered, never applied.** Step 7. The resolver reports;
   it does not decide. And it completes only the *kind* the field wants — a
   place completed against classifications would offer names that cannot
   possibly be right.

8. **The dropdown is the same everywhere.** Steps 5–7b. `TAB` takes, `C-n`/`C-p`
   choose, `S-TAB` and `esc` dismiss, and anything else typed refines the list.
   The same keys do the same things in the move prompt and on the import plan
   screen, because it is the same component.

9. **The best match is what `TAB` takes.** Step 6. The list is ranked — exact,
   then the name starting with what you typed, then the name containing it,
   then the letters merely appearing in order — and the highlight starts on the
   best one. It only stays where you put it once you have moved it yourself.

10. **The name warns and does not offer.** Step 2a. Two items may share a name;
    the schema permits it and tells them apart by category. So this is a fact
    put where the decision is being made, not a refusal — and deliberately not
    takeable, because completing it would make recreating what you already have
    the fastest path through the form.

8. **Fewer fields for simpler things.** Steps 12–13. A Location has a name, a
   parent, and a description. If the panel shows `counting` greyed out, it is
   one panel pretending rather than one panel adapting.

---

## What is deliberately NOT being judged yet

- **The plan screen.** 11b — where this panel appears with a receipt behind it.
- **`o` on a Holding.** Stock arrives by `acquire`; there is no panel for it.
- **Keystroke actions.** 10f.

## Sign-off

- [ ] Reviewed by:
- [ ] Date:
- [ ] Verdict: accept / rework
- [ ] Notes:
