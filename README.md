# code-smells

An agent skill that holds a repository's code size, complexity, duplication and
imports to a committed `.smells.json`. Code already over a limit may shrink but not
grow, and new code must stay within the limits. It measures Go and TypeScript.

The scanner lives in the skill, not in each project. A project keeps only its
`.smells.json` and a Makefile target that calls the skill, so every repository
measures the same way and none has to install the tools. The alternative, a copy of
the scanner in each repository, was turned down because the copies drift apart.

## Install and use

Install the folder as the `code-smells` skill (machine-environment does this from
`asabirov/claude-2`), or clone it anywhere. Then, inside a repository:

```bash
path/to/code-smells/bin/smells init     # once: writes .smells.json
path/to/code-smells/bin/smells check    # fails if anything grew
```

The first run builds the scanner into `~/.cache/code-smells/<build>/`, where
`<build>` is a hash of the scanner's source. It needs Go 1.26 and Bun, and the
network if Go modules or the TypeScript package are not cached yet. Later runs are
offline. [SKILL.md](SKILL.md) lists every command and the rules the check enforces.

A project's Makefile finds the skill like this:

```make
# The code-smells skill (asabirov/code-smells-skill) checks .smells.json.
SMELLS_SKILL ?= $(firstword $(wildcard \
  $(HOME)/.local/share/machine-environment/current/source/skills/code-smells \
  $(HOME)/.agents/skills/code-smells $(HOME)/.claude/skills/code-smells))
smell-check smell-report smell-baseline:
	@test -x "$(SMELLS_SKILL)/bin/smells" || { echo "The code-smells skill is not installed on this machine, so the smell check cannot run. Install it with machine-environment apply, or set SMELLS_SKILL to a checkout of asabirov/code-smells-skill." >&2; exit 1; }
	"$(SMELLS_SKILL)/bin/smells" $(subst smell-,,$@)
```

Run `make check` in this repository to test the skill itself.
