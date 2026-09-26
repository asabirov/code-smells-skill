# code-smells

This skill keeps each repository’s code size, complexity, duplication, and imports within committed limits. It measures Go and TypeScript code. The design choice is to keep one shared scanner in the skill and store only each project’s `.smells.json` plus a Makefile target, so every repository measures the same way and none has to install the tools. The alternative, copying the scanner into every repository, was turned down because the copies would drift apart.

Code that is already over a limit may become smaller, but it may not grow. New code must stay within the configured limits. If a capped item is removed or drops to or below its limit, a new item may take its cap when it is no larger. The match uses size only, so a function may move, be renamed, shrink, or move to another package in the same change. A split may also use the caps of several removed or fixed items. An unrelated new function may take a freed cap too, but the count and total excess for each limit must still not grow, and `review` names every item that takes a cap. Matching by a content hash was rejected because moving and editing code broke the match and required an exception.

## Install and use

Install this folder as the `code-smells` skill. `machine-environment` installs it from `asabirov/claude-2`. You can also clone it anywhere.

From inside a repository, run the scanner as follows:

```bash
path/to/code-smells/bin/smells baseline               # once: writes .smells.candidate.json
mv .smells.candidate.json .smells.json                # after reading it; then commit it
path/to/code-smells/bin/smells check                  # fails if anything grew
```

The first run builds the scanner in `~/.cache/code-smells/<build>/`. Here, `<build>` is a hash of the scanner’s source. The first run requires Go 1.26 and Bun. It also needs network access if the required Go modules or TypeScript package are not already cached. Later runs work offline. Bun measures TypeScript on every run. It starts in the skill’s own folder, so a project’s `bunfig.toml` or `.env` is never loaded and cannot run code during a review.

The check compares a branch’s `.smells.json` with the one on the repository’s default branch, which git records as `origin/HEAD`. If `origin/HEAD` is not set, the check fails and says to run `git remote set-head origin --auto`.

## When a new skill version measures differently

`.smells.json` records the measuring tools in `tools`, including a hash of the measuring code. When a new version of this skill measures differently, `check` fails in every repository until that repository re-measures its baseline:

```bash
path/to/code-smells/bin/smells baseline               # writes a re-measured .smells.candidate.json
mv .smells.candidate.json .smells.json                # then commit it on its own branch
```

`baseline` prints each cap that changed with its old and new value, and the candidate records the skill build and Go version in `measured_by`. The check accepts a baseline measured with other tools than the default branch’s only when every measured file and `go.mod` match the default branch and the caps are exactly what `baseline` measures, so growth cannot hide in a re-measure. `review` shows the old and new numbers. The Go version is recorded but not compared: gocognit and dupl are pinned, and a new Go rarely parses existing code differently.

For the complete command list and the rules enforced by `check`, see [SKILL.md](SKILL.md).

A project’s Makefile locates the installed skill like this:

```make
# The code-smells skill (asabirov/code-smells-skill) checks .smells.json.
SMELLS_SKILL ?= $(firstword $(wildcard \
  $(HOME)/.local/share/machine-environment/current/source/skills/code-smells \
  $(HOME)/.agents/skills/code-smells $(HOME)/.claude/skills/code-smells))
smell-check smell-report smell-baseline:
	@test -x "$(SMELLS_SKILL)/bin/smells" || { echo "The code-smells skill is not installed on this machine, so the smell check cannot run. Install it with machine-environment apply, or set SMELLS_SKILL to a checkout of asabirov/code-smells-skill." >&2; exit 1; }
	"$(SMELLS_SKILL)/bin/smells" $(subst smell-,,$@)
```

## What is measured

Each measurement is an item with a whole-number value. It belongs to either the `production` or `tests` scope. Files that match `files.tests` are treated as tests.

- `function_lines`: the number of lines of a function that hold code. Comment lines and blank lines do not count; lines of nested functions do. In Go, it measures declared functions and methods, named `path:Receiver.Name`. In TypeScript, it measures every function with a body and names it using the surrounding functions, such as `mount/render`. A callback is named after the call it is passed to, followed by that call’s first string argument, such as `test("sends")`. `#2`, `#3`, and later numbers distinguish functions with the same name.
- `complexity`: for Go, this is gocognit’s cognitive complexity, including closures. For TypeScript, the value starts at 1 and adds 1 for each `if`, loop, `catch`, `case`, `?:`, `&&`, `||`, and `??` in the function itself. Functions nested inside it are not counted.
- `file_lines`: the number of lines in a source file.
- `fan_out` (production only): the number of local packages (Go folders) or local modules (TypeScript files) imported by a package or module.
- `duplicate_lines`: the number of lines in a file that belong to a repeated stretch of at least 100 tokens. Go uses dupl’s syntax clones. TypeScript looks for identical runs of 100 tokens while ignoring spaces and comments. Every file is compared with every other file.
- `duplicate_groups`: each group of repeated stretches is one item. Its name identifies the files it spans and includes a hash of the repeated code. Its value is the number of copies. A group belongs to `production` if any copy belongs to `production`.
- `cycles` (production only): each import cycle is one item, named after its members.

Run `make check` in this repository to test the skill itself.
