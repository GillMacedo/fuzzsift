# 0003: One process per entry, in a mirrored directory

**Decision:** each corpus entry runs in its own test binary process, with only that entry in `testdata/fuzz/FuzzX/`, in a temporary tree of links that mirrors the module down to the package.

**Why:** a panic ends the process, so one process per entry is the only way to see every failure. `-test.run` selects one entry, but the testing package reads every file in the target's directory first and fails the whole target on one unreadable file, so the directory must hold only the entry being run. The testing package reads `testdata/fuzz` relative to the working directory and has no flag to change it, so the binary runs in a mirror rather than in the package.

**Cost:** one process start per entry, plus a second for each failure. Symlinks may need extra rights on Windows, which is untested.

**Alternatives:** copying entries into the real package directory changes the user's files. Parsing corpus files ourselves and calling the fuzz function directly would need code generation.
