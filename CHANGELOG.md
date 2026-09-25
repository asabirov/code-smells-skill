# Changelog

## Unreleased

- The scanner and ratchet from asabirov/amberstone#333 move into this skill, with the
  same numbers on Amberstone: Go through gocognit v1.2.0 and dupl v1.0.0 compiled in,
  TypeScript through its own pinned compiler (5.9.3).
- `bin/smells` builds the scanner into `~/.cache/code-smells/` on first use; later runs
  are offline.
- `.smells.json` names the files to measure (`files.include`) and the project's
  `languages`; a listed language that measures nothing fails the check.
- The check fails, naming both hashes, when the baseline was measured by different
  measuring code.
- `review --base <ref>` shows what a branch changed without failing, and
  `report <dir>` surveys a repository that has no baseline.
