# code-smells

An agent skill that stops a Go or TypeScript repository's code size, complexity, duplication and imports from growing past limits that the repository records. It measures `.ts` and `.tsx` files, not `.d.ts` files.

**Design choice:** one shared scanner in the skill. Each project keeps only a `.smells.json` file and a Makefile target, so every repository measures the same way and none installs the tools. Turned down: a copy of the scanner in every repository, because the copies drift apart.

**Limits:** code that is already over a limit may shrink but not grow. New code stays within the limits. When a capped item is removed or drops to its limit, a new item that is no larger may take its cap. The match uses size only, so a function can move, get a new name, shrink or change package in one change. Turned down: a match by content hash, because moved and edited code broke the match and forced an exception.

## What it changes

A function that got worse becomes a failed command that an agent can read and fix. This is a real run on the scanner's own Go code, in a scratch copy with a baseline from `bin/smells baseline`. The change added two `if` statements to one function, and `bin/smells check` exits 1:

```text
smell: production complexity: files.go:detectFiles is 17, over the limit of 15
smells (code-smells 6e7467c85c59, build 24071777bd786b75): the smell check failed with 1 problem(s); the limits are in .smells.json
```

The fix moved the two checks into a new function, `defaultRules`. `detectFiles` went back to complexity 15, and the check exits 0:

```text
smell check passed (code-smells 6e7467c85c59, build 24071777bd786b75): nothing grew past its limit in .smells.json
```

The skill author uses it in one private repository, where the check runs before every merge. In 30 days, 56 commits on 16 different days changed that repository's baseline.

## Token cost

An agent loads the name and description when a session starts, and `SKILL.md` only when a task needs it. The outputs are from the run above. A count is characters divided by 4, rounded up, because no tokenizer was available, so real counts differ somewhat.

| Item | Characters | Tokens (about) |
| --- | ---: | ---: |
| Name and description, always loaded | 434 | 109 |
| `SKILL.md`, loaded when used | 4,989 | 1,248 |
| `bin/smells check`, passing | 115 | 29 |
| `bin/smells check`, failing with one problem | 211 | 53 |
| `bin/smells report`, ten worst functions | 1,077 | 270 |
| `bin/smells review`, passing | 747 | 187 |
| `bin/smells review`, failing | 993 | 249 |

## Install

```sh
DO_NOT_TRACK=1 npx skills add -g https://github.com/asabirov/code-smells-skill/tree/v0.3.0
```

- `-g` installs the skill for your user, because the scanner measures a skill inside a project as part of that project. For a project install, add the skill's folder to `files.exclude` in `.smells.json`, for example `".agents/skills/code-smells/"`.
- `DO_NOT_TRACK=1` turns off the telemetry of the [skills CLI](https://github.com/vercel-labs/skills).
- The command installs release v0.3.0, not the main branch. A new skill version can measure differently, so a fixed release keeps a passing check from failing. To update or roll back, use the tag of another [release](https://github.com/asabirov/code-smells-skill/releases). `metadata.version` in the `SKILL.md` frontmatter gives the version.
- To remove the skill, run `DO_NOT_TRACK=1 npx skills remove -g code-smells`.

Then ask your agent, for example: "Add a smell baseline to this repository and run the check."

| Dependency | Needed for |
| --- | --- |
| Node.js 22.20 or newer, npm and Git | Installing the skill. Git also lets the check compare with the default branch. |
| Go 1.26 or newer | Building the scanner on the first run. An older Go downloads the 1.26.5 toolchain. |
| Bun | Measuring TypeScript, and running this repository's tests |
| Network access, once | Fetching the Go modules and the TypeScript compiler on the first build |

The scanner compiles in gocognit v1.2.0 (Go complexity), dupl v1.0.0 (Go duplication) and golang.org/x/tools v0.23.0. It uses the TypeScript compiler 5.9.3.

## Use

From inside a repository, run `bin/smells` from the installed skill folder:

```sh
bin/smells baseline                        # once: writes .smells.candidate.json
mv .smells.candidate.json .smells.json     # after reading it; then commit it
bin/smells check                           # fails if anything grew
```

- The first run builds the scanner in `~/.cache/code-smells/<build>/`, where `<build>` is a hash of the scanner's source. Later runs work offline.
- Bun starts in the skill's folder, so a project's `bunfig.toml` or `.env` never loads and cannot run code during a review.
- The check compares the branch's `.smells.json` with the one on the default branch, which git records as `origin/HEAD`. If `origin/HEAD` is not set, the check fails and says to run `git remote set-head origin --auto`.
- When a new skill version measures differently, `check` fails until the repository re-measures. `SKILL.md` gives the steps and the rule, and lists every command.

A project can run the check from its Makefile. A `SMELLS_SKILL` value already set in the environment, for example by a provisioning script, wins over this lookup.

```make
SMELLS_SKILL ?= $(firstword $(wildcard $(CURDIR)/.agents/skills/code-smells $(CURDIR)/.claude/skills/code-smells $(HOME)/.agents/skills/code-smells $(HOME)/.claude/skills/code-smells))
smell-check smell-report smell-baseline:
	@test -x "$(SMELLS_SKILL)/bin/smells" || { echo "The code-smells skill is not installed, so the smell check cannot run. Install it with npx skills add -g https://github.com/asabirov/code-smells-skill/tree/v0.3.0, or set SMELLS_SKILL to its folder." >&2; exit 1; }
	"$(SMELLS_SKILL)/bin/smells" $(subst smell-,,$@)
```

### Known limits

- The build cache is never pruned. Each skill version adds about 29 MB.
- The scanner reads symlinked source files, and `baseline` writes through a symlinked `.smells.candidate.json`. Run it only on repositories you trust.
- A broken cached build does not rebuild itself. Delete `~/.cache/code-smells/` to force it.

## Test it

```sh
make check
```

This runs the scanner's tests and a smoke test of `bin/smells` on a copy of a fixture project. It needs Go and Bun. [AGENTS.md](AGENTS.md) gives the time budget. The `check` workflow in `.github/workflows/check.yml` runs the same command on each pull request and on each push to main.

## What is measured

[docs/metrics.md](docs/metrics.md) defines each metric: `function_lines`, `complexity`, `file_lines`, `fan_out`, `duplicate_lines`, `duplicate_groups` and `cycles`.

## License

MIT. See [LICENSE](LICENSE).
