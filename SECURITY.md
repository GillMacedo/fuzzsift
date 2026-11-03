# Security

Report vulnerabilities through [GitHub's private vulnerability reporting](https://github.com/GillMacedo/fuzzsift/security/advisories/new), not in a public issue.

`fuzzsift` builds and runs the tests of the package you point it at, the same as `go test`, so only run it on code you trust. Corpus files are passed to your own fuzz function, which is what `go test` does too. A wrong grouping or a missed failure is a normal bug. A case where it writes into your package directory, reads files you didn't point it at, or runs something you didn't ask for is a security issue, so report that privately. It doesn't use the network itself; your tests might.
