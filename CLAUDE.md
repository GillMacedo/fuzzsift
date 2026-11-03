# fuzzsift

Claude Code skill and Go CLI that runs each entry of a Go package's fuzz corpus (`testdata/fuzz/FuzzX/*`, or a CI artifact with `-corpus`) in its own process, groups the failing entries by how they fail, and prints the smallest entry and a `go test` command for each distinct failure.

## Architecture
`plugin/scripts/fuzzsift.go` holds all the logic in one standard-library file, so `go run` needs no setup. `Triage` runs `go list` and `go test -c` once, lists fuzz targets with `-test.list`, and reads the corpus. `newMirror` makes a temporary copy of the module path down to the package, made of links, with an empty `testdata/fuzz`. `runner.precheck` runs each target with no entries, `runner.runEntry` copies one entry in and runs it, and `Classify` reads the output and exit code and refuses any run where they disagree. `summarize` groups failures by `Failure.Key`. `plugin/skills/triaging-go-fuzz-crashers/SKILL.md` tells Claude when and how to run it. See `docs/architecture.md`.

## Commands
- `make test`: gofmt, vet, unit tests in `plugin/scripts/` on real test binary output, and integration tests in `tests/`, which build throwaway packages, run the tool on them and on `examples/`, and run every `$ ` line in the README and check its output
- `make lint`: plugin and marketplace validation
- `make demo`: run the examples

## Conventions
- Standard library only. The tool runs `go` and the compiled test binary, nothing else, and never uses the network.
- Never write to the user's package directory. Runs happen in the mirror.
- A corpus file, package or run that can't be trusted exits 2. Never print "No entry fails" or exit 0 for it. Add every new kind of bad input to `tests/testdata/bad/`, `TestBadCorpusDirs` or `TestBadPackages`.
- A failing entry must fail twice with the same key. Don't weaken this.
- Files in `examples/config/*/testdata/fuzz/`, `examples/real/` and `tests/testdata/runs/` are real output. `examples/README.md` says what made each one.
- README examples are generated from real output. If output changes, update the README, or `TestReadmeExamples` fails.
- Code must build and pass tests on Go 1.22.

## Do and don't
- Do say which entries were run. A quiet report covers those entries only.
- Don't call code free of bugs, and don't call a group one root cause.

## Definition of done
`make test` and `make lint` pass, and `ROADMAP.md`, the README and docs are updated when relevant.
