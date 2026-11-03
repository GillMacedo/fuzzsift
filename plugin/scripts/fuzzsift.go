// Command fuzzsift runs every entry of a Go package's fuzz corpus on its
// own, groups the failing entries by how they fail, and names the smallest
// entry and a go test command for each distinct failure.
//
// go test stops at the first entry that panics, and one unreadable corpus
// file stops every entry of its target, so a directory of crashers from
// go test -fuzz or CI shows one failure at a time. fuzzsift builds the test
// binary once and runs each entry in a separate process.
//
// Usage:
//
//	fuzzsift [flags] [DIR]
//
// Exit codes: 0 when every entry was triaged, 1 with -fail when an entry
// fails, 2 when the package or any entry can't be trusted.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const version = "0.1.0"

// Result of one corpus entry.
const (
	Pass       = "pass"
	Skip       = "skip"
	Fail       = "fail"
	Flaky      = "flaky"
	Unreadable = "unreadable"
	Untrusted  = "untrusted"
)

// Failure is how one run of an entry failed.
type Failure struct {
	// Kind is panic, fatal, signal, hang, error or exit.
	Kind string `json:"kind"`
	// Message is the first line of the panic or failure, as printed.
	Message string `json:"message"`
	// Frame is the top stack frame outside the standard library, or the
	// file and line of the failing t.Error or t.Fatal call.
	Frame string `json:"frame,omitempty"`
	// Key groups failures: kind, normalized message and frame.
	Key string `json:"key"`
}

// Entry is one corpus file.
type Entry struct {
	Target  string   `json:"target"`
	Name    string   `json:"name"`
	Path    string   `json:"path"`
	Size    int64    `json:"size"`
	Result  string   `json:"result"`
	Reason  string   `json:"reason,omitempty"`
	Failure *Failure `json:"failure,omitempty"`
	// Second is the second run of a failing entry when it differs.
	Second    *Failure `json:"second,omitempty"`
	abs       string
	inPackage bool
}

// Group is a set of entries that fail the same way.
type Group struct {
	Target    string   `json:"target"`
	Kind      string   `json:"kind"`
	Message   string   `json:"message"`
	Frame     string   `json:"frame,omitempty"`
	Key       string   `json:"key"`
	Entries   []string `json:"entries"`
	Smallest  string   `json:"smallest"`
	Reproduce string   `json:"reproduce"`
}

// Target is the result for one fuzz target.
type Target struct {
	Name    string   `json:"name"`
	Problem string   `json:"problem,omitempty"`
	Entries []*Entry `json:"entries"`
	Groups  []*Group `json:"groups"`
}

// Report is the whole result.
type Report struct {
	Version   string    `json:"version"`
	Package   string    `json:"package"`
	Dir       string    `json:"dir"`
	Corpus    string    `json:"corpus"`
	Timeout   string    `json:"timeout"`
	Targets   []*Target `json:"targets"`
	Orphans   []*Entry  `json:"orphans,omitempty"`
	Entries   int       `json:"entries"`
	Failing   int       `json:"failing"`
	Distinct  int       `json:"distinct"`
	Passing   int       `json:"passing"`
	Skipped   int       `json:"skipped"`
	Flaky     int       `json:"flaky"`
	NotTriage int       `json:"notTriaged"`
	Runs      int       `json:"runs"`
	// Done is true when every entry was triaged and not all were skipped.
	Done bool `json:"complete"`
}

// Complete reports whether every entry was triaged and at least one
// entry passed or failed. When every entry skips itself, nothing was
// checked.
func (r *Report) Complete() bool { return r.NotTriage == 0 && !r.AllSkipped() }

// AllSkipped reports whether every entry that ran was skipped.
func (r *Report) AllSkipped() bool { return r.Skipped > 0 && r.Passing+r.Failing == 0 }

// fatalError means nothing was triaged.
type fatalError struct{ msg string }

func (e *fatalError) Error() string { return e.msg }

func fatalf(format string, args ...any) error {
	return &fatalError{fmt.Sprintf(format, args...)}
}

type config struct {
	dir     string
	corpus  string
	run     string
	tags    string
	race    bool
	timeout time.Duration
	fail    bool
	json    bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("fuzzsift", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var c config
	fs.StringVar(&c.corpus, "corpus", "", "directory with one subdirectory per fuzz target (default DIR/testdata/fuzz)")
	fs.StringVar(&c.run, "run", "", "only triage fuzz targets matching this regular expression")
	fs.StringVar(&c.tags, "tags", "", "build tags, as for go test")
	fs.BoolVar(&c.race, "race", false, "build with the race detector")
	fs.DurationVar(&c.timeout, "timeout", 10*time.Second, "timeout for each run of an entry")
	fs.BoolVar(&c.fail, "fail", false, "exit 1 when any entry fails")
	fs.BoolVar(&c.json, "json", false, "print the report as JSON")
	showVersion := fs.Bool("version", false, "print the version")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "usage: fuzzsift [flags] [DIR]\n\nRuns each fuzz corpus entry of the Go package in DIR (default .) alone and groups the failures.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, "fuzzsift", version)
		return 0
	}
	switch fs.NArg() {
	case 0:
		c.dir = "."
	case 1:
		c.dir = fs.Arg(0)
	default:
		fmt.Fprintln(stderr, "fuzzsift: nothing was triaged: give one package directory; flags go before it")
		return 2
	}
	if c.timeout <= 0 {
		fmt.Fprintln(stderr, "fuzzsift: nothing was triaged: -timeout must be positive")
		return 2
	}
	rep, err := Triage(c)
	if err != nil {
		fmt.Fprintf(stderr, "fuzzsift: nothing was triaged: %v\n", err)
		return 2
	}
	rep.Done = rep.Complete()
	if c.json {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		enc.Encode(rep)
	} else {
		writeText(stdout, rep, c)
	}
	switch {
	case !rep.Complete():
		return 2
	case c.fail && rep.Failing > 0:
		return 1
	}
	return 0
}

