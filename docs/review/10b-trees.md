# 10b — Trees

**Status:** built; rollup layout decided; awaiting sign-off on the rest
**Gate:** human.

Written before the substage was built.

---

## Carried in from the 10a review

One piece of feedback landed on the trees rather than the table, and is recorded
here so it is not lost between substages:

> The counted rollup on the right should be **right-aligned, not staggered by
> the depth of the tree** — or possibly *inversely* staggered.

The current behaviour is neither. `10a-frames.md` shows it: the count is placed
a fixed distance after the name, so it steps rightward as the tree deepens.

```
  Garage                         3 holdings
    Metal Shelving Unit            2 holdings
      Bay 3                          2 holdings
        Blue Crate                     2 holdings
          Small Parts Tray               2 holdings
```

Two candidate layouts to compare at review, since the feedback allowed for both:

**A — right-aligned.** Counts form a straight column against the right edge.
Comparing magnitudes down the tree becomes a single vertical scan, which is what
a rollup is for.

```
  Garage                                    3
    Metal Shelving Unit                     2
      Bay 3                                 2
        Blue Crate                          2
          Small Parts Tray                  2
```

**B — inversely staggered.** The count steps *left* as the tree deepens, so the
indentation of the name and the position of the count together say the depth,
and the counts still nearly line up.

```
  Garage                                    3
    Metal Shelving Unit                   2
      Bay 3                             2
        Blue Crate                    2
          Small Parts Tray          2
```

A is the conventional choice and the easier scan. B encodes depth twice, which
may read as reinforcing or as noisy. **Both are to be rendered and looked at
before one is picked** — this is exactly the kind of judgement no assertion
reaches.

### Decided: A, right-aligned

> No, let's do right-aligned. The different colors address the underlying need.

Which is the reason worth recording, because it is not the reason the question
was originally asked. B existed to give the eye something to track along a row
and a way to feel the depth. **The banding added in 10a already does the first,
and the indentation already does the second** — so B would have been repeating an
answer at the cost of the column.

`RollupStaggered` and the `--rollup` flag are gone rather than left behind. A
rejected alternative kept "in case" is the drift this project spends its effort
avoiding, and the decision is recorded here where it can be re-opened on
purpose.

---

## The rest of the substage

Category and Location views with folds — `za`, `zR`, `zM` — and `h`/`l` to
ascend and descend.

*Exit:* both trees traverse and fold; hierarchy is visible where the table
layout cannot show it.

## The script

To be written before building, alongside the criteria.

## Sign-off

- [ ] Reviewed by:
- [ ] Date:
- [ ] Verdict: accept / rework
- [ ] Notes:
