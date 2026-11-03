# Contributing

Thanks for helping. Fork the repo, make your change on a branch, and open a pull request. Only the maintainer merges to `main`.

## Setup

    make test
    make lint

You need Go 1.22 or newer. `make lint` also needs Node and Claude Code. The dev container in `.devcontainer/` sets up all of them. The tests need no network.

## Pull requests
- PRs go through an automatic check before a person reads them: changes over 30 lines need a linked open issue (`Closes #<number>`), code changes need tests, `make test` must pass, and a PR can have at most 800 changed lines.
- Keep each one to a single change, with tests.
- A new kind of result needs a package in `tests/integration_test.go` that shows it, and `examples/config/addr` must stay quiet. Use made-up code or public open-source projects, never code from private systems.
- Any corpus file or package it can't fully trust must exit 2. `TestBadEntries`, `TestBadEntriesFromCorpusFlag`, `TestBadCorpusDirs` and `TestBadPackages` must keep passing.
- Files in `examples/config/*/testdata/fuzz/` and `examples/real/` are real output. Don't edit them by hand; say in `examples/README.md` how new ones were made.
- Update `ROADMAP.md` and docs when relevant.
- Releases are automatic: a merge that changes what users install becomes a new version with its changelog line taken from the PR title, so write a title that says what changed for users. Edit `CHANGELOG.md` yourself only if you want different wording. Docs and CI changes wait for the next one.

## Working with AI agents
Agents are welcome. `CLAUDE.md` and `AGENTS.md` have the project's instructions, and `.claude/commands/` has common tasks. Say in your PR that an agent helped, and review the result yourself before submitting. You're responsible for what you submit.

By contributing, you agree that your work is licensed under the MIT License.
