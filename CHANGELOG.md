# Changelog

## 0.2.0 — 2026-10-24

### Added

- **MIT licence.** You may use, copy and change the skill under the MIT licence.
- **Install and proof.** The README gives a global install command, a tested example and measured token estimates.

### Changed

- **General description.** The skill description applies to Go and TypeScript repositories with or without a baseline.
- **Private names.** The skill files and test example use public terms instead of private project and host names.

### Fixed

- **Same build on every host.** macOS and Linux now compute the same build id for the same scanner source. On macOS the id changes once, so the first run builds the scanner again. Baselines do not need a re-measure.

## 0.1.1 — 2026-10-23

- SKILL.md states the skill version in `metadata.version` for release installs.

## 0.1.0 — 2026-10-23

This skill checks code size, complexity, duplication and imports against limits set by Go and TypeScript projects. Developers and coding agents use it.

- The TypeScript scanner reads `.tsx` files with the same measures as `.ts` files, so a glob such as `ui/**/*.tsx` in `files.include` selects them. JSX counts like other code: element lines are code lines, and `&&` and `?:` inside JSX add to complexity. An import of a `.tsx` file or of a folder with `index.tsx` counts toward `fan_out`. This changes the `definitions` hash, so every existing baseline must be re-measured with `baseline`. Measured values of `.go` files do not change. A `.ts` file that imports `./component` or `./pkg` can now resolve `component.tsx` or `pkg/index.tsx`, so its `fan_out` can increase. Re-measure every baseline with `baseline` and review the changes.
- The scanner and ratchet are part of this skill and use pinned measuring tools: gocognit v1.2.0 and dupl v1.0.0 are compiled in, while TypeScript uses its pinned compiler, 5.9.3.
- On first use, `bin/smells` builds the scanner in `~/.cache/code-smells/`. Later runs work offline.
- `.smells.json` defines the files to measure (`files.include`) and the project's `languages`. The check fails if a listed language produces no measurements.
- The check fails, and reports both hashes, when the baseline was measured with different measuring code.
- Use `review --base <ref>` to see what a branch changed without failing the check. Use `report <dir>` to survey a repository that has no baseline.
- When a capped item is removed or drops to or below its limit, a new item may take its cap if it is not larger. The match uses size only, so moving, renaming, shrinking, moving to another package, or splitting code in one change can pass without a manual edit. Keeping the old function as a thin wrapper now frees its cap too. Each cap can pass to only one item no larger than it, caps and totals cannot grow, and `review` names each recipient and whether its source was removed or shrank to the limit. When nothing takes a removed item's cap, `check` says the item is gone and `baseline` drops the cap. `.smells.json` no longer contains `fingerprints`, and `check` no longer rejects them.
- `function_lines` counts only lines that contain code in Go and TypeScript. Comment and blank lines no longer count, so adding documentation to a capped function does not make the check fail. TypeScript lines and tokens come from the parsed tree, so a multi-line template literal with `${}` no longer makes later comment and blank lines count as code or hides duplicates after it.
- `baseline` re-measures a baseline written by another skill version, prints the old and new numbers, and records the build in `measured_by`. The check accepts a re-measure only when every measured file and `go.mod` match the default branch, and its caps are what `baseline` measures. The Go version is recorded there but no longer compared.
- The check and `review` compare with the repository's default branch (`origin/HEAD`) instead of `origin/main`, and say so when it is unknown.
- Bun starts in the skill's folder, not the project, so a project's `bunfig.toml` or `.env` cannot run code inside the scanner.
- `baseline` writes the repository's first baseline as a candidate for moving into place. `.smells.json` no longer contains `schema` or `metrics`; the README defines the metrics, and the check compares the complete `tools` block, including the `definitions` hash.
