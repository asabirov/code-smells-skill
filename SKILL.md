---
name: code-smells
description: Measure code size, complexity, duplication and imports in a Go or TypeScript repository against its committed .smells.json. Use when running or fixing a smell check, lowering a baseline after a refactor, reviewing a pull request's smells, or surveying an unfamiliar repository. Not for style linting, formatting or finding bugs.
---

# Code smells

`bin/smells` in this skill's folder measures the repository containing your current directory. Run it from anywhere inside that repository. The first run builds the scanner in `~/.cache/code-smells/`; it needs Go, Bun and network access once. Later runs work offline.

## Commands

| Run | When | Result |
|---|---|---|
| `bin/smells check` | Before you finish any change in a repository with `.smells.json`. Projects usually run it as `make smell-check` inside `make check`. | Exit 0 when nothing grew. Otherwise each `smell:` line names one problem and it exits 1. |
| `bin/smells report [dir]` | To see the repository's current totals and ten worst functions. Works without `.smells.json`, using the default limits. | A table per language, then the worst functions. Never fails on what it finds. |
| `bin/smells baseline` | After a change made something smaller, when `check` printed `note:` lines saying a cap can come down. | Writes `.smells.candidate.json`. Read it, `mv` it over `.smells.json`, and commit it with the change. |
| `bin/smells review --base origin/main` | When reviewing a branch or pull request. | Whether the check passes, how the baseline moved and the reasons given, new hotspots, and the worst functions. Exits 0 unless the code cannot be measured. |
| `bin/smells init` | Once, to give a repository its first `.smells.json`. | Writes `.smells.json` with a cap for everything already over a limit. Commit it. |

## The rules the check enforces

- A function, file or package already over a limit has a cap in `.smells.json`. It may become smaller, but must not grow beyond that cap.
- New code must stay within these limits: 80 lines per function, 500 per file, complexity 15, 8 local imports per package, no duplicated stretch of 100 tokens and no import cycle.
- The number of items over each limit and their total excess must not increase. Improving one hotspot does not allow a new problem elsewhere.
- A branch's `.smells.json` may not be looser than the one on `origin/main`, unless `exceptions` lists the loosened key with a `reason` and an `issue`.
- The scan fails if it finds a file it cannot parse, or if a listed language has no files or no functions.
- The baseline records a `definitions` hash for the measuring code. If this skill measures differently, the check fails and reports both hashes.

## Fixing a failed check

Split or simplify the code that grew; do not raise a cap. If the growth is truly needed, raise the cap by hand and add an exception with the reason and the issue. A reviewer must then decide whether to accept it. If the `definitions` hash fails, this machine's skill differs from the one that created the baseline. Use the same skill version, or re-measure the baseline in its own reviewed change.

## `.smells.json`

A project records `languages` (`go`, `typescript`) and `files` (`include` as git pathspecs such as `*.go` or `ui/*.ts`, `exclude` as path prefixes, `tests` as regular expressions). `init` fills in the remaining fields. `budgets` contain the limits, `caps` contain existing hotspots, `aggregate` contains totals over each limit, and `tools` contains the measuring versions.

## In a review

The numbers show what grew. You must decide whether an exception is justified, whether the growth is in decision code or plumbing, and whether a split separates responsibilities or only moves lines.
