# Roadmap

## Next
- Several packages in one call (`./...`), building each test binary once
- Run entries in parallel, with one mirror directory per worker
- A Markdown output for pull request comments and CI job summaries

## Later
- Minimize the smallest entry of each group with `go test -fuzz` and `-fuzzminimizetime`
- Group `t.Error` failures by the call that failed rather than the last message line
- Convert go-fuzz and libFuzzer crashers to the `go test fuzz v1` format
- Test on Windows and macOS hosts, with `-race`, and with cgo crashes
