// Package tests runs fuzzsift the way the skill and the README do: as a
// single Go file, on the example module and on throwaway packages built here.
package tests

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

var bin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fuzzsift-bin")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "fuzzsift")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	// Build the single file, as the skill runs it.
	out, err := exec.Command("go", "build", "-o", bin, "../plugin/scripts/fuzzsift.go").CombinedOutput()
	if err != nil {
		panic(string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// fuzzsift runs the tool in dir (the repository root when dir is "").
func fuzzsiftIn(t *testing.T, dir string, env []string, args ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	if dir == "" {
		cmd.Dir = ".."
	}
	cmd.Env = append(os.Environ(), env...)
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return code, out.String(), errOut.String()
}

func fuzzsift(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	return fuzzsiftIn(t, "", nil, args...)
}

// module writes a throwaway Go module and returns its directory.
func module(t *testing.T, path string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files["go.mod"] = "module " + path + "\n\ngo 1.22\n"
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// entry is a corpus file holding one string.
func entry(s string) string {
	return fmt.Sprintf("go test fuzz v1\nstring(%q)\n", s)
}

// copyExamples copies examples/config to a temporary directory.
func copyExamples(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	src := filepath.Join("..", "examples", "config")
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

type report struct {
	Entries    int
	Failing    int
	Distinct   int
	Passing    int
	Skipped    int
	Flaky      int
	NotTriaged int
	Targets    []struct {
		Name    string
		Problem string
		Entries []struct {
			Name   string
			Result string
			Reason string
		}
		Groups []struct {
			Kind     string
			Message  string
			Frame    string
			Entries  []string
			Smallest string
		}
	}
}

func jsonReport(t *testing.T, dir string, env []string, args ...string) (int, report) {
	t.Helper()
	code, out, errOut := fuzzsiftIn(t, dir, env, append([]string{"-json"}, args...)...)
	var rep report
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("%v: exit %d: %v\n%s%s", args, code, err, out, errOut)
	}
	return code, rep
}

// readmeExample is one `$ command` in the README and the output under it.
type readmeExample struct {
	line int
	cmd  string
	want string
}

func readmeExamples(t *testing.T) []readmeExample {
	f, err := os.Open("../README.md")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	var examples []readmeExample
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "    $ ") {
			continue
		}
		ex := readmeExample{line: i + 1, cmd: strings.TrimPrefix(lines[i], "    $ ")}
		var out []string
		j := i + 1
		for ; j < len(lines); j++ {
			l := lines[j]
			if strings.HasPrefix(l, "    $ ") || (l != "" && !strings.HasPrefix(l, "    ")) {
				break
			}
			out = append(out, strings.TrimPrefix(l, "    "))
		}
		for len(out) > 0 && out[len(out)-1] == "" {
			out = out[:len(out)-1]
		}
		ex.want = strings.Join(out, "\n")
		examples = append(examples, ex)
		i = j - 1
	}
	return examples
}

// TestReadmeExamples runs every `$ ` line in the README from the repository
// root and checks that the output shown under it is what the command prints.
func TestReadmeExamples(t *testing.T) {
	examples := readmeExamples(t)
	if len(examples) < 4 {
		t.Fatalf("only %d README examples found", len(examples))
	}
	for _, ex := range examples {
		t.Run(fmt.Sprintf("line%d", ex.line), func(t *testing.T) {
			cmd := exec.Command("sh", "-c", ex.cmd)
			cmd.Dir = ".."
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("README line %d: %s: %v\n%s", ex.line, ex.cmd, err, out)
			}
			got := strings.TrimRight(string(out), "\n")
			if got != ex.want {
				t.Fatalf("README line %d: %s\n--- README\n%s\n--- actual\n%s", ex.line, ex.cmd, ex.want, got)
			}
		})
	}
}