// pkgInfo is what go list says about the package.
type pkgInfo struct {
	ImportPath   string
	Dir          string
	Name         string
	TestGoFiles  []string
	XTestGoFiles []string
	Module       *struct {
		Path string
		Dir  string
	}
	Error *struct{ Err string }
}

// Triage builds the package's test binary and runs every corpus entry.
func Triage(c config) (*Report, error) {
	dir, err := filepath.Abs(c.dir)
	if err != nil {
		return nil, fatalf("%v", err)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, fatalf("%s is not a directory", c.dir)
	}
	var runRE *regexp.Regexp
	if c.run != "" {
		if runRE, err = regexp.Compile(c.run); err != nil {
			return nil, fatalf("-run: %v", err)
		}
	}
	pkg, err := goList(dir, c)
	if err != nil {
		return nil, err
	}
	if len(pkg.TestGoFiles)+len(pkg.XTestGoFiles) == 0 {
		return nil, fatalf("%s has no _test.go files, so it has no fuzz targets", pkg.ImportPath)
	}
	corpus := c.corpus
	if corpus == "" {
		corpus = filepath.Join(dir, "testdata", "fuzz")
	}
	corpusAbs, err := filepath.Abs(corpus)
	if err != nil {
		return nil, fatalf("%v", err)
	}
	st, err := os.Stat(corpusAbs)
	if err != nil {
		return nil, fatalf("no corpus directory at %s", display(corpusAbs))
	}
	if !st.IsDir() {
		return nil, fatalf("%s is a file; the corpus is a directory with one subdirectory per fuzz target", display(corpusAbs))
	}

	tmp, err := os.MkdirTemp("", "fuzzsift-")
	if err != nil {
		return nil, fatalf("%v", err)
	}
	defer os.RemoveAll(tmp)
	bin, err := build(dir, tmp, pkg, c)
	if err != nil {
		return nil, err
	}
	modRoot := dir
	modPath := pkg.ImportPath
	if pkg.Module != nil && pkg.Module.Dir != "" {
		modRoot = pkg.Module.Dir
		modPath = pkg.Module.Path
	}
	mirror, err := newMirror(modRoot, dir, filepath.Join(tmp, "run"))
	if err != nil {
		return nil, fatalf("making the run directory: %v", err)
	}
	r := &runner{bin: bin, dir: mirror, timeout: c.timeout, ctx: frameContext{goroot: goroot(), modDir: modRoot, modPath: modPath}}

	targets, err := r.list()
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, fatalf("the test binary of %s lists no fuzz targets (no Fuzz functions, or a TestMain that never calls m.Run)", pkg.ImportPath)
	}
	listed := map[string]bool{}
	for _, t := range targets {
		listed[t] = true
	}

	rep := &Report{Version: version, Package: pkg.ImportPath, Dir: dir, Corpus: corpusAbs, Timeout: c.timeout.String()}
	pkgCorpus := filepath.Join(dir, "testdata", "fuzz")
	items, err := os.ReadDir(corpusAbs)
	if err != nil {
		return nil, fatalf("reading %s: %v", display(corpusAbs), err)
	}
	addTarget := func(name, p string) error {
		files, err := os.ReadDir(p)
		if err != nil {
			return fatalf("reading %s: %v", display(p), err)
		}
		var entries []*Entry
		for _, f := range files {
			if f.IsDir() {
				continue // go test ignores directories in a corpus too
			}
			fp := filepath.Join(p, f.Name())
			info, err := os.Stat(fp)
			if err != nil {
				return fatalf("reading %s: %v", display(fp), err)
			}
			entries = append(entries, &Entry{Target: name, Name: f.Name(), Path: display(fp), abs: fp, Size: info.Size(),
				inPackage: samePath(p, filepath.Join(pkgCorpus, name))})
		}
		if len(entries) == 0 {
			return nil
		}
		if !listed[name] {
			for _, e := range entries {
				e.Result = Untrusted
				e.Reason = fmt.Sprintf("%s has no fuzz target named %s, so this entry can't run (was the target renamed or removed, or is its file excluded by build tags?)", pkg.ImportPath, name)
				rep.Orphans = append(rep.Orphans, e)
			}
			return nil
		}
		rep.Targets = append(rep.Targets, &Target{Name: name, Entries: entries})
		return nil
	}
	if base := filepath.Base(corpusAbs); listed[base] {
		// -corpus points at one target's directory, such as a CI artifact.
		if runRE == nil || runRE.MatchString(base) {
			if err := addTarget(base, corpusAbs); err != nil {
				return nil, err
			}
		}
	} else {
		for _, it := range items {
			p := filepath.Join(corpusAbs, it.Name())
			if !it.IsDir() {
				rep.Orphans = append(rep.Orphans, &Entry{Name: it.Name(), Path: display(p), abs: p, Result: Untrusted,
					Reason: "a file directly in the corpus directory belongs to no fuzz target; corpus files go in a subdirectory named after the target"})
				continue
			}
			if runRE != nil && !runRE.MatchString(it.Name()) {
				continue
			}
			if err := addTarget(it.Name(), p); err != nil {
				return nil, err
			}
		}
	}
	n := len(rep.Orphans)
	for _, t := range rep.Targets {
		n += len(t.Entries)
	}
	if n == 0 {
		what := "fuzz targets"
		if runRE != nil {
			what = "fuzz targets matching -run"
		}
		return nil, fatalf("%s has no corpus entries for the package's %s", display(corpusAbs), what)
	}

	for _, t := range rep.Targets {
		r.triageTarget(t)
	}
	summarize(rep, c, pkgArg(c.dir))
	rep.Runs = r.runs
	return rep, nil
}

