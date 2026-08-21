# 10e — Creation and confirmation

**Status:** not built
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
| 3 | type `Turmeric`, `tab` | Onto `counting`. Does focus move somewhere you expected? |
| 4 | `l` / `h` or arrows | Choose between the three presets. Is the choice legible without reading all three? |
| 5 | pick *something I measure*, `tab` | The unit and package fields appear. Do they appear *because* of the choice? |
| 6 | type `g`, `tab`, `2000`, `tab` | Onto `category`. |
| 7 | type `spic` | Autocomplete from the resolve index. Does it suggest without applying? |
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

3. **`esc` leaves exactly one mode.** Steps 9 and 10. The confirmation is inside
   the panel, which is inside the list. Two escapes, one at a time — and the
   first must not discard what was typed.

4. **The measured fields appear because of the choice.** Step 5. A `unit` box
   that is always visible and sometimes meaningless is the nullable-column
   muddle this project spent months eliminating, drawn on a screen.

5. **The confirmation is the same one the `:` line produces.** Step 8. Two
   confirmations that differ would mean two ideas of what is permanent.

6. **A second `o` starts clean.** Step 11. A panel that remembers an abandoned
   attempt will eventually create it.

7. **Fewer fields for simpler things.** Steps 12–13. A Location has a name, a
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
