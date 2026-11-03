# 0001: One standard-library Go file, run with go run

**Decision:** all logic lives in `plugin/scripts/fuzzsift.go`, using only the standard library. The skill runs it with `go run`.

**Why:** anyone fuzzing Go code has Go installed. One file needs no install step, no binaries per platform and no npm package, and the plugin stays small.

**Cost:** the file is long. Splitting it would need `go run` on a directory, which also works, so this can change later.

**Alternatives:** an MCP server. Claude only needs to run a command and read its output, which a skill with a script does.