func samePath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

// display shows a path relative to the working directory when it is inside it.
func display(p string) string {
	wd, err := os.Getwd()
	if err != nil {
		return p
	}
	rel, err := filepath.Rel(wd, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return p
	}
	return filepath.ToSlash(rel)
}

func goList(dir string, c config) (*pkgInfo, error) {
	args := []string{"list", "-json"}
	if c.tags != "" {
		args = append(args, "-tags", c.tags)
	}
	args = append(args, ".")
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		return nil, fatalf("go list failed in %s: %s", display(dir), firstLines(errOut.String(), 5))
	}
	var p pkgInfo
	if err := json.Unmarshal(out.Bytes(), &p); err != nil {
		return nil, fatalf("go list printed something that isn't one package: %v", err)
	}
	if p.Error != nil {
		return nil, fatalf("go list: %s", p.Error.Err)
	}
	return &p, nil
}

func build(dir, tmp string, pkg *pkgInfo, c config) (string, error) {
	bin := filepath.Join(tmp, "fuzzsift.test")
	if os.PathSeparator == '\\' {
		bin += ".exe"
	}
	args := []string{"test", "-c", "-o", bin}
	if c.tags != "" {
		args = append(args, "-tags", c.tags)
	}
	if c.race {
		args = append(args, "-race")
	}
	args = append(args, ".")
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fatalf("%s failed to build, so no entry ran:\n%s", pkg.ImportPath, indent(firstLines(string(out), 12)))
	}
	if _, err := os.Stat(bin); err != nil {
		return "", fatalf("go test -c wrote no test binary for %s", pkg.ImportPath)
	}
	return bin, nil
}

func goroot() string {
	out, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = append(lines[:n], "...")
	}
	return strings.Join(lines, "\n")
}

func indent(s string) string {
	return "  " + strings.ReplaceAll(s, "\n", "\n  ")
}

// newMirror makes a directory tree in which the package directory holds
// links to every file of the real one, except testdata/fuzz, which is empty.
// The levels from the module root down are mirrored the same way, so tests
// that read ../ paths or look for go.mod still find them. The test binary
// runs there with one corpus entry at a time.
func newMirror(modRoot, pkgDir, dst string) (string, error) {
	rel, err := filepath.Rel(modRoot, pkgDir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		modRoot, rel = pkgDir, "."
	}
	var parts []string
	if rel != "." {
		parts = strings.Split(rel, string(filepath.Separator))
	}
	src := modRoot
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return "", err
	}
	for i := 0; ; i++ {
		next := "testdata"
		if i < len(parts) {
			next = parts[i]
		}
		if err := linkAllBut(src, dst, next); err != nil {
			return "", err
		}
		if i == len(parts) {
			break
		}
		src, dst = filepath.Join(src, next), filepath.Join(dst, next)
		if err := os.Mkdir(dst, 0o755); err != nil {
			return "", err
		}
	}
	td := filepath.Join(dst, "testdata")
	if err := os.Mkdir(td, 0o755); err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(src, "testdata")); err == nil {
		if err := linkAllBut(filepath.Join(src, "testdata"), td, "fuzz"); err != nil {
			return "", err
		}
	}
	if err := os.Mkdir(filepath.Join(td, "fuzz"), 0o755); err != nil {
		return "", err
	}
	return dst, nil
}

func linkAllBut(src, dst, skip string) error {
	items, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, it := range items {
		if it.Name() == skip {
			continue
		}
		if err := os.Symlink(filepath.Join(src, it.Name()), filepath.Join(dst, it.Name())); err != nil {
			return err
		}
	}
	return nil
}

type runner struct {
	bin     string
	dir     string
	timeout time.Duration
	ctx     frameContext
	runs    int
}

// exec runs the test binary in the mirror and returns its combined output.
func (r *runner) exec(args ...string) (out string, code int, killed bool, err error) {
	r.runs++
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout+30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.bin, args...)
	cmd.Dir = r.dir
	cmd.WaitDelay = 5 * time.Second
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err = cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return buf.String(), -1, true, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return buf.String(), ee.ExitCode(), false, nil
	}
	if err != nil {
		return buf.String(), -1, false, err
	}
	return buf.String(), 0, false, nil
}

var fuzzName = regexp.MustCompile(`^Fuzz[\p{L}\p{N}_]*$`)

