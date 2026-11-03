# fuzzsift: triage Go fuzz crashers by running each corpus entry alone

[![CI](https://github.com/GillMacedo/fuzzsift/actions/workflows/ci.yml/badge.svg)](https://github.com/GillMacedo/fuzzsift/actions/workflows/ci.yml) [![MIT license](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

**A Claude Code skill and Go CLI for Go developers with a `testdata/fuzz` directory full of failing inputs from `go test -fuzz`, nightly CI fuzzing or a teammate's branch. It runs every corpus entry in its own process, groups the failures by panic message and stack frame (or by the failing `t.Fatal` line), names the smallest entry and a `go test` command for each distinct failure, and lists the entries that pass now.**

`go test` doesn't group or triage fuzz crashers. It runs the seed corpus in one process, so the first entry that panics kills the run and hides every entry after it. One unreadable corpus file (cut off, empty, from an older fuzz signature) makes `go test` fail every entry of that target before any of them runs. And ten crashers in a directory may be one bug or ten. Crash triage tools exist for other fuzzers: CASR's README lists libFuzzer and AFL++ based fuzzers, including the older `go-fuzz`, and doesn't list `go test -fuzz`. `fuzzsift` builds the package's test binary once and runs each entry with `-run '^FuzzX$/^entry$'`, in a copy of the package directory that holds only that one entry.

## What you can ask Claude

- "Nightly fuzzing left 12 files in testdata/fuzz/FuzzParse. How many different bugs are they?"
- "Which of these fuzz crashers still fail after my fix, and which pass now?"
- "go test stops at the first panic in my fuzz corpus. Show me all the failures."
- "CI uploaded a fuzz crasher as an artifact. Triage it against this package without copying it into the repo."
- "go test says my corpus file is malformed. Which file is it, and does anything else fail?"

## Install

As a Claude Code plugin:

    claude plugin marketplace add GillMacedo/fuzzsift
    claude plugin install fuzzsift@fuzzsift-marketplace

Or as a CLI with Go 1.22 or newer. It has no dependencies:

    go run github.com/GillMacedo/fuzzsift/plugin/scripts@latest ./internal/parser

## Example

`examples/config/size` is a small made-up package that parses sizes like `4K` and `16Mi`, with three bugs left in on purpose. Its 7 corpus entries are real `go test -fuzz` output: five from the current code and two from an older version with an overflow bug that is now fixed (`examples/README.md` says how each was made). From a clone of this repo:

    $ go run ./plugin/scripts examples/config/size
    fuzzsift: example.com/config/size, 1 fuzz target, 7 corpus entries
    Each entry ran alone with a 10s timeout. Each failure ran twice.

    FuzzParse: 7 entries, 5 fail in 3 distinct ways, 2 pass

      1. test failure: Parse(Format(1100)) = 1024, <nil> (3 entries)
         at size_test.go:27
         smallest: examples/config/size/testdata/fuzz/FuzzParse/56a4d1e974615dc0 (31 bytes)
         reproduce: go test -run='^FuzzParse$/^56a4d1e974615dc0$' ./examples/config/size
         same failure: 3d1b4a3dd3ef0276, 8bdd82070c526a19

      2. panic: runtime error: index out of range [-1] (1 entry)
         at example.com/config/size.Parse (size/size.go:22)
         smallest: examples/config/size/testdata/fuzz/FuzzParse/8643a79bab153d1b (28 bytes)
         reproduce: go test -run='^FuzzParse$/^8643a79bab153d1b$' ./examples/config/size

      3. panic: runtime error: index out of range [-1] (1 entry)
         at example.com/config/size.Parse (size/size.go:24)
         smallest: examples/config/size/testdata/fuzz/FuzzParse/7269cbe9808cecce (28 bytes)
         reproduce: go test -run='^FuzzParse$/^7269cbe9808cecce$' ./examples/config/size

      Pass now: 147ef74e57ecdc3b, 499dfb62c501743d

    5 of 7 entries fail, in 3 distinct ways. 2 pass. 14 test binary runs.

The two panics have the same message and are on different lines, so they are two groups. The three round-trip failures differ only in their numbers, so they are one.

Plain `go test` on the same corpus reports three failing entries and then dies on the first panic. The entries `8643a79bab153d1b` (the blank-string panic at `size.go:22`) and `8bdd82070c526a19` never run:

    $ cd examples/config && go test ./size 2>&1 | grep -o -e 'FAIL: FuzzParse/[0-9a-f]*' -e '^panic: runtime error: index out of range \[-1\]'
    FAIL: FuzzParse/3d1b4a3dd3ef0276
    FAIL: FuzzParse/56a4d1e974615dc0
    FAIL: FuzzParse/7269cbe9808cecce
    panic: runtime error: index out of range [-1]

### A healthy package stays quiet

`examples/config/addr` splits listen addresses. Its 5 corpus entries are crashers the fuzzer found in earlier versions, and the bugs are fixed. The report says what it covered and doesn't call the code bug-free:

    $ go run ./plugin/scripts -fail examples/config/addr
    fuzzsift: example.com/config/addr, 1 fuzz target, 5 corpus entries
    Each entry ran alone with a 10s timeout. Each failure ran twice.

    FuzzSplit: 5 entries, 5 pass

      Pass now: 40b67391c23ed673, 5797e1f0ff3615e0, 771e938e4458e983, ae3f7e2b7845dd0e, d0884ee533c73b8a

    No entry fails: 5 of 5 pass. 7 test binary runs.
    That covers these entries only. It doesn't show the code has no bugs.

### From a CI artifact, with a file it can't trust

`-corpus` reads entries from outside the package: a directory named after one target, or a directory with one subdirectory per target, such as a downloaded CI artifact. `tests/testdata/ci-artifact/FuzzSplit` holds one real entry and one cut off partway. The cut file is named, the run exits 2, and the report never says all is well:

    $ go run ./plugin/scripts -corpus tests/testdata/ci-artifact/FuzzSplit examples/config/addr 2>&1 || true
    fuzzsift: example.com/config/addr, 1 fuzz target, 2 corpus entries
    Each entry ran alone with a 10s timeout. Each failure ran twice.

    Not triaged (1 entry):
      tests/testdata/ci-artifact/FuzzSplit/d0884ee533c73b8a: go test can't read it: unmarshal: malformed line "string(\"[": (test):1:8: string literal not terminated

    FuzzSplit: 2 entries, 1 passes, 1 not triaged

      Pass now: 771e938e4458e983

    Incomplete: 1 of 2 entries was not triaged, so this is not a full result.
    exit status 2

The last line comes from `go run`, which prints the exit code it got. For an entry outside the package, the reproduce command copies the file into `testdata/fuzz/<target>/` first.

### Input it won't trust

A package that doesn't build is refused with exit code 2, and nothing is reported as passing:

    $ go run ./plugin/scripts tests/testdata/broken 2>&1 || true
    fuzzsift: nothing was triaged: example.com/broken failed to build, so no entry ran:
      # example.com/broken [example.com/broken.test]
      ./broken.go:46:9: invalid operation: host + ":" + port (mismatched types string and int)
    exit status 2

The same goes for an empty, blank, binary, UTF-16 or cut-off corpus file, a file of compiler or `go test` output saved by mistake into the corpus, a file for an older fuzz signature, an entry for a target that no longer exists, a file outside any target's directory, a missing or empty corpus directory, a package with no fuzz targets, and a test binary whose exit code doesn't match its output. A target that fails, hangs or skips itself with no corpus entries (in its `f.Add` seeds, `init` or `TestMain`) is refused too, because its entries' failures couldn't be told apart from that. When every entry skips itself, nothing was checked, and that exits 2 as well. Each case is a test in `tests/` and `plugin/scripts/`.

### Real projects

Run on 2026-11-03 on Go 1.27.3, linux/arm64. Full output is in `examples/real/`. These need the network to fetch the projects, so they aren't `$` examples.

**pelletier/go-toml v2**, commit 8bbb673 ("Fuzzing setup and fixes"), which added 11 fuzz crashers together with the fixes for them. With the four changed source and test files put back to their state before that commit, 10 of the 11 entries fail and `fuzzsift` puts them in 6 groups. One group of 4 is testify's "Not equal" at the same line, and may hold more than one cause (see Limits). The 11th entry skips itself because it contains "nan":

    FuzzUnmarshal: 11 entries, 10 fail in 6 distinct ways, 1 skipped

      1. test failure: Not equal: (4 entries)
         at fuzz_test.go:54
      2. test failure: failed round trip: toml: invalid character (2 entries)
         at fuzz_test.go:52
      3. test failure: cannot marshal unmarshaled document: toml: new line characters in keys are not supported (1 entry)
         at fuzz_test.go:43

On today's go-toml (commit 686c980) the same 11 entries give "No entry fails: 10 of 11 pass and 1 skipped".

**mvdan/sh**, commit 916eaf5 ("syntax: fix a crash with LangZsh and Simplify(true)"), with the two-line fix in `syntax/simplify.go` reverted. The crasher from that commit fails with a nil pointer panic, and the other entries of `FuzzParsePrint` skip themselves as invalid shell input. All 8 entries of `FuzzQuote` skip themselves on this machine:

      1. panic: runtime error: invalid memory address or nil pointer dereference (1 entry)
         at mvdan.cc/sh/v3/syntax.(*simplifier).inlineSimpleParams (syntax/simplify.go:155)

With the hang fix from commit cf432cd also reverted in `syntax/lexer.go`, `FuzzParsePrint` hangs on its own `f.Add` seeds. `fuzzsift` refused to triage its entries and exited 2. `go test -run 'FuzzParsePrint/seed#'` timed out the same way.

The printed reproduce commands for go-toml, mvdan/sh and `examples/config/size` were run by hand and failed. Healthy cases that stayed quiet: today's go-toml, `hashicorp/hcl` `hclwrite/fuzz` (29 entries, commit 94e232b), `hclsyntax/fuzz` (29 entries), and `goccy/go-json` `jsontext` (35 entries, commit c443dcc).

## Usage

    fuzzsift [flags] [DIR]

`DIR` is the directory of one package (default `.`). Flags go before it.

- `-corpus DIR`: read entries from this directory instead of `DIR/testdata/fuzz`. It can hold one subdirectory per target, or be one target's directory (its name is the target's).
- `-run REGEXP`: only triage fuzz targets whose names match.
- `-timeout D`: timeout for each run of an entry (default 10s). An entry that runs longer is a hang, grouped by the stack of the goroutine running the fuzz function.
- `-tags LIST`, `-race`: build flags, the same as for `go test`.
- `-fail`: exit 1 when any entry fails.
- `-json`: print the full report as JSON, with every entry and its result.

Exit codes: 0 when every entry was triaged, 1 with `-fail` when an entry fails, 2 when the package or any entry can't be trusted. `go run` turns every non-zero exit code into 1 and prints `exit status N`, so build the tool with `go build` if a script needs to tell them apart.

## What it reports

| Result | Meaning |
|---|---|
| fail | The entry failed twice in the same way. Grouped with entries that fail the same way |
| pass now | The entry passes. Keep it as a regression seed |
| skipped | The fuzz function called `t.Skip` for this input |
| not repeatable | The entry failed once and then passed, or failed differently the second time |
| not triaged | `go test` can't read the file, its target doesn't exist, or the run's output and exit code disagree. Exit code 2 |

A group's key is the kind of failure (panic, fatal error, signal, hang, test failure, process exit), the message with quoted text and numbers removed, and the top stack frame outside the standard library. For `t.Error` and `t.Fatal`, the frame is the file and line of the last message before the failure.

## How it works

1. `go list` finds the package and its module. `go test -c` builds one test binary. `-test.list '^Fuzz'` lists the fuzz targets.
2. A temporary directory mirrors the module from its root down to the package, with links to every file, so relative paths like `testdata/golden.txt` and `../shared` still work. Only `testdata/fuzz` is replaced with an empty directory.
3. Each target first runs with no corpus entries. It must pass, or none of its entries can be judged.
4. Each entry is copied alone into the mirror and run with `-test.v`, `-test.paniconexit0`, `-test.timeout` and `-test.run '^FuzzX$/^entry$'`. The output must contain the entry's own result line, and the exit code must match it.
5. Each failing entry runs a second time. A different result makes it "not repeatable".

The package directory itself is never written to. See `docs/architecture.md`.

## Limits

- Grouping is by symptom, not root cause. Two bugs that fail at the same assertion with the same message shape land in one group, and one bug that panics in two places makes two groups. The go-toml run shows the first case.
- For `t.Error` followed by `t.Log`, the group uses the last message, which is the log line.
- It runs one package per call, and runs entries one at a time.
- Hangs take the full `-timeout` twice per entry.
- It doesn't minimize inputs. "Smallest" is the smallest file in a group.

## What was tested

- Unit tests on real test binary output from Go 1.27.3 and Go 1.22.12: panics, `t.Fatal`, a stack overflow, a hang, `log.Fatal`, `os.Exit(0)`, `t.Skip`, and every prefix of those outputs cut off.
- Integration tests that build throwaway packages and run the tool on them, on `examples/`, and on each bad file in `tests/testdata/bad/` placed into a healthy corpus and a failing one, and given with `-corpus`. Each bad case must exit 2 with and without `-fail`. Every `$ ` line in this README is run and its output compared.
- The test suite passes on Go 1.27.3 and Go 1.22.12, on linux/arm64. CI runs it on linux/amd64.
- Real projects: the go-toml, mvdan/sh, hcl and go-json runs above.

Not tested: Windows and macOS hosts (the mirror uses symlinks, which may need extra rights on Windows), an entry that triggers a data race report under `-race` (a `-race` build of the examples was only run once by hand), cgo crashes, and corpora with thousands of entries.

## FAQ

**Why not just run `go test`?** It runs all entries in one process. The first panic ends it, and one unreadable file stops the whole target. It also doesn't say which failures are the same.

**Does it work with go-fuzz or OSS-Fuzz crashers?** Only files in the `go test fuzz v1` format, which `go test -fuzz` writes. Crashers from go-fuzz or libFuzzer need converting first.

**Does it change my repository?** No. It runs in a temporary copy that links to your files. Your tests can still write through those links, the same as under `go test`.

**Does it run my code?** Yes, it builds and runs your package's tests, the same as `go test`. Only use it on code you'd run `go test` on.

**Does "No entry fails" mean my parser is safe?** No. It means the entries in the corpus pass. Fuzz again to look for new bugs.

**Can I use it in CI?** Yes. `fuzzsift -fail ./pkg` exits 1 when an entry fails and 2 when the corpus or package can't be trusted.

## License

MIT. See [LICENSE](LICENSE).

## Contributing

Contributions are welcome, from people and from people working with AI agents. See [CONTRIBUTING.md](CONTRIBUTING.md). Good first issues are [listed here](https://github.com/GillMacedo/fuzzsift/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22). The most useful bug reports include a corpus entry and a package that reproduce the problem.

### How pull requests work

1. **Open a PR.** Fork the repo, work on a branch, and keep it to one change. Write a title that says what changes for users, because it becomes the changelog line: `feat:` goes under Added and `fix:` under Fixed.
2. **Automatic check.** Code changes need tests, and `make test` must pass. Changes over 30 lines need a linked open issue (`Closes #<number>`), and a PR can have at most 800 changed lines. If something is missing, a comment tells you what to fix.
3. **Review.** Claude reviews each PR automatically. Code PRs also get a security review and are tested in a sandbox. The review then merges the PR or asks for changes, and a small problem may become a follow-up issue instead. A PR with no new commit 14 days after a change request is closed, and you can reopen it. PRs that change CI, agent settings, npm or Python dependencies, vendored code or build files can't be merged this way, and a comment says so. The same goes for anything the security review doesn't clear.
4. **Release.** A merge that changes what users install becomes a new version within about two hours, with a GitHub Release and, if the project has one, its npm package. Docs and CI changes wait for the next release.

## AI disclosure

This project was largely generated with Claude Code and reviewed by its maintainer.

## About me

I'm Giulliano Macedo, and I build developer tools at GoTech Solutions. Follow along or get in touch:

- GitHub: [GillMacedo](https://github.com/GillMacedo)
- LinkedIn: [giulliano](https://www.linkedin.com/in/giulliano/)
- X: [@giumacedo](https://x.com/giumacedo)

If `fuzzsift` saved you some prompts, a star helps others find it.

---

Built by [Giulliano Macedo](https://github.com/GillMacedo), GoTech Solutions · [LinkedIn](https://www.linkedin.com/in/giulliano/) · [X](https://x.com/giumacedo)