func TestExampleExitCodes(t *testing.T) {
	cases := []struct {
		args []string
		code int
	}{
		{[]string{"examples/config/size"}, 0},
		{[]string{"-fail", "examples/config/size"}, 1},
		{[]string{"examples/config/addr"}, 0},
		{[]string{"-fail", "examples/config/addr"}, 0},
		{[]string{"-fail", "-corpus", "examples/config/size/testdata/fuzz/FuzzParse", "examples/config/size"}, 1},
		{[]string{"-fail", "-corpus", "examples/config/addr/testdata/fuzz", "examples/config/addr"}, 0},
	}
	for _, c := range cases {
		code, out, errOut := fuzzsift(t, c.args...)
		if code != c.code {
			t.Errorf("%v: exit %d, want %d\n%s%s", c.args, code, c.code, out, errOut)
		}
	}
}

func TestSizeGroups(t *testing.T) {
	code, rep := jsonReport(t, "", nil, "examples/config/size")
	if code != 0 || rep.Entries != 7 || rep.Failing != 5 || rep.Distinct != 3 || rep.Passing != 2 || rep.NotTriaged != 0 {
		t.Fatalf("exit %d: %+v", code, rep)
	}
	g := rep.Targets[0].Groups
	if g[0].Kind != "error" || len(g[0].Entries) != 3 || g[0].Smallest != "56a4d1e974615dc0" || g[0].Frame != "size_test.go:27" {
		t.Errorf("round trip group: %+v", g[0])
	}
	frames := []string{g[1].Frame, g[2].Frame}
	sort.Strings(frames)
	if frames[0] != "example.com/config/size.Parse (size/size.go:22)" || frames[1] != "example.com/config/size.Parse (size/size.go:24)" {
		t.Errorf("panic frames: %v", frames)
	}
}

// The healthy package stays quiet, and the report doesn't call it bug-free.
func TestHealthyPackageStaysQuiet(t *testing.T) {
	code, out, _ := fuzzsift(t, "-fail", "examples/config/addr")
	if code != 0 || !strings.Contains(out, "No entry fails: 5 of 5 pass.") || !strings.Contains(out, "doesn't show the code has no bugs") {
		t.Fatalf("exit %d\n%s", code, out)
	}
	for _, bad := range []string{"fail in", "Not triaged", "Incomplete"} {
		if strings.Contains(out, bad) {
			t.Errorf("healthy report has %q:\n%s", bad, out)
		}
	}
}

// A built-in -trimpath changes the paths in stack traces. The frames must
// stay the same.
func TestTrimpath(t *testing.T) {
	_, plain := jsonReport(t, "", nil, "examples/config/size")
	_, trimmed := jsonReport(t, "", []string{"GOFLAGS=-trimpath"}, "examples/config/size")
	a, _ := json.Marshal(plain.Targets[0].Groups)
	b, _ := json.Marshal(trimmed.Targets[0].Groups)
	if string(a) != string(b) {
		t.Fatalf("groups differ with -trimpath:\n%s\n%s", a, b)
	}
}

func badFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("testdata/bad/*")
	if err != nil || len(files) < 10 {
		t.Fatalf("bad inputs: %v %v", files, err)
	}
	return files
}

// mustRefuse checks a run that can't be trusted: exit 2, with and without
// -fail, and no line that reads like a clean result.
func mustRefuse(t *testing.T, dir string, args ...string) string {
	t.Helper()
	var first string
	for _, extra := range [][]string{nil, {"-fail"}} {
		code, out, errOut := fuzzsiftIn(t, dir, nil, append(extra, args...)...)
		if code != 2 {
			t.Errorf("%v %v: exit %d, want 2\n%s%s", extra, args, code, out, errOut)
		}
		if strings.Contains(out, "No entry fails") || strings.Contains(out, "doesn't show the code has no bugs") {
			t.Errorf("%v %v: reads like a clean result:\n%s", extra, args, out)
		}
		if out != "" && !strings.Contains(out, "Incomplete:") {
			t.Errorf("%v %v: a report without the Incomplete line:\n%s", extra, args, out)
		}
		if out == "" && !strings.Contains(errOut, "nothing was triaged") {
			t.Errorf("%v %v: no report and no reason:\n%s", extra, args, errOut)
		}
		first = out + errOut
	}
	return first
}