func (r *runner) list() ([]string, error) {
	out, code, killed, err := r.exec("-test.list", "^Fuzz")
	if err != nil {
		return nil, fatalf("running the test binary: %v", err)
	}
	if killed || code != 0 {
		return nil, fatalf("listing the fuzz targets failed (exit %d), so the test binary can't be trusted:\n%s", code, indent(firstLines(out, 12)))
	}
	var names []string
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if fuzzName.MatchString(l) {
			names = append(names, l)
		}
	}
	return names, nil
}

func (r *runner) fuzzDir(target string) string {
	return filepath.Join(r.dir, "testdata", "fuzz", target)
}

func (r *runner) runTarget(target, name string) (string, int, bool, error) {
	pattern := "^" + regexp.QuoteMeta(target) + "$"
	if name != "" {
		pattern += "/" + runPattern(name)
	}
	return r.exec("-test.v", "-test.paniconexit0", "-test.count=1",
		"-test.timeout="+r.timeout.String(), "-test.run", pattern)
}

// precheck runs a target with an empty corpus directory. Its f.Add seeds
// and setup must pass, or no entry's result could be told apart from them.
func (r *runner) precheck(target string) string {
	out, code, killed, err := r.runTarget(target, "")
	if err != nil {
		return fmt.Sprintf("running %s: %v", target, err)
	}
	if killed {
		return fmt.Sprintf("%s didn't finish within %s with no corpus entries, so its entries can't be told apart from its own seeds", target, r.timeout)
	}
	lines := splitLines(out)
	pass := hasLine(lines, "--- PASS: "+target+" ")
	skip := hasLine(lines, "--- SKIP: "+target+" ")
	switch {
	case code == 0 && pass:
		return ""
	case code == 0 && skip:
		return fmt.Sprintf("%s skips itself even with no corpus entries, so its entries can't run", target)
	case code == 0:
		return fmt.Sprintf("%s exited 0 without a result line with no corpus entries (a TestMain that skips m.Run can do that), so its entries can't be checked", target)
	}
	msg := failureSummary(lines, target)
	return fmt.Sprintf("%s fails with no corpus entries (in its f.Add seeds or its setup), so a failing entry can't be told apart from that: %s", target, msg)
}

func failureSummary(lines []string, target string) string {
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "panic: ") || strings.HasPrefix(t, "fatal error: ") {
			return t
		}
	}
	var last string
	for _, l := range lines {
		if m := msgLine.FindStringSubmatch(l); m != nil {
			last = strings.TrimSpace(l)
		}
	}
	if last != "" {
		return last
	}
	return "exit with no message"
}

func (r *runner) triageTarget(t *Target) {
	if err := os.MkdirAll(r.fuzzDir(t.Name), 0o755); err != nil {
		t.Problem = err.Error()
	}
	if t.Problem == "" {
		t.Problem = r.precheck(t.Name)
	}
	if t.Problem != "" {
		for _, e := range t.Entries {
			e.Result = Untrusted
			e.Reason = "not run: " + t.Name + " can't be checked"
		}
		return
	}
	for _, e := range t.Entries {
		res, reason, f := r.runEntry(e)
		e.Result, e.Reason, e.Failure = res, reason, f
		if res != Fail {
			continue
		}
		res2, reason2, f2 := r.runEntry(e)
		switch {
		case res2 == Untrusted || res2 == Unreadable:
			e.Result, e.Reason = Untrusted, "the second run couldn't be read: "+reason2
		case res2 != Fail:
			e.Result = Flaky
			e.Reason = "failed on the first run and passed on the second"
			if res2 == Skip {
				e.Reason = "failed on the first run and skipped on the second"
			}
		case f2.Key != f.Key:
			e.Result = Flaky
			e.Second = f2
			e.Reason = "failed differently on the second run"
		}
	}
}

func (r *runner) runEntry(e *Entry) (string, string, *Failure) {
	data, err := os.ReadFile(e.abs)
	if err != nil {
		return Untrusted, "can't read the file: " + err.Error(), nil
	}
	dst := filepath.Join(r.fuzzDir(e.Target), e.Name)
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return Untrusted, "can't copy the file: " + err.Error(), nil
	}
	defer os.Remove(dst)
	out, code, killed, err := r.runTarget(e.Target, e.Name)
	if err != nil {
		return Untrusted, "running the test binary: " + err.Error(), nil
	}
	return Classify(e.Target, e.Name, out, code, killed, r.timeout, r.ctx)
}

// runPattern is a -test.run element that matches exactly one corpus file.
// go test names the subtest after the file as it is, but rewrites spaces in
// -run patterns to underscores, so spaces and other unprintable runes are
// written as \x{...} escapes.
func runPattern(name string) string {
	var b strings.Builder
	b.WriteString("^")
	for _, r := range regexp.QuoteMeta(name) {
		if unicode.IsSpace(r) || !strconv.IsPrint(r) {
			fmt.Fprintf(&b, `\x{%x}`, r)
			continue
		}
		b.WriteRune(r)
	}
	b.WriteString("$")
	return b.String()
}

func splitLines(s string) []string {
	return strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
}

func hasLine(lines []string, prefix string) bool {
	return indexLine(lines, prefix) >= 0
}

func indexLine(lines []string, prefix string) int {
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimLeft(l, " "), prefix) {
			return i
		}
	}
	return -1
}

var msgLine = regexp.MustCompile(`^ {4,}([^\s:]+\.go):(\d+): ?(.*)$`)

