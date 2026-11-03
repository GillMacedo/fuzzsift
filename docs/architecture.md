# Architecture

`fuzzsift` is one standard-library Go file, `plugin/scripts/fuzzsift.go`, plus a skill that tells Claude when and how to run it.

## Flow

1. **Find the package.** `go list -json` gives the import path, directory, module and test files. No test files exits 2.
2. **Build once.** `go test -c` builds one test binary into a temporary directory, with `-tags` and `-race` if given. A build failure exits 2 with the compiler's first lines.
3. **Mirror the package.** `newMirror` creates a temporary tree from the module root down to the package. At each level it links every entry except the next directory down. In the package directory it links everything except `testdata`, and in `testdata` everything except `fuzz`, which starts empty. Tests that read `testdata/x`, `../shared/y` or walk up to `go.mod` see the same files as under `go test`, and nothing is written into the real package.
4. **List targets.** The binary's `-test.list ^Fuzz` gives the fuzz targets. None listed means no `Fuzz` functions, or a `TestMain` that never calls `m.Run`. Both exit 2.
5. **Read the corpus.** `DIR/testdata/fuzz`, or `-corpus`. A directory named after a listed target is read as that target's entries. Otherwise each subdirectory is a target. Files outside a target directory, and directories of targets the package doesn't have, are "not triaged".
6. **Precheck.** Each target runs with an empty corpus directory, with `-test.run '^FuzzX$'`. Its `f.Add` seeds and setup must pass with exit 0 and a `--- PASS: FuzzX` line. If not, its entries can't be judged and are "not triaged".
7. **Run each entry.** The file is copied alone into the mirror's `testdata/fuzz/FuzzX/` and the binary runs with `-test.v -test.paniconexit0 -test.count=1 -test.timeout=T -test.run '^FuzzX$/^name$'`. The name is escaped with `\x{..}` for spaces, because the testing package rewrites spaces in `-run` patterns but not in corpus subtest names. A hard kill at `T + 30s` covers a binary that ignores its own timeout.
8. **Classify.** `Classify` reads the output and exit code:
   - A `"testdata/fuzz/FuzzX/name": ...` line: go test couldn't read the file. Not triaged.
   - No `=== RUN   FuzzX/name` line: the entry never ran. Not triaged.
   - `panic:`, `fatal error:` or `SIG...:` with a non-zero exit: a failure. The frame is the first frame in the first goroutine after it that isn't `panic`, `_testmain.go` or the standard library. `panic: test timed out` is a hang, and its frame comes from the goroutine running `FuzzX.func...`.
   - `--- FAIL: FuzzX/name` with a non-zero exit: a test failure, located at the last `file.go:NN:` message before it.
   - `--- PASS` or `--- SKIP` with exit 0: pass or skip.
   - A non-zero exit after the entry started, with none of the above: `os.Exit` or `log.Fatal` in the code under test.
   - Any other combination, such as a failure line with exit 0 or exit 0 with no result line: not triaged.
9. **Repeat failures.** Each failing entry runs again. A pass, a skip or a different key makes it "not repeatable".
10. **Group and report.** The key is kind, normalized message (quoted text and numbers replaced) and frame. Groups are sorted by size. The smallest file in each gives the reproduce command.

## Standard library frames

A frame is in the standard library if its file is under `go env GOROOT`, or if it is outside the module and its package path has no dot in the first element. The second rule handles `-trimpath` builds (`testing/testing.go`) and traces from another Go installation.

## Why the checks are strict

A triage tool can say "no entry fails" for the wrong reason: the package didn't build, a corpus file was unreadable and `go test` failed the target before running anything, `TestMain` returned before running tests, the `-run` pattern matched nothing (the binary then prints PASS and exits 0), or every entry skipped itself. Each check above exists so that none of these produces a quiet report or exit code 0. See `docs/decisions/0002-refuse-untrusted-input.md`.
