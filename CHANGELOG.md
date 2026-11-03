# Changelog

All notable changes are documented here, in [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) format. This project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.0] - 2026-11-03
### Added
- Runs each entry of a Go package's fuzz corpus in its own process, so a panic in one entry doesn't hide the rest, and an unreadable file doesn't stop its whole target
- Groups failing entries by kind, normalized message and top stack frame outside the standard library, or by the failing `t.Error` or `t.Fatal` line, and prints the smallest entry and a `go test` command for each group
- Lists entries that pass now and entries the target skips, and runs each failure twice to report entries that aren't repeatable
- Reports panics, fatal errors such as stack overflows, signals, hangs (with `-timeout`), `t.Fatal` failures, and `os.Exit` or `log.Fatal` in the code under test
- `-corpus` reads entries from outside the package, such as a CI artifact, without copying them into it
- Refuses with exit code 2: unreadable corpus files, entries of missing targets, packages that don't build or have no fuzz targets, targets that fail, hang or skip with no corpus entries, runs whose exit code and output disagree, and runs where every entry was skipped
- `-run`, `-timeout`, `-tags`, `-race`, `-fail` and `-json` options
- Claude Code plugin with the `triaging-go-fuzz-crashers` skill