// Classify reads the output of one run of the test binary for one entry.
// It returns a result, a reason for anything that isn't a plain pass, and
// the failure for a failing entry. The output and the exit code must agree,
// or the run is untrusted.
func Classify(target, name, out string, code int, killed bool, timeout time.Duration, ctx frameContext) (string, string, *Failure) {
	lines := splitLines(out)
	full := target + "/" + name

	runAt := indexLine(lines, "=== RUN   "+full)
	// go test reads every corpus file before running any; a file it can't
	// read fails the target and no entry runs.
	for _, l := range lines {
		if runAt >= 0 {
			break
		}
		if i := strings.Index(l, `"testdata/fuzz/`+target+`/`); i >= 0 {
			msg := l[i:]
			if j := strings.Index(msg, `": `); j >= 0 {
				msg = msg[j+3:]
			}
			return Unreadable, "go test can't read it: " + printable(strings.TrimSpace(msg), 160), nil
		}
	}
	if runAt < 0 {
		if killed {
			return Untrusted, fmt.Sprintf("the test binary was killed after %s before the entry started", timeout+30*time.Second), nil
		}
		return Untrusted, fmt.Sprintf("the entry never ran (exit %d): %s", code, lastText(lines)), nil
	}
	rest := lines[runAt:]
	passAt := indexLine(rest, "--- PASS: "+full+" ")
	skipAt := indexLine(rest, "--- SKIP: "+full+" ")
	failAt := indexLine(rest, "--- FAIL: "+full+" ")

	if killed {
		return Fail, "", newFailure("hang", fmt.Sprintf("killed after %s with no timeout report", timeout+30*time.Second), "")
	}
	for i, l := range rest {
		switch {
		case strings.HasPrefix(l, "panic: "):
			if code == 0 {
				return Untrusted, "the output has a panic but the test binary exited 0", nil
			}
			msg := panicMessage(strings.TrimPrefix(l, "panic: "))
			if strings.HasPrefix(msg, "test timed out after ") {
				return Fail, "", newFailure("hang", "test timed out after "+timeout.String(), hangFrame(rest[i:], target, ctx))
			}
			return Fail, "", newFailure("panic", msg, topFrame(firstGoroutine(rest[i:]), ctx))
		case strings.HasPrefix(l, "fatal error: "):
			if code == 0 {
				return Untrusted, "the output has a fatal error but the test binary exited 0", nil
			}
			return Fail, "", newFailure("fatal", strings.TrimPrefix(l, "fatal error: "), topFrame(firstGoroutine(rest[i:]), ctx))
		case signalLine.MatchString(l):
			if code == 0 {
				return Untrusted, "the output has a signal but the test binary exited 0", nil
			}
			return Fail, "", newFailure("signal", l, topFrame(firstGoroutine(rest[i:]), ctx))
		}
	}
	if failAt >= 0 {
		if code == 0 {
			return Untrusted, "the output says the entry failed but the test binary exited 0", nil
		}
		msg, frame := lastMessage(rest[:failAt])
		if msg == "" && frame == "" {
			msg = "failed with no message"
		}
		return Fail, "", newFailure("error", msg, frame)
	}
	if passAt >= 0 {
		if code != 0 {
			return Untrusted, fmt.Sprintf("the entry passed but the test binary exited %d: %s", code, lastText(lines)), nil
		}
		return Pass, "", nil
	}
	if skipAt >= 0 {
		if code != 0 {
			return Untrusted, fmt.Sprintf("the entry was skipped but the test binary exited %d: %s", code, lastText(lines)), nil
		}
		msg, at := lastMessage(rest[:skipAt])
		switch {
		case msg == "" && at != "":
			msg = "skipped at " + at
		case msg == "":
			msg = "skipped"
		}
		return Skip, msg, nil
	}
	if code == 0 {
		return Untrusted, "the test binary exited 0 without a result for the entry, so it may never have finished", nil
	}
	// The process ended while the entry ran, with no panic and no result:
	// os.Exit or log.Fatal in the code under test.
	return Fail, "", newFailure("exit", fmt.Sprintf("exit status %d: %s", code, lastText(rest[1:])), "")
}

// printable escapes control and invalid bytes and shortens s to about max
// runes, so a binary corpus file doesn't garble the report.
func printable(s string, max int) string {
	var b strings.Builder
	for n := 0; len(s) > 0; n++ {
		if n == max {
			b.WriteString("...")
			break
		}
		r, size := utf8.DecodeRuneInString(s)
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&b, "\\x%02x", s[0])
		case !strconv.IsPrint(r):
			q := strconv.QuoteRune(r)
			b.WriteString(q[1 : len(q)-1])
		default:
			b.WriteRune(r)
		}
		s = s[size:]
	}
	return b.String()
}

var signalLine = regexp.MustCompile(`^SIG[A-Z]+: `)

func panicMessage(s string) string {
	for _, suffix := range []string{" [recovered, repanicked]", " [recovered]"} {
		s = strings.TrimSuffix(s, suffix)
	}
	return s
}

// lastText is the last line of output that says something.
func lastText(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		t := strings.TrimSpace(lines[i])
		if t == "" || t == "FAIL" || t == "PASS" || strings.HasPrefix(t, "=== ") || strings.HasPrefix(t, "--- ") {
			continue
		}
		return printable(t, 200)
	}
	return "no output"
}

