# 0002: Refuse anything that could produce a false all clear

**Decision:** an unreadable corpus file, a file outside a target directory, an entry for a target the package doesn't have, a package that doesn't build or has no fuzz targets, a target that fails, hangs or skips itself with no entries, a run whose output and exit code disagree, and a run where every entry skipped all exit 2. A report with any of these says "Incomplete" and never "No entry fails".

**Why:** earlier tools in this family passed their own tests and still reported a failed build or an unreadable report as all clear. Here, `go test -run '^FuzzX$/^missing$'` prints PASS and exits 0, so trusting the exit code alone would call an entry that never ran a pass.

**Cost:** one bad file makes the whole run exit 2, even when the other entries were triaged. The report still shows them.

**Alternatives:** warn and exit 0. A warning in CI output is easy to miss, and the exit code is what scripts read.