// Each bad file goes into a real corpus: the healthy one, where every
// other entry passes, and the failing one.
func TestBadEntries(t *testing.T) {
	for _, f := range badFiles(t) {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		name := "bad-" + filepath.Base(f)
		for _, pkg := range []string{"addr/testdata/fuzz/FuzzSplit", "size/testdata/fuzz/FuzzParse"} {
			t.Run(name+"/"+strings.SplitN(pkg, "/", 2)[0], func(t *testing.T) {
				dir := copyExamples(t)
				if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(pkg), name), data, 0o644); err != nil {
					t.Fatal(err)
				}
				out := mustRefuse(t, dir, "./"+strings.SplitN(pkg, "/", 2)[0])
				if !strings.Contains(out, name+": go test can't read it") {
					t.Errorf("the bad file isn't named:\n%s", out)
				}
			})
		}
	}
}

// The same files given with -corpus, next to a real entry.
func TestBadEntriesFromCorpusFlag(t *testing.T) {
	for _, f := range badFiles(t) {
		t.Run(filepath.Base(f), func(t *testing.T) {
			ci := filepath.Join(t.TempDir(), "FuzzSplit")
			os.MkdirAll(ci, 0o755)
			data, _ := os.ReadFile(f)
			os.WriteFile(filepath.Join(ci, "bad"), data, 0o644)
			good, _ := os.ReadFile("../examples/config/addr/testdata/fuzz/FuzzSplit/771e938e4458e983")
			os.WriteFile(filepath.Join(ci, "771e938e4458e983"), good, 0o644)
			mustRefuse(t, "", "-corpus", ci, "examples/config/addr")
			mustRefuse(t, "", "-corpus", filepath.Dir(ci), "examples/config/addr")
		})
	}
}

func TestBadCorpusDirs(t *testing.T) {
	tmp := t.TempDir()
	empty := filepath.Join(tmp, "empty")
	os.MkdirAll(empty, 0o755)
	emptyTarget := filepath.Join(tmp, "emptytarget", "FuzzSplit")
	os.MkdirAll(emptyTarget, 0o755)
	file := filepath.Join(tmp, "file")
	os.WriteFile(file, []byte(entry(":80")), 0o644)
	orphan := filepath.Join(tmp, "orphan", "FuzzRenamed")
	os.MkdirAll(orphan, 0o755)
	os.WriteFile(filepath.Join(orphan, "e1"), []byte(entry(":80")), 0o644)
	loose := filepath.Join(tmp, "loose")
	os.MkdirAll(loose, 0o755)
	os.WriteFile(filepath.Join(loose, "e1"), []byte(entry(":80")), 0o644)
	cases := [][]string{
		{"-corpus", filepath.Join(tmp, "missing"), "examples/config/addr"},
		{"-corpus", empty, "examples/config/addr"},
		{"-corpus", filepath.Dir(emptyTarget), "examples/config/addr"},
		{"-corpus", emptyTarget, "examples/config/addr"},
		{"-corpus", file, "examples/config/addr"},
		{"-corpus", filepath.Dir(orphan), "examples/config/addr"},
		{"-corpus", loose, "examples/config/addr"},
		{"-run", "FuzzNope", "examples/config/addr"},
		{"-run", "[", "examples/config/addr"},
		{"examples/config/missing"},
		{"examples/config/addr/addr.go"},
		{"examples/config/addr", "examples/config/size"},
		{"-timeout", "0s", "examples/config/addr"},
	}
	for _, c := range cases {
		mustRefuse(t, "", c...)
	}
}

const sizeTest = `package size

import "testing"

func FuzzParse(f *testing.F) {
	f.Fuzz(func(t *testing.T, s string) {
		if s == "bad" {
			t.Fatal("bad input")
		}
	})
}
`