// lastMessage is the last t.Log, t.Error or t.Fatal line before the result
// line, with its file and line. t.Fatal is always last; after t.Error, a
// later t.Log would be picked instead.
func lastMessage(lines []string) (string, string) {
	for i := len(lines) - 1; i >= 0; i-- {
		m := msgLine.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		msg := strings.TrimSpace(m[3])
		if msg == "" {
			// testify and others start the message on the next line.
			for _, l := range lines[i+1:] {
				if !strings.HasPrefix(l, "        ") {
					break
				}
				t := strings.TrimSpace(l)
				if strings.HasPrefix(t, "Error:") {
					msg = strings.TrimSpace(strings.TrimPrefix(t, "Error:"))
					break
				}
				if msg == "" && t != "" && !strings.HasPrefix(t, "Error Trace:") {
					msg = t
				}
			}
		}
		return msg, m[1] + ":" + m[2]
	}
	return "", ""
}

func newFailure(kind, msg, frame string) *Failure {
	msg = printable(strings.TrimSpace(msg), 200)
	return &Failure{Kind: kind, Message: msg, Frame: frame, Key: kind + "|" + Normalize(msg) + "|" + frame}
}

var (
	quoted = regexp.MustCompile(`"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|` + "`[^`]*`")
	number = regexp.MustCompile(`0x[0-9a-fA-F]+|[0-9]+`)
)

// Normalize removes the parts of a message that change with the input:
// quoted text and numbers.
func Normalize(msg string) string {
	msg = quoted.ReplaceAllString(msg, `"…"`)
	msg = number.ReplaceAllStringFunc(msg, func(n string) string {
		if strings.HasPrefix(n, "0x") {
			return "0x…"
		}
		return "N"
	})
	return msg
}

type frameContext struct {
	goroot  string
	modDir  string
	modPath string
}

type frame struct {
	fn   string
	file string
	line string
}

var goroutineHeader = regexp.MustCompile(`^goroutine \d+`)
var fileLine = regexp.MustCompile(`^\t(.+\.go|_testmain\.go):(\d+)`)

// goroutines splits a trace into goroutine blocks.
func goroutines(lines []string) [][]frame {
	var blocks [][]frame
	var cur []frame
	in := false
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if goroutineHeader.MatchString(l) {
			if in {
				blocks = append(blocks, cur)
			}
			cur, in = nil, true
			continue
		}
		if !in {
			continue
		}
		if strings.TrimSpace(l) == "" {
			blocks = append(blocks, cur)
			cur, in = nil, false
			continue
		}
		if strings.HasPrefix(l, "\t") || strings.HasPrefix(l, "created by ") {
			continue
		}
		if i+1 < len(lines) {
			if m := fileLine.FindStringSubmatch(lines[i+1]); m != nil {
				cur = append(cur, frame{fn: funcName(l), file: m[1], line: m[2]})
				i++
			}
		}
	}
	if in {
		blocks = append(blocks, cur)
	}
	return blocks
}

func firstGoroutine(lines []string) []frame {
	b := goroutines(lines)
	if len(b) == 0 {
		return nil
	}
	return b[0]
}

// hangFrame finds the goroutine running the fuzz function after a timeout.
func hangFrame(lines []string, target string, ctx frameContext) string {
	for _, b := range goroutines(lines) {
		for _, f := range b {
			if strings.Contains(f.fn, "."+target+".func") {
				return topFrame(b, ctx)
			}
		}
	}
	return ""
}

// funcName strips the argument list from a frame's function line.
func funcName(l string) string {
	l = strings.TrimSpace(l)
	if !strings.HasSuffix(l, ")") {
		return l
	}
	depth := 0
	for i := len(l) - 1; i >= 0; i-- {
		switch l[i] {
		case ')':
			depth++
		case '(':
			depth--
			if depth == 0 {
				return l[:i]
			}
		}
	}
	return l
}

// topFrame is the first frame outside the standard library and the
// testing harness, shown as "func (file:line)".
func topFrame(frames []frame, ctx frameContext) string {
	for _, f := range frames {
		if f.fn == "panic" || f.file == "_testmain.go" || isStd(f, ctx) {
			continue
		}
		return fmt.Sprintf("%s (%s:%s)", f.fn, shortFile(f.file, ctx), f.line)
	}
	return ""
}

// isStd reports whether a frame is in the standard library: its file is
// under GOROOT, or it is outside the module and its package path has no dot
// in the first element (runtime, testing, reflect). The second rule also
// covers -trimpath builds and traces from another Go installation.
func isStd(f frame, ctx frameContext) bool {
	file := filepath.ToSlash(f.file)
	if ctx.goroot != "" && strings.HasPrefix(file, filepath.ToSlash(ctx.goroot)+"/src/") {
		return true
	}
	if ctx.modDir != "" && strings.HasPrefix(file, filepath.ToSlash(ctx.modDir)+"/") {
		return false
	}
	if ctx.modPath != "" && strings.HasPrefix(file, ctx.modPath+"/") {
		return false
	}
	pkg := f.fn
	slash := strings.LastIndex(pkg, "/")
	if dot := strings.Index(pkg[slash+1:], "."); dot >= 0 {
		pkg = pkg[:slash+1+dot]
	}
	first := strings.SplitN(pkg, "/", 2)[0]
	return !strings.Contains(first, ".")
}

