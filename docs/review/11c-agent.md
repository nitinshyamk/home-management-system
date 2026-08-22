# 11c — The agent loop

**Status:** built
**Gate:** corpus — measured, not asserted.

---

## The loop

```bash
hms schema                       # the vocabulary, as instructions to paste
hms schema --format json         # the same, for a program
hms import plan.csv --dry-run    # the resolved plan as JSON, to iterate against
hms import plan.csv              # the plan screen, for a person to approve
```

**An agent never applies anything.** It produces a file; a person approves it.

## Why the schema is generated

A command whose schema says one thing and whose binder does another is the
failure this exists to prevent — and it is *unrepresentable* rather than merely
tested for, because both read the same specs. `TestTheSchemaCoversEveryOp`
guards the remaining gap: a command that exists and is not described.

## The corpus, and the number that matters

Eight files of realistic input — receipts, grocery lists, an agent's JSON Lines,
plurals, shouted names, ties, values that are not values — reported as three
numbers.

```
go test ./internal/importer -run Corpus -v
8 files: 11 ready, 5 need confirming, 6 blocked
```

**The third number is the one no hand-written test would surface.** A row that
fails is a row somebody fixes. A row that quietly attached to the wrong item is
a row nobody ever looks at again. So every row carries a written-down
expectation, and `TestCorpusNeverBindsSilentlyToTheWrongThing` checks that the
rows which came out *ready* resolved to what they were written to mean.

The expectations live in the test rather than beside the files, because a corpus
carrying its own answers is a corpus somebody edits to match the output.

## What it is not

It is **not** an agent reading the prompt and writing rows — that needs an agent,
and it is the only real test of `--format prompt`. What is here measures the
half that can be measured deterministically: given input of the shape an agent
produces, does the system land it in the right state.

Per the plan, the agent-in-the-loop corpus is run on demand rather than in
`make check` — slow and non-deterministic — and **a schema change without
re-running it is a change of unknown quality.**

## Re-run it after

- any change to the command vocabulary
- any change to `resolve`'s tiers or tuning
- any change to what `Bind` accepts

## Sign-off

- [ ] Reviewed by:
- [ ] Date:
- [ ] Verdict: accept / rework
- [ ] Notes:
