# Changelog

## Unreleased

- The scanner and ratchet from asabirov/amberstone#333 now belong to this skill and use the same Amberstone values: gocognit v1.2.0 and dupl v1.0.0 are compiled in, while TypeScript uses its pinned compiler, 5.9.3.
- On first use, `bin/smells` builds the scanner in `~/.cache/code-smells/`. Later runs work offline.
- `.smells.json` defines the files to measure (`files.include`) and the project's `languages`. The check fails if a listed language produces no measurements.
- The check fails, and reports both hashes, when the baseline was measured with different measuring code.
- Use `review --base <ref>` to see what a branch changed without failing the check. Use `report <dir>` to survey a repository that has no baseline.
- When a capped item is removed, a new item may take its cap if it is not larger. The match uses size only, so moving, renaming, shrinking, moving to another package, or splitting code in one change can pass without a manual edit. `review` lists each new item that takes a cap. When nothing takes a removed item's cap, `check` says the item is gone and `baseline` drops the cap. `.smells.json` no longer contains `fingerprints`, and `check` no longer rejects them.
- `function_lines` counts only lines that contain code in Go and TypeScript. Comment and blank lines no longer count, so adding documentation to a capped function does not make the check fail. TypeScript lines and tokens come from the parsed tree, so a multi-line template literal with `${}` no longer makes later comment and blank lines count as code or hides duplicates after it.
- `baseline` re-measures a baseline written by another skill version, prints the old and new numbers, and records the build in `measured_by`. The check accepts a re-measure only when every measured file and `go.mod` match the default branch, and its caps are what `baseline` measures. The Go version is recorded there but no longer compared.
- The check and `review` compare with the repository's default branch (`origin/HEAD`) instead of `origin/main`, and say so when it is unknown.
- Bun starts in the skill's folder, not the project, so a project's `bunfig.toml` or `.env` cannot run code inside the scanner.
- `baseline` writes the repository's first baseline as a candidate for moving into place. `.smells.json` no longer contains `schema` or `metrics`; the README defines the metrics, and the check compares the complete `tools` block, including the `definitions` hash.

### Planned for a later release

From the CTO and security reviews of PR #2:

- Exceptions never expire. `check` should note an exception whose key is no longer looser than the default branch, and `baseline` should drop it.
- A broken cached scanner does not rebuild itself, and its error does not say to delete the cache folder.
- The README does not say that the cache is never pruned (about 29 MB per skill version), or that `go.mod` asks for Go 1.26.5, which an older Go downloads on first run.
- `baseline` follows a symlinked `.smells.candidate.json` and overwrites its target. It should refuse symlinks.
- Symlinked source files are read, so a link to a file outside the project, or to `/dev/zero`, is measured or hangs the scan. Skip anything that is not a regular file.
- The launcher trusts whatever is in the cache folder. It should create it with mode 700 and refuse one that other users can write to.
- The scanner build inherits the caller's Go environment, such as `GOWORK` and `GOINSECURE`. Build with `GOWORK=off` and `CGO_ENABLED=0`.
- `review` shows a cap that moved to a new item as "tighter: ... (18 -> none)". It should say which item took it.
- `inherit` and `uncapped` could be one function for `check`, `baseline` and `review`; `looserMetric` offers only the freed caps while leaving out every old cap, so it needs care.
