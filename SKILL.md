---
name: code-smells
description: Measure code size, complexity, duplication and imports in a Go or TypeScript repository against its committed .smells.json. Use when running or fixing a smell check, lowering a baseline after a refactor, reviewing a pull request's smells, or surveying an unfamiliar repository. Not for style linting, formatting or finding bugs.
---

# Code smells

`bin/smells` in this skill's folder measures the repository that contains your current directory. You can run it from anywhere inside that repository. The first run builds the scanner in `~/.cache/code-smells/`; this requires Go, Bun and network access once. Later runs work offline.

## Commands

| Run | When | Result |
|---|---|---|
| `bin/smells check` | Before you finish a change in a repository with `.smells.json`. Projects usually run it as `make smell-check` inside `make check`. | Exits 0 when no smell has grown. Otherwise, each `smell:` line identifies a problem and the command exits 1. |
| `bin/smells report [dir]` | To view the repository's current totals and the ten worst functions. It works without `.smells.json` and uses the default limits. | Shows a table for each language, followed by the worst functions. It never fails because of what it finds. |
| `bin/smells baseline` | Run once to create a repository's first `.smells.json`. Run it again after making something smaller when `check` prints `note:` lines saying that a cap can be reduced. | Writes `.smells.candidate.json`: either the first baseline, with a cap for every existing limit violation, or the current baseline with lower caps. Read it, `mv` it over `.smells.json`, and commit it. |
| `bin/smells review --base origin/main` | When reviewing a branch or pull request. | Reports whether the check passes, how the baseline changed, the stated reasons, new hotspots, and the worst functions. It exits 0 unless the code cannot be measured. |

## The rules the check enforces

- If a function, file or package already exceeds a limit, `.smells.json` records a cap for it. The code may become smaller, but it must not grow beyond that cap.
- New code must remain within these limits: 80 lines per function, 500 lines per file, complexity 15, 8 local imports per package, no duplicated stretch of 100 tokens, and no import cycle.
- The number of items above each limit, and their total excess, must not increase. Improving one hotspot does not permit a new problem elsewhere.
- A branch's `.smells.json` must not be looser than the version on `origin/main`, unless `exceptions` lists the loosened key with a `reason` and an `issue`.
- The scan fails if it finds a file it cannot parse, or if a listed language has no files or no functions.
- The baseline records the measuring tools in `tools`, including a `definitions` hash of the measuring code. If this skill measures code differently, the check fails and names both sets of tools.

## Fixing a failed check

If code has grown, split or simplify it; do not raise its cap. If the growth is genuinely required, raise the cap by hand and add an exception with the reason and the issue. A reviewer must then decide whether to accept it. If the `definitions` hash fails, this machine's skill differs from the skill that created the baseline. Use the same skill version, or re-measure the baseline in a separate reviewed change.

## `.smells.json`

A project records its `languages` (`go`, `typescript`) and `files`: `include` contains git pathspecs such as `*.go` or `ui/*.ts`, `exclude` contains path prefixes, and `tests` contains regular expressions. The first `baseline` run fills in the remaining fields:

- `budgets` contain the limits.
- `caps` contain existing hotspots.
- `aggregate` contains totals for each limit.
- `tools` contains the measuring versions.

## In a review

Use the numbers to see what grew. Decide whether an exception is justified, whether the growth is in decision code or plumbing, and whether a split creates separate responsibilities or only moves lines from one place to another.
