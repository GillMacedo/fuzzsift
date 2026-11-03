# Examples

## `config/`
A small made-up Go module with two packages and no dependencies. It is its own module, so `go test ./...` at the repository root doesn't run its failing fuzz entries.

### `config/size`
Parses sizes like `4K` and `16Mi`, with three bugs left in on purpose: a blank string panics (`size.go:22`), a lone `i` panics (`size.go:24`), and `Format` rounds, so `Parse(Format(n))` differs from `n` for sizes that aren't whole units.

Every file in `size/testdata/fuzz/FuzzParse/` was written by `go test -run '^$' -fuzz FuzzParse -fuzztime 60s ./size` on Go 1.27.3, linux/arm64, on 2026-11-03, after `go clean -fuzzcache`. The fuzzer stops at the first failure, so to reach later bugs, some runs were made on scratch copies with earlier bugs patched:

- `8643a79bab153d1b` (`" "`): the current code.
- `7269cbe9808cecce` (`"i"`): a copy with the blank-string bug patched.
- `56a4d1e974615dc0` (`"1100"`), `8bdd82070c526a19` (`"1070"`), `3d1b4a3dd3ef0276`: copies with the first two bugs patched.
- `147ef74e57ecdc3b` and `499dfb62c501743d` (`"8700000000G"`, `"10000000000G"`): a copy with all three bugs patched and without the overflow check that the current code has. They pass now.

### `config/addr`
Splits listen addresses like `[::1]:9090`. Its 5 corpus entries were written the same way by copies with older bugs: no check for a missing colon (`771e938e4458e983`), no check for a missing bracket (`5797e1f0ff3615e0`, `d0884ee533c73b8a`), and no upper limit on the port (`ae3f7e2b7845dd0e`). `40b67391c23ed673` (`":]:1"`) found a bug in a draft of the current code, which was then fixed. The current code passes all 5, and 90 seconds of fresh fuzzing found nothing more.

## `real/`
Output of `fuzzsift` on Go 1.27.3, linux/arm64, on 2026-11-03, in clones of public projects:

- `go-toml-8bbb673-prefix.txt`: `pelletier/go-toml` (MIT, per its LICENSE file) at commit 8bbb6734318beecf9746d68b1c66c18559bae492, with `decode.go`, `marshaler.go`, `unmarshaler.go`, `marshaler_test.go` and `unmarshaler_test.go` checked out from its parent, so the 11 crashers added in that commit run against the code before their fixes.
- `go-toml-686c980-quiet.txt`: `pelletier/go-toml` at commit 686c980c47581bf19b6119b60e9a59f2aed04170, with `-fail`.
- `sh-916eaf5-simplify-reverted.txt`: `mvdan/sh` (BSD-3-Clause) at commit 916eaf5430955997d2fa711dfcd8d1d0341d3b3e, in `syntax/`, with that commit's change to `syntax/simplify.go` reverted.
- `sh-916eaf5-simplify-and-lexer-reverted.txt`: the same, also with the `syntax/lexer.go` change of commit cf432cd085a681d14fc16a83be9a703e77272292 reverted. The target's own seeds hang, and the run exits 2.
- `hcl-hclwrite-94e232b-quiet.txt`: `hashicorp/hcl` (MPL-2.0) at commit 94e232b58b12531a80299e3aa7566f345e17c21b, in `hclwrite/fuzz`, with `-fail`.
- `go-json-jsontext-c443dcc-quiet.txt`: `goccy/go-json` (MIT) at commit c443dccd4887ffe7c3d57fac0d2dd9d2bdaff82b, in `jsontext`, with `-fail`.

The reproduce commands printed for go-toml and mvdan/sh were run by hand and failed.

## `../tests/testdata/`
- `runs/`: real output of a test binary for a small package at `/tmp/exp/q`, whose test file is `runs/q_test.go.txt`, built with Go 1.27.3 and Go 1.22.12 and run once per entry the way fuzzsift runs it, with `-test.timeout=1s`. The file name holds the entry, Go version and exit code.
- `bad/`: corpus files that can't be trusted: empty, blank, 200 random bytes, a real entry converted to UTF-16 with `iconv`, real entries cut after 20 and 26 bytes, the header alone, an `int` for a `string` target, two values for a one-value target, a `v2` header, and real output saved from a failed `go build`, a failed `go test ./...` and the failing `go test ./size` on `examples/config`.
- `ci-artifact/FuzzSplit/`: one real entry from `config/addr`, and another cut after 25 bytes.
- `broken/`: a copy of `config/addr` with a type error added to `Join`.
