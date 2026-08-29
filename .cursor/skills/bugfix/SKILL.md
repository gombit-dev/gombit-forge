---
name: bugfix
description: Reproduces, tests, and fixes a Gombit Forge defect with a minimal, verified change. Use when the user asks to fix a bug, fix a failing test, chase a regression, address review findings, or resolve an issue that is a defect rather than a new capability.
---

# Bugfix

Reproduce first, then fix the root cause only. Read `AGENTS.md` first.

## When not to use

- New capability or issue implementation → `feature`
- Judging whether a change should merge → `review`

## Reproduce before fixing

A reported defect is a claim until you have seen it. This applies with full
force to automated review findings and to bug reports written confidently.

1. Write the failing test, or run the exact reproduction.
2. Watch it fail against the current code, and read the actual failure — not
   the one you expected.
3. Only then change source.

If it does not reproduce, say so and stop. Report what you tried rather than
fixing something adjacent that happens to look wrong.

When a report bundles several findings, verify each one independently. Some
will be real and some will not, and they need different responses.

## Diagnose

Find the invariant that broke, not just the line that failed. Ask what allowed
the state to exist:

- Does a comment promise a contract the code never implemented? Then the fix is
  the behavior or the comment — decide which, don't leave both.
- Does validation bless a state the type can represent? Prefer making it
  unrepresentable.
- Does an error path continue, leaving malformed state visible to a later pass?
- Does a skip-continue turn a guaranteed invariant into a fail-open one?

Fix the root cause. If the same class of bug can recur elsewhere, say so; fix
it in this change only if it is genuinely the same defect.

## Verify the fix

The test must fail before and pass after — confirm both directions:

1. With the test in place, restore the pre-fix source (`git stash`, or
   `git show HEAD:<file> > <file>`).
2. Run it and watch it fail or panic.
3. Restore your fix and watch it pass.

A test that passes against the old code proves nothing, no matter how carefully
it is written.

Beware a panicking test: it aborts the whole binary, so every later subtest
never runs and appears to pass. Run subtests individually when a panic is
involved, or the coverage you think you added may not exist.

## Finishing

Keep the diff minimal — no drive-by refactors, no reformatting untouched code.

Run `make all` and `make race`. Report the root cause, the fix, and how you
verified it. Say plainly if part of the report did not reproduce or was wrong.
Don't commit, push, or open a PR unless asked.
