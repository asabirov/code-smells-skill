# Changelog

## Unreleased

- The scanner and ratchet from asabirov/amberstone#333 now belong to this skill and use the same Amberstone values: gocognit v1.2.0 and dupl v1.0.0 are compiled in, while TypeScript uses its pinned compiler, 5.9.3.
- On first use, `bin/smells` builds the scanner in `~/.cache/code-smells/`. Later runs work offline.
- `.smells.json` defines the files to measure (`files.include`) and the project's `languages`. The check fails if a listed language produces no measurements.
- The check fails, and reports both hashes, when the baseline was measured with different measuring code.
- Use `review --base <ref>` to see what a branch changed without failing the check. Use `report <dir>` to survey a repository that has no baseline.
- `baseline` writes the repository's first baseline as a candidate for moving into place. `.smells.json` no longer contains `schema` or `metrics`; the README defines the metrics, and the check compares the complete `tools` block, including the `definitions` hash.
