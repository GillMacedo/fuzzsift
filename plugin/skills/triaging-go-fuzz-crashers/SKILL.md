---
name: triaging-go-fuzz-crashers
description: Triages a Go package's fuzz corpus (testdata/fuzz/FuzzX files from go test -fuzz, nightly CI fuzzing or a downloaded artifact). Runs each corpus entry in its own process, groups failing entries by panic message and stack frame or by the failing t.Fatal line, and prints the smallest entry and a go test command for each distinct failure, plus the entries that pass now. Use when the user has several fuzz crashers and asks how many bugs they are, which still fail after a fix, why go test stops at the first fuzz panic, or why go test says a corpus file is malformed.
---

# Triaging Go fuzz crashers

`go test` runs a fuzz target's seed corpus in one process. The first entry that panics ends the run, so later entries never run, and one unreadable corpus file makes every entry of that target fail before running. The bundled tool runs each entry alone and groups the failures. It needs Go 1.22 or newer and runs the package's own tests, so use it only on code the user would run `go test` on.

## Run the tool

Pass the directory of one package, such as `./internal/parser`:

    go run "${CLAUDE_PLUGIN_ROOT}/scripts/fuzzsift.go" [flags] ./path/to/pkg

- Entries outside the package (a CI artifact): add `-corpus DIR`. `DIR` can be one target's directory (named after the target) or hold one subdirectory per target. Don't copy the files into the repo first.
- One target only: `-run FuzzParse`.
- A target that hangs: lower `-timeout` (default 10s per run) to save time.
- Build tags or the race detector: `-tags`, `-race`.
- `-json` gives every entry and its result for further work.
- Exit code 2 means something can't be trusted: an unreadable corpus file, a target that no longer exists, a package that doesn't build, a target that fails or hangs with no entries, or every entry skipped. Report what the tool printed under "Not triaged". Never present a run that ended with exit code 2 as "no failures".

## Explain each group

1. Lead with the number of distinct failures and the number of failing entries.
2. For each group, open the printed frame (`at pkg.Func (file.go:NN)`) and read the code there. For a test failure the location is the assertion in the fuzz function; read the message to see which property broke.
3. Show the input of the smallest entry: `cat` the file. Corpus files are text: `go test fuzz v1` and one Go literal per argument.
4. Run the printed reproduce command once to show the failure before changing code.
5. "Pass now" entries are regression seeds. Suggest keeping them in `testdata/fuzz` so `go test` keeps checking them.
6. "Not repeatable" entries depend on something besides the input (time, randomness, global state, the network). Say so; don't treat them as fixed.
7. An unreadable file in `testdata/fuzz` blocks every entry of its target under `go test`. Suggest fixing or removing it, and say which.

## Don't

- Don't say two groups are one bug, or that one group is one bug, without reading the code. Groups are by symptom.
- Don't call the code bug-free because no entry fails. Only these entries were checked.
- Don't edit code or delete corpus files unless the user asks.