// Packages that can't be trusted, each with a real corpus entry.
func TestBadPackages(t *testing.T) {
	corpus := map[string]string{
		"testdata/fuzz/FuzzParse/e1": entry("ok"),
		"testdata/fuzz/FuzzParse/e2": entry("bad"),
	}
	with := func(files map[string]string) map[string]string {
		for k, v := range corpus {
			files[k] = v
		}
		return files
	}
	cases := map[string]map[string]string{
		"build fails": with(map[string]string{
			"size.go":      "package size\n\nfunc F() int { return \"x\" }\n",
			"size_test.go": sizeTest,
		}),
		"test file fails to build": with(map[string]string{
			"size.go":      "package size\n",
			"size_test.go": sizeTest + "\nvar _ int = \"x\"\n",
		}),
		"no test files": with(map[string]string{
			"size.go": "package size\n",
		}),
		"no fuzz targets": with(map[string]string{
			"size.go":      "package size\n",
			"size_test.go": "package size\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n",
		}),
		"TestMain skips m.Run": with(map[string]string{
			"size.go":      "package size\n",
			"size_test.go": sizeTest + "\nfunc TestMain(m *testing.M) {}\n",
		}),
		"TestMain hides failures": with(map[string]string{
			"size.go":      "package size\n",
			"size_test.go": sizeTest + "\nfunc TestMain(m *testing.M) { m.Run(); os.Exit(0) }\n",
		}),
		"TestMain always exits 1": with(map[string]string{
			"size.go":      "package size\n",
			"size_test.go": sizeTest + "\nfunc TestMain(m *testing.M) { m.Run(); os.Exit(1) }\n",
		}),
		"init panics": with(map[string]string{
			"size.go":      "package size\n\nfunc init() { panic(\"no config\") }\n",
			"size_test.go": sizeTest,
		}),
		"seed fails": with(map[string]string{
			"size.go": "package size\n",
			"size_test.go": `package size

import "testing"

func FuzzParse(f *testing.F) {
	f.Add("bad")
	f.Fuzz(func(t *testing.T, s string) {
		if s == "bad" {
			panic("bad input")
		}
	})
}
`,
		}),
		"target skips itself": with(map[string]string{
			"size.go":      "package size\n",
			"size_test.go": "package size\n\nimport \"testing\"\n\nfunc FuzzParse(f *testing.F) {\n\tf.Skip(\"needs a database\")\n\tf.Fuzz(func(t *testing.T, s string) {})\n}\n",
		}),
		"every entry skips": {
			"size.go":                    "package size\n",
			"size_test.go":               "package size\n\nimport \"testing\"\n\nfunc FuzzParse(f *testing.F) {\n\tf.Fuzz(func(t *testing.T, s string) {\n\t\tif s != \"\" {\n\t\t\tt.Skip(\"needs bash\")\n\t\t}\n\t})\n}\n",
			"testdata/fuzz/FuzzParse/e1": entry("ok"),
			"testdata/fuzz/FuzzParse/e2": entry("bad"),
		},
		"corpus of a renamed target": {
			"size.go":                    "package size\n",
			"size_test.go":               sizeTest,
			"testdata/fuzz/FuzzOld/e1":   entry("ok"),
			"testdata/fuzz/FuzzParse/e1": entry("ok"),
		},
		"fuzz test excluded by build tags": with(map[string]string{
			"size.go":      "package size\n",
			"size_test.go": "//go:build integration\n\n" + sizeTest,
			"a_test.go":    "package size\n\nimport \"testing\"\n\nfunc FuzzOther(f *testing.F) { f.Fuzz(func(t *testing.T, s string) {}) }\n",
		}),
		"no corpus": {
			"size.go":      "package size\n",
			"size_test.go": sizeTest,
		},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			if strings.Contains(name, "TestMain") {
				files["size_test.go"] = strings.Replace(files["size_test.go"], `import "testing"`, "import (\n\t\"os\"\n\t\"testing\"\n)\n\nvar _ = os.Exit", 1)
			}
			dir := module(t, "example.com/size", files)
			mustRefuse(t, dir, ".")
		})
	}
}