func shortFile(file string, ctx frameContext) string {
	f := filepath.ToSlash(file)
	if ctx.modDir != "" {
		root := filepath.ToSlash(ctx.modDir) + "/"
		if strings.HasPrefix(f, root) {
			return strings.TrimPrefix(f, root)
		}
	}
	if ctx.modPath != "" && strings.HasPrefix(f, ctx.modPath+"/") {
		return strings.TrimPrefix(f, ctx.modPath+"/")
	}
	if i := strings.Index(f, "/pkg/mod/"); i >= 0 {
		return f[i+len("/pkg/mod/"):]
	}
	return f
}

// summarize groups the failures and counts the results.
func summarize(rep *Report, c config, pkg string) {
	for _, t := range rep.Targets {
		groups := map[string]*Group{}
		var smallest = map[string]*Entry{}
		for _, e := range t.Entries {
			rep.Entries++
			switch e.Result {
			case Pass:
				rep.Passing++
			case Skip:
				rep.Skipped++
			case Flaky:
				rep.Flaky++
				rep.Failing++
			case Unreadable, Untrusted:
				rep.NotTriage++
			case Fail:
				rep.Failing++
				g := groups[e.Failure.Key]
				if g == nil {
					g = &Group{Target: t.Name, Kind: e.Failure.Kind, Frame: e.Failure.Frame, Key: e.Failure.Key}
					groups[e.Failure.Key] = g
					t.Groups = append(t.Groups, g)
				}
				g.Entries = append(g.Entries, e.Name)
				if s := smallest[g.Key]; s == nil || e.Size < s.Size || (e.Size == s.Size && e.Name < s.Name) {
					smallest[g.Key] = e
				}
			}
		}
		for _, g := range t.Groups {
			s := smallest[g.Key]
			g.Smallest = s.Name
			g.Message = s.Failure.Message
			g.Reproduce = reproduce(s, pkg, c)
			sort.Strings(g.Entries)
		}
		sort.SliceStable(t.Groups, func(i, j int) bool {
			if len(t.Groups[i].Entries) != len(t.Groups[j].Entries) {
				return len(t.Groups[i].Entries) > len(t.Groups[j].Entries)
			}
			return t.Groups[i].Key < t.Groups[j].Key
		})
		rep.Distinct += len(t.Groups)
	}
	rep.Entries += len(rep.Orphans)
	rep.NotTriage += len(rep.Orphans)
}

// pkgArg is the package argument for go test, as the user gave it.
func pkgArg(dir string) string {
	d := filepath.ToSlash(filepath.Clean(dir))
	if d == "." || filepath.IsAbs(dir) || strings.HasPrefix(d, "../") || d == ".." {
		return d
	}
	return "./" + d
}

