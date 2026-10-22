# Changelog

## 0.1.0 — Unreleased

This skill checks that the code size, complexity, duplication and imports of a Go or TypeScript repository do not grow past limits the repository records. Developers and their coding agents use it.

### Added

- **Check.** `bin/smells check` fails when a function, file or package grows past a limit or past its recorded cap.
- **Baseline.** `bin/smells baseline` writes a first `.smells.json` for a repository, with a cap for each item that is already over a limit.
  Action: Read the candidate file it writes, move it over `.smells.json`, and commit it.
- **Report.** `bin/smells report [dir]` shows totals and the ten worst functions for any repository, including one with no baseline.
- **Review.** `bin/smells review [--base <ref>]` shows what a branch changed in the baseline and in the hotspots, and exits 0 unless the code cannot be measured.
- **Cap moves.** A new item can take the cap of an item that was removed or shrank to its limit, if the new item is no larger. Moving, renaming, splitting or wrapping a function therefore needs no manual edit. `review` names each item that took a cap.
- **Measuring tools in the baseline.** `.smells.json` records the measuring tools. The check fails and prints both hashes when the baseline was measured with other tools.
  Action: Run `bin/smells baseline` on a branch of its own and commit the re-measured file.
- **Default branch comparison.** The check and `review` compare with the default branch (`origin/HEAD`) and say so when it is unknown.
- **Code lines only.** The function length counts lines that hold code, so adding comments to a capped function does not fail the check.
- **Safe TypeScript start.** Bun starts in the skill's folder, so a project's `bunfig.toml` or `.env` cannot run code in the scanner.
- **First-run build.** The first run builds the scanner in `~/.cache/code-smells/`, using gocognit v1.2.0, dupl v1.0.0 and the TypeScript compiler 5.9.3. Later runs work offline.
- **MIT licence.** You may use, copy and change the skill under the MIT licence in `LICENSE`.
