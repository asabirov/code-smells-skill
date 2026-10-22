# code-smells

An agent skill that keeps a Go or TypeScript repository's code size, complexity, duplication and imports from growing past limits the repository records.

The design choice is one shared scanner in the skill. Each project keeps only a `.smells.json` file and a Makefile target, so every repository measures the same way and none installs the tools. The alternative, copying the scanner into every repository, was turned down because the copies drift apart.

Code that is already over a limit may shrink but may not grow. New code stays within the limits. If a capped item is removed or drops to its limit, a new item may take its cap when it is no larger. The match uses size only, so a function may move, be renamed, shrink, or change package in one change. Matching by a content hash was turned down because moving and editing code broke the match and forced an exception.

## What it changes

The check turns "this function got worse" into a failed command that an agent can read and fix. This example is a real run on the scanner's own Go code, in a scratch copy with a baseline written by `bin/smells baseline`. The change added two `if` statements to one function.

Before the fix, `bin/smells check` exits 1:

```text
smell: production complexity: baseline.go:guard is 19, over the limit of 15
smell: production complexity: 1 over the limit by 4 in total; the baseline allows 0 over by 0
```

The fix moved the two checks into a new function, `sizeNotes`. `guard` returned to complexity 15, and `bin/smells check` exits 0:

```text
smell check passed (code-smells c563d3084f27, build bb70b8a8433bd6d5): nothing grew past its limit in .smells.json
```

The skill author uses it in one private repository, where the check runs before every merge. In 30 days, 56 commits changed that repository's baseline, on 16 different days.

## How few tokens it uses

An agent loads the name and description when a session starts. It loads `SKILL.md` only when a task needs it. The command outputs below are from the run above. The count is characters divided by 4, rounded up, because no tokenizer was available. Real counts differ somewhat.

| Item | Characters | Tokens (about) |
| --- | ---: | ---: |
| Name and description, always loaded | 434 | 109 |
| `SKILL.md`, loaded when used | 5,695 | 1,424 |
| `bin/smells check`, passing | 115 | 29 |
| `bin/smells check`, failing with two problems | 302 | 76 |
| `bin/smells report`, ten worst functions | 1,077 | 270 |
| `bin/smells review`, passing | 747 | 187 |
| `bin/smells review`, failing | 993 | 249 |

A passing check costs 29 tokens. A failing one costs 76 tokens and names the function to fix.

## Install

```sh
DO_NOT_TRACK=1 npx skills add asabirov/code-smells-skill
```

`DO_NOT_TRACK=1` turns off the telemetry of the [skills CLI](https://github.com/vercel-labs/skills). The command needs Node.js 22.20 or newer, npm and Git. To update, run the command again. The version is `metadata.version` in the `SKILL.md` frontmatter.

Then ask your agent, for example: "Add a smell baseline to this repository and run the check."

### Dependencies

| Dependency | Needed for |
| --- | --- |
| Node.js 22.20 or newer, npm and Git | Installing the skill; Git also lets the check compare with the default branch |
| Go 1.26 or newer | Building the scanner on the first run; an older Go downloads the 1.26.5 toolchain |
| Bun | Measuring TypeScript, and running this repository's tests |
| Network access, once | Fetching the Go modules and the TypeScript compiler on the first build |

The scanner compiles in these Go modules: gocognit v1.2.0 for Go complexity, dupl v1.0.0 for Go duplication, and golang.org/x/tools v0.23.0. It uses the TypeScript compiler 5.9.3 for TypeScript.

## Use

From inside a repository, run `bin/smells` from the installed skill folder:

```sh
bin/smells baseline                        # once: writes .smells.candidate.json
mv .smells.candidate.json .smells.json     # after reading it; then commit it
bin/smells check                           # fails if anything grew
```

The first run builds the scanner in `~/.cache/code-smells/<build>/`, where `<build>` is a hash of the scanner's source. Later runs work offline. Bun starts in the skill's folder, so a project's `bunfig.toml` or `.env` never loads and cannot run code during a review.

The check compares a branch's `.smells.json` with the one on the default branch, which git records as `origin/HEAD`. If `origin/HEAD` is not set, the check fails and says to run `git remote set-head origin --auto`.

`SKILL.md` lists every command and rule. A project can run the check from its Makefile:

```make
SMELLS_SKILL ?= $(firstword $(wildcard $(CURDIR)/.agents/skills/code-smells $(CURDIR)/.claude/skills/code-smells $(HOME)/.agents/skills/code-smells $(HOME)/.claude/skills/code-smells))
smell-check smell-report smell-baseline:
	@test -x "$(SMELLS_SKILL)/bin/smells" || { echo "The code-smells skill is not installed, so the smell check cannot run. Install it with npx skills add asabirov/code-smells-skill, or set SMELLS_SKILL to its folder." >&2; exit 1; }
	"$(SMELLS_SKILL)/bin/smells" $(subst smell-,,$@)
```

### When a new skill version measures differently

`check` fails until the repository re-measures. `SKILL.md` holds the steps and the rule.

### Known limits

- The build cache is never pruned. Each skill version adds about 29 MB.
- The scanner reads symlinked source files, and `baseline` writes through a symlinked `.smells.candidate.json`. Run it only on repositories you trust.
- A broken cached build does not rebuild itself. Delete `~/.cache/code-smells/` to force it.

## Test it

```sh
make check
```

This runs the scanner's tests and a smoke test of `bin/smells` on a copy of a fixture project. It needs Go and Bun. See [AGENTS.md](AGENTS.md) for the time budget. There is no CI workflow.

## What is measured

Each measurement is an item with a whole-number value. It belongs to either the `production` or `tests` scope. Files that match `files.tests` are treated as tests.

- `function_lines`: the number of lines of a function that hold code. Comment lines and blank lines do not count; lines of nested functions do. In Go, it measures declared functions and methods, named `path:Receiver.Name`. In TypeScript, it measures every function with a body and names it using the surrounding functions, such as `mount/render`. A callback is named after the call it is passed to, followed by that call’s first string argument, such as `test("sends")`. `#2`, `#3`, and later numbers distinguish functions with the same name.
- `complexity`: for Go, this is gocognit’s cognitive complexity, including closures. For TypeScript, the value starts at 1 and adds 1 for each `if`, loop, `catch`, `case`, `?:`, `&&`, `||`, and `??` in the function itself. Functions nested inside it are not counted.
- `file_lines`: the number of lines in a source file.
- `fan_out` (production only): the number of local packages (Go folders) or local modules (TypeScript files) imported by a package or module.
- `duplicate_lines`: the number of lines in a file that belong to a repeated stretch of at least 100 tokens. Go uses dupl’s syntax clones. TypeScript looks for identical runs of 100 tokens while ignoring spaces and comments. Every file is compared with every other file.
- `duplicate_groups`: each group of repeated stretches is one item. Its name identifies the files it spans and includes a hash of the repeated code. Its value is the number of copies. A group belongs to `production` if any copy belongs to `production`.
- `cycles` (production only): each import cycle is one item, named after its members.

## License

MIT. See [LICENSE](LICENSE).
