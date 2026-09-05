# 11d — help, and folds that stay folded

**Status:** built; awaiting sign-off

---

## What this substage is for

Two things that turned out to be the same thing: the interface knowing what it
can do, and remembering what you told it.

**Help is generated, never written.** Every line of it comes from a declaration
somewhere else — the keymap in `internal/tui/keys`, the command vocabulary in
`internal/command/spec.go`. Help text written by hand is help text that is wrong
one release after it is written, and it is the one kind of documentation nobody
re-reads: the people who would notice the drift are the people who already know
the answer. `hms schema` emits the command spec rather than a description of it
for exactly this reason; this is the same idea pointed at a person.

**Folds survive a reload.** They did not, and nothing noticed until the trees
learned to show what they contain — because asking for the contents *is* a
reload. A house somebody had carefully collapsed sprang open with every holding
in it, which is the opposite of what the key was for.

---

## What to run

```bash
make reseed && ./bin/hms
```

## The script

| # | Keys | What to look at |
|---|---|---|
| 1 | `2`, `C-n`, `TAB` | Shut the Garage. Its own row stays; its five levels go. |
| 2 | `C-n`, `C-f`, `TAB` | Shut the Spice Cabinet too, inside an open Kitchen. |
| 3 | `v` | Contents. **Neither shut node springs open**, and everything else shows what it holds. |
| 4 | `1`, then `2` | The Categories tree folds independently, and the Locations folds are still there when you come back. |
| 5 | `M-x help` | Every key, by what you are trying to do, then every command. **Is the shape of the interface visible from one screen?** |
| 6 | `esc` | Back where you were, like History. |
| 7 | `M-x help acquire` | One command: what it does, how it is written, and what each field will accept. |
| 8 | `M-x help consu` | A near miss. Does it offer the name you meant? |

---

## What "good" means here

1. **`v` does not force anything open.** Step 3. A fold is a statement about the
   shape of a tree, and it is still true after a write, a refresh, or a toggle.
   The tree is rebuilt from scratch on every one of those, so this only works
   because the folds are kept beside the view rather than on the widget.

2. **Folds belong to their own tree.** Step 4. The two trees fold different
   things, and a fold carried across would collapse whatever happened to share a
   number. They survive a view switch, unlike the cursor — a cursor carried
   across views restores a position nobody was in, but a fold is still true when
   you come back.

3. **The help names only keys that exist.** Step 5. The keys are read from the
   keymap, so a rebinding moves them here too. `TestTheGeneralHelpNamesOnlyRealKeys`
   is what says so, and it is the test that would catch a section left behind.

4. **Every command is listed, because the list is walked.** Step 5. Adding a
   command adds a line here without anybody remembering to.

5. **A command's help is its own declaration.** Step 7. The fields, their order,
   which are required, what each accepts — all read from the spec that `Bind`
   reads, so the help cannot describe a command the parser does not accept.

6. **Help is not a Command.** It acts on the interface rather than on the house,
   so it is answered before `Bind` sees it. Sending it through the write path
   would need a Command that writes nothing, and a write path with a no-op in it
   is a write path somebody will one day give something to do.

7. **A near miss is answered.** Step 8. Through the same ranked matcher the
   completion dropdowns use, so "did you mean" here and "did you mean" on the
   import plan screen are one idea rather than two that resemble each other.

---

## What is deliberately NOT here

- **Completion on the `M-x` line.** `help` would be the obvious first thing to
  complete, and the command line has no completion at all yet. That is the next
  substage, and it will use the dropdown 11c built rather than a second one.

- **A key for help.** It is a command, not a keystroke. Binding a letter to it
  would spend one of the few left on the thing you need least often once you
  have read it.
