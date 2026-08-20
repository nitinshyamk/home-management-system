# 10b — Trees

**Status:** not built
**Gate:** human.

Written before the substage is built.

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
