# fuzzsift plugin

This plugin adds the `triaging-go-fuzz-crashers` skill to Claude Code. When a Go package has failing fuzz corpus entries, from `go test -fuzz`, nightly CI fuzzing or a downloaded artifact, Claude runs the bundled `scripts/fuzzsift.go` with `go run`. It runs each entry in its own process, groups the failures by panic message and stack frame or by the failing `t.Fatal` line, and prints the smallest entry and a `go test` command for each group. It needs Go 1.22 or newer and runs the package's tests. See the repository README for examples and limits.

Install it with `claude plugin marketplace add GillMacedo/fuzzsift` and then `claude plugin install fuzzsift@fuzzsift-marketplace`.
