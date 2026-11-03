Add a test for this input that fuzzsift must refuse: $ARGUMENTS

1. Make the input from a real command run, not by hand, where you can: a corpus file goes in `tests/testdata/bad/`, a broken package goes in `TestBadPackages` in `tests/integration_test.go`, and a corpus directory layout goes in `TestBadCorpusDirs`.
2. Run `make test`. `TestBadEntries` and `TestBadEntriesFromCorpusFlag` pick up every file in `tests/testdata/bad/` and put it into a healthy corpus and a failing one. Each run must exit 2 with and without `-fail`, and must not print "No entry fails". If it doesn't, fix the tool.
3. Describe the input in `examples/README.md`.