// One package with each kind of failure, found in real runs.
func TestKinds(t *testing.T) {
	files := map[string]string{
		"q.go": "package q\n\nfunc rec(n int) int { return rec(n+1) + 1 }\n",
		"q_test.go": `package q

import (
	"log"
	"os"
	"testing"
	"time"
)

type T struct{ m map[string]int }

func (t *T) Set(k string) { t.m[k] = 1 }

func FuzzQ(f *testing.F) {
	f.Add("seed")
	f.Fuzz(func(t *testing.T, s string) {
		switch s {
		case "hang":
			for {
				time.Sleep(time.Millisecond)
			}
		case "so":
			rec(0)
		case "logfatal":
			log.Fatal("boom")
		case "exit0":
			os.Exit(0)
		case "skip":
			t.Skip("not this one")
		case "fatal", "fatal2":
			t.Fatalf("bad value %q", s)
		case "nilmap", "nilmap2":
			(&T{}).Set(s)
		}
	})
}
`,
	}
	names := []string{"hang", "so", "logfatal", "exit0", "skip", "fatal", "fatal2", "nilmap", "nilmap2", "ok"}
	for _, n := range names {
		files["testdata/fuzz/FuzzQ/"+n] = entry(n)
	}
	files["testdata/fuzz/FuzzQ/nilmap2"] = entry("nilmap2") + "\n" // a bigger file
	dir := module(t, "example.com/q", files)
	code, rep := jsonReport(t, dir, nil, "-timeout", "2s", ".")
	if code != 0 || rep.Failing != 8 || rep.Distinct != 6 || rep.Passing != 1 || rep.Skipped != 1 || rep.NotTriaged != 0 {
		t.Fatalf("exit %d: %+v", code, rep)
	}
	kinds := map[string]string{}
	for _, g := range rep.Targets[0].Groups {
		kinds[g.Smallest] = g.Kind + " " + g.Frame
	}
	want := map[string]string{
		"hang":     "hang example.com/q.FuzzQ.func1 (q_test.go:20)",
		"so":       "fatal example.com/q.rec (q.go:3)",
		"logfatal": "exit ",
		"exit0":    "panic example.com/q.FuzzQ.func1 (q_test.go:27)",
		"fatal":    "error q_test.go:31",
		"nilmap":   "panic example.com/q.(*T).Set (q_test.go:12)",
	}
	for k, v := range want {
		if kinds[k] != v {
			t.Errorf("%s: %q, want %q", k, kinds[k], v)
		}
	}
	code, _, _ = fuzzsiftIn(t, dir, nil, "-fail", "-timeout", "2s", "-run", "FuzzQ", ".")
	if code != 1 {
		t.Errorf("-fail: exit %d", code)
	}
}