func shellQuote(s string) string {
	if regexp.MustCompile(`^[A-Za-z0-9_./=+-]+$`).MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func reproduce(e *Entry, pkg string, c config) string {
	flags := ""
	if c.tags != "" {
		flags += " -tags " + shellQuote(c.tags)
	}
	if c.race {
		flags += " -race"
	}
	pattern := "^" + regexp.QuoteMeta(e.Target) + "$/" + runPattern(e.Name)
	cmd := fmt.Sprintf("go test%s -run=%s %s", flags, shellQuote(pattern), shellQuote(pkg))
	if !e.inPackage {
		dst := filepath.ToSlash(filepath.Join(filepath.FromSlash(pkg), "testdata", "fuzz", e.Target)) + "/"
		cmd = fmt.Sprintf("mkdir -p %s && cp %s %s && %s", shellQuote(dst), shellQuote(e.Path), shellQuote(dst), cmd)
	}
	return cmd
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

func writeText(w io.Writer, rep *Report, c config) {
	fmt.Fprintf(w, "fuzzsift: %s, %s, %s\n", rep.Package, plural(len(rep.Targets), "fuzz target", "fuzz targets"), plural(rep.Entries, "corpus entry", "corpus entries"))
	fmt.Fprintf(w, "Each entry ran alone with a %s timeout. Each failure ran twice.\n", rep.Timeout)

	var bad []*Entry
	bad = append(bad, rep.Orphans...)
	for _, t := range rep.Targets {
		for _, e := range t.Entries {
			if e.Result == Unreadable || e.Result == Untrusted {
				bad = append(bad, e)
			}
		}
	}
	if len(bad) > 0 {
		fmt.Fprintf(w, "\nNot triaged (%s):\n", plural(len(bad), "entry", "entries"))
		for _, t := range rep.Targets {
			if t.Problem != "" {
				fmt.Fprintf(w, "  %s: %s\n", t.Name, t.Problem)
			}
		}
		for _, e := range bad {
			if strings.HasPrefix(e.Reason, "not run: ") {
				continue
			}
			fmt.Fprintf(w, "  %s: %s\n", e.Path, e.Reason)
		}
		blocked := map[string]bool{}
		for _, e := range bad {
			if e.Result == Unreadable && e.inPackage {
				blocked[e.Target] = true
			}
		}
		for _, t := range rep.Targets {
			if blocked[t.Name] {
				fmt.Fprintf(w, "  While an unreadable file is in its testdata/fuzz directory, go test fails every %s entry before running it.\n", t.Name)
			}
		}
	}

	for _, t := range rep.Targets {
		if t.Problem != "" {
			continue
		}
		var pass, skip, flaky []*Entry
		fails := 0
		for _, e := range t.Entries {
			switch e.Result {
			case Pass:
				pass = append(pass, e)
			case Skip:
				skip = append(skip, e)
			case Flaky:
				flaky = append(flaky, e)
			case Fail:
				fails++
			}
		}
		var parts []string
		if fails > 0 {
			parts = append(parts, fmt.Sprintf("%d %s in %s", fails, verb(fails, "fails", "fail"), plural(len(t.Groups), "distinct way", "distinct ways")))
		}
		if len(flaky) > 0 {
			parts = append(parts, fmt.Sprintf("%d not repeatable", len(flaky)))
		}
		if len(pass) > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", len(pass), verb(len(pass), "passes", "pass")))
		}
		if len(skip) > 0 {
			parts = append(parts, fmt.Sprintf("%d skipped", len(skip)))
		}
		if nt := len(t.Entries) - fails - len(flaky) - len(pass) - len(skip); nt > 0 {
			parts = append(parts, fmt.Sprintf("%d not triaged", nt))
		}
		if len(parts) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n%s: %s, %s\n", t.Name, plural(len(t.Entries), "entry", "entries"), strings.Join(parts, ", "))
		for i, g := range t.Groups {
			fmt.Fprintf(w, "\n  %d. %s (%s)\n", i+1, describe(g.Kind, g.Message), plural(len(g.Entries), "entry", "entries"))
			if g.Frame != "" {
				fmt.Fprintf(w, "     at %s\n", g.Frame)
			}
			var s *Entry
			for _, e := range t.Entries {
				if e.Name == g.Smallest {
					s = e
				}
			}
			fmt.Fprintf(w, "     smallest: %s (%d bytes)\n", s.Path, s.Size)
			fmt.Fprintf(w, "     reproduce: %s\n", g.Reproduce)
			if len(g.Entries) > 1 {
				var others []string
				for _, n := range g.Entries {
					if n == g.Smallest {
						continue
					}
					if len(others) == 10 {
						others = append(others, fmt.Sprintf("and %d more", len(g.Entries)-11))
						break
					}
					others = append(others, n)
				}
				fmt.Fprintf(w, "     same failure: %s\n", strings.Join(others, ", "))
			}
		}
		if len(flaky) > 0 {
			fmt.Fprintf(w, "\n  Not repeatable:\n")
			for _, e := range flaky {
				fmt.Fprintf(w, "    %s: %s, first %s\n", e.Name, e.Reason, describe(e.Failure.Kind, e.Failure.Message))
			}
		}
		if len(pass)+len(skip) > 0 {
			fmt.Fprintln(w)
		}
		if len(pass) > 0 {
			fmt.Fprintf(w, "  Pass now: %s\n", names(pass))
		}
		if len(skip) > 0 {
			fmt.Fprintf(w, "  Skipped by the target: %s\n", names(skip))
		}
	}

	fmt.Fprintln(w)
	switch {
	case rep.NotTriage == 0 && rep.AllSkipped():
		fmt.Fprintf(w, "Incomplete: every entry was skipped by its target, so nothing was checked.\n")
	case !rep.Complete():
		fmt.Fprintf(w, "Incomplete: %d of %s %s not triaged, so this is not a full result.\n", rep.NotTriage, plural(rep.Entries, "entry", "entries"), verb(rep.NotTriage, "was", "were"))
		if rep.Failing > 0 {
			fmt.Fprintf(w, "Of the rest, %d %s in %s.\n", rep.Failing, verb(rep.Failing, "fails", "fail"), plural(rep.Distinct, "distinct way", "distinct ways"))
		}
	case rep.Failing > 0:
		line := fmt.Sprintf("%d of %s %s, in %s.", rep.Failing, plural(rep.Entries, "entry", "entries"), verb(rep.Failing, "fails", "fail"), plural(rep.Distinct, "distinct way", "distinct ways"))
		if rep.Flaky > 0 {
			line = fmt.Sprintf("%d of %s %s: %d in %s, %d not repeatable.", rep.Failing, plural(rep.Entries, "entry", "entries"), verb(rep.Failing, "fails", "fail"), rep.Failing-rep.Flaky, plural(rep.Distinct, "distinct way", "distinct ways"), rep.Flaky)
		}
		fmt.Fprintf(w, "%s %d %s. %d test binary runs.\n", line, rep.Passing, verb(rep.Passing, "passes", "pass"), rep.Runs)
	default:
		fmt.Fprintf(w, "No entry fails: %d of %d pass", rep.Passing, rep.Entries)
		if rep.Skipped > 0 {
			fmt.Fprintf(w, " and %d skipped", rep.Skipped)
		}
		fmt.Fprintf(w, ". %d test binary runs.\n", rep.Runs)
		fmt.Fprintln(w, "That covers these entries only. It doesn't show the code has no bugs.")
	}
}

func verb(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// names lists entry names, up to 10, for the text report. -json has all.
func names(es []*Entry) string {
	var n []string
	for i, e := range es {
		if i == 10 {
			n = append(n, fmt.Sprintf("and %d more", len(es)-10))
			break
		}
		n = append(n, e.Name)
	}
	return strings.Join(n, ", ")
}

func describe(kind, msg string) string {
	switch kind {
	case "panic":
		return "panic: " + msg
	case "fatal":
		return "fatal error: " + msg
	case "hang":
		return "hang: " + msg
	case "exit":
		return "process exit: " + msg
	case "error":
		return "test failure: " + msg
	}
	return msg
}