// An entry that fails once and then passes is reported as not repeatable.
func TestFlaky(t *testing.T) {
	mark := filepath.Join(t.TempDir(), "mark")
	dir := module(t, "example.com/flaky", map[string]string{
		"f_test.go": `package flaky

import (
	"os"
	"testing"
)

func FuzzOnce(f *testing.F) {
	f.Fuzz(func(t *testing.T, s string) {
		if s != "once" {
			return
		}
		if _, err := os.Stat(os.Getenv("FLAKY_MARK")); err != nil {
			os.WriteFile(os.Getenv("FLAKY_MARK"), nil, 0o644)
			panic("first time")
		}
	})
}
`,
		"testdata/fuzz/FuzzOnce/once":  entry("once"),
		"testdata/fuzz/FuzzOnce/other": entry("other"),
	})
	env := []string{"FLAKY_MARK=" + mark}
	code, rep := jsonReport(t, dir, env, ".")
	if code != 0 || rep.Flaky != 1 || rep.Failing != 1 || rep.Distinct != 0 || rep.Passing != 1 {
		t.Fatalf("exit %d: %+v", code, rep)
	}
	os.Remove(mark)
	code, out, _ := fuzzsiftIn(t, dir, env, "-fail", ".")
	if code != 1 || !strings.Contains(out, "once: failed on the first run and passed on the second") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

// Entries run in a mirror of the package directory. Relative paths into
// testdata and the parent directory, and a walk up to go.mod, must work,
// and the real package directory must not change.
func TestMirrorKeepsPaths(t *testing.T) {
	dir := module(t, "example.com/m", map[string]string{
		"shared/words.txt":         "apple\n",
		"pkg/testdata/golden.txt":  "golden\n",
		"pkg/testdata/fuzz/README": "",
		"pkg/p_test.go": `package pkg

import (
	"os"
	"path/filepath"
	"testing"
)

func FuzzPaths(f *testing.F) {
	f.Fuzz(func(t *testing.T, s string) {
		for _, p := range []string{"testdata/golden.txt", "../shared/words.txt", "../go.mod"} {
			if _, err := os.ReadFile(p); err != nil {
				t.Fatal(err)
			}
		}
		wd, _ := os.Getwd()
		if filepath.Base(wd) != "pkg" {
			t.Fatalf("working directory %s", wd)
		}
		os.WriteFile("scratch.txt", []byte(s), 0o644)
	})
}
`,
		"pkg/testdata/fuzz/FuzzPaths/e1": entry("one"),
		"pkg/testdata/fuzz/FuzzPaths/e2": entry("two"),
	})
	// testdata/fuzz/README is a file directly in the corpus directory.
	os.Remove(filepath.Join(dir, "pkg/testdata/fuzz/README"))
	before := listFiles(t, dir)
	code, out, errOut := fuzzsiftIn(t, filepath.Join(dir, "pkg"), nil, "-fail", ".")
	if code != 0 || !strings.Contains(out, "No entry fails: 2 of 2 pass.") {
		t.Fatalf("exit %d\n%s%s", code, out, errOut)
	}
	if after := listFiles(t, dir); after != before {
		t.Fatalf("the package directory changed:\n%s\n---\n%s", before, after)
	}
}

func listFiles(t *testing.T, dir string) string {
	var files []string
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil {
			files = append(files, p)
		}
		return nil
	})
	return strings.Join(files, "\n")
}

// go test stops at the first panicking entry. fuzzsift must reach the rest.
func TestPanicsDontHideLaterEntries(t *testing.T) {
	files := map[string]string{
		"p_test.go": `package p

import "testing"

func FuzzP(f *testing.F) {
	f.Fuzz(func(t *testing.T, s string) {
		var a []int
		switch s {
		case "a":
			_ = a[1]
		case "b":
			var m map[string]int
			m[s] = 1
		case "c":
			panic(s)
		}
	})
}
`,
	}
	for _, n := range []string{"a", "b", "c", "d"} {
		files["testdata/fuzz/FuzzP/"+n] = entry(n)
	}
	// go test rewrites spaces in -run patterns, and fuzzsift must still select it.
	files["testdata/fuzz/FuzzP/a (copy)"] = entry("a")
	dir := module(t, "example.com/p", files)
	code, rep := jsonReport(t, dir, nil, ".")
	if code != 0 || rep.Failing != 4 || rep.Distinct != 3 || rep.Passing != 1 || rep.NotTriaged != 0 {
		t.Fatalf("exit %d: %+v", code, rep)
	}
}

// A target whose own f.Add seed hangs can't have its entries judged.
func TestSeedHangs(t *testing.T) {
	dir := module(t, "example.com/hang", map[string]string{
		"h_test.go": `package hang

import (
	"testing"
	"time"
)

func FuzzH(f *testing.F) {
	f.Add("seed")
	f.Fuzz(func(t *testing.T, s string) {
		if s == "seed" {
			time.Sleep(time.Hour)
		}
	})
}
`,
		"testdata/fuzz/FuzzH/e1": entry("ok"),
	})
	out := mustRefuse(t, dir, "-timeout", "1s", ".")
	if !strings.Contains(out, "FuzzH fails with no corpus entries") || !strings.Contains(out, "test timed out") {
		t.Fatalf("%s", out)
	}
}
