package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The outputs in tests/testdata/runs are real: a test binary for a small
// package at /tmp/exp/q, built with Go 1.27.3 and Go 1.22.12, run once per
// corpus entry the way runner.runTarget runs it, with a 1s timeout.
var qctx = frameContext{modDir: "/tmp/exp/q", modPath: "example.com/q"}

func readRun(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "tests", "testdata", "runs", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestClassifyRealOutputs(t *testing.T) {
	cases := []struct {
		entry, result, kind, msg, frame string
		code                            int
	}{
		{"ok", Pass, "", "", "", 0},
		{"skip", Skip, "", "", "", 0},
		{"nilmap", Fail, "panic", "assignment to entry in nil map", "example.com/q.(*T).Set (q_test.go:14)", 2},
		{"fatal", Fail, "error", `bad value "fatal" at 7`, "q_test.go:33", 1},
		{"so", Fail, "fatal", "stack overflow", "example.com/q.rec (q_test.go:10)", 2},
		{"hang", Fail, "hang", "test timed out after 1s", "example.com/q.FuzzQ.func1 (q_test.go:21)", 2},
		{"exit0", Fail, "panic", "unexpected call to os.Exit(0) during test", "example.com/q.FuzzQ.func1 (q_test.go:28)", 2},
		{"logfatal", Fail, "exit", "exit status 1: ", "", 1},
	}
	for _, v := range []string{"go127", "go122"} {
		for _, c := range cases {
			name := c.entry + "." + v + ".exit" + string(rune('0'+c.code)) + ".txt"
			out := readRun(t, name)
			res, reason, f := Classify("FuzzQ", c.entry, out, c.code, false, time.Second, qctx)
			if res != c.result {
				t.Errorf("%s: result %s (%s), want %s", name, res, reason, c.result)
				continue
			}
			if c.result == Skip && reason != "not this one" {
				t.Errorf("%s: skip reason %q", name, reason)
			}
			if c.kind == "" {
				continue
			}
			if f.Kind != c.kind || !strings.HasPrefix(f.Message, c.msg) || f.Frame != c.frame {
				t.Errorf("%s: got %s %q at %q, want %s %q at %q", name, f.Kind, f.Message, f.Frame, c.kind, c.msg, c.frame)
			}
		}
	}
}

// Go 1.22 and Go 1.27 print a panic differently ([recovered] and a second
// panic line, or [recovered, repanicked]); the key must not change.
func TestKeysSameAcrossGoVersions(t *testing.T) {
	for _, e := range []string{"nilmap", "so", "hang", "fatal", "exit0"} {
		code := "2"
		if e == "fatal" {
			code = "1"
		}
		var keys []string
		for _, v := range []string{"go127", "go122"} {
			n, _ := (map[string]int{"1": 1, "2": 2})[code]
			_, _, f := Classify("FuzzQ", e, readRun(t, e+"."+v+".exit"+code+".txt"), n, false, time.Second, qctx)
			if f == nil {
				t.Fatalf("%s %s: no failure", e, v)
			}
			keys = append(keys, f.Key)
		}
		if keys[0] != keys[1] {
			t.Errorf("%s: keys differ: %q and %q", e, keys[0], keys[1])
		}
	}
}

// An output and exit code that disagree is never a pass or a failure.
func TestClassifyDisagreement(t *testing.T) {
	cases := []struct {
		file  string
		entry string
		code  int
	}{
		{"ok.go127.exit0.txt", "ok", 1},
		{"ok.go127.exit0.txt", "ok", 2},
		{"skip.go127.exit0.txt", "skip", 1},
		{"nilmap.go127.exit2.txt", "nilmap", 0},
		{"fatal.go127.exit1.txt", "fatal", 0},
		{"so.go127.exit2.txt", "so", 0},
		{"hang.go127.exit2.txt", "hang", 0},
		{"ok.go127.exit0.txt", "other", 0},
		{"nilmap.go127.exit2.txt", "other", 2},
	}
	for _, c := range cases {
		res, reason, _ := Classify("FuzzQ", c.entry, readRun(t, c.file), c.code, false, time.Second, qctx)
		if res != Untrusted {
			t.Errorf("%s as %s exit %d: %s (%s), want untrusted", c.file, c.entry, c.code, res, reason)
		}
	}
}

// Every prefix of a run's output, with exit code 0, must not pass unless
// it holds the entry's own PASS line, and must never fail or skip.
func TestCutOutputsNeverPass(t *testing.T) {
	for _, file := range []string{"ok.go127.exit0.txt", "nilmap.go127.exit2.txt", "fatal.go122.exit1.txt", "skip.go122.exit0.txt"} {
		out := readRun(t, file)
		entry := strings.SplitN(file, ".", 2)[0]
		for i := 0; i < len(out); i++ {
			cut := out[:i]
			res, _, _ := Classify("FuzzQ", entry, cut, 0, false, time.Second, qctx)
			hasPass := strings.Contains(cut, "--- PASS: FuzzQ/"+entry+" ")
			hasSkip := strings.Contains(cut, "--- SKIP: FuzzQ/"+entry+" ")
			switch {
			case res == Pass && !hasPass:
				t.Fatalf("%s cut at %d passed:\n%s", file, i, cut)
			case res == Skip && !hasSkip:
				t.Fatalf("%s cut at %d skipped:\n%s", file, i, cut)
			case res == Fail:
				t.Fatalf("%s cut at %d with exit 0 failed:\n%s", file, i, cut)
			}
		}
	}
}

func TestClassifyEmptyAndGarbage(t *testing.T) {
	for _, out := range []string{"", "\n", "\x00\xff\xfe garbage", "PASS\n", "ok  \texample.com/q\t0.01s\n", "--- PASS: FuzzQ (0.00s)\nPASS\n"} {
		for _, code := range []int{0, 1, 2} {
			res, _, _ := Classify("FuzzQ", "ok", out, code, false, time.Second, qctx)
			if res != Untrusted {
				t.Errorf("%q exit %d: %s, want untrusted", out, code, res)
			}
		}
	}
}

func TestClassifyUnreadable(t *testing.T) {
	out := "=== RUN   FuzzQ\n    q_test.go:7: \"testdata/fuzz/FuzzQ/bad\": unmarshal: cannot unmarshal empty string\n--- FAIL: FuzzQ (0.00s)\nFAIL\n"
	res, reason, _ := Classify("FuzzQ", "bad", out, 1, false, time.Second, qctx)
	if res != Unreadable || reason != "go test can't read it: unmarshal: cannot unmarshal empty string" {
		t.Fatalf("%s %q", res, reason)
	}
}

func TestClassifyKilled(t *testing.T) {
	out := "=== RUN   FuzzQ\n=== RUN   FuzzQ/x\n"
	res, _, f := Classify("FuzzQ", "x", out, -1, true, time.Second, qctx)
	if res != Fail || f.Kind != "hang" {
		t.Fatalf("%s %+v", res, f)
	}
	res, _, _ = Classify("FuzzQ", "x", "", -1, true, time.Second, qctx)
	if res != Untrusted {
		t.Fatalf("killed before the entry ran: %s", res)
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"runtime error: index out of range [3] with length 2": "runtime error: index out of range [N] with length N",
		`Parse(Format(1100)) = 1024, <nil>`:                   "Parse(Format(N)) = N, <nil>",
		`bad value "x\"y" at 0x1f`:                            `bad value "…" at 0x…`,
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFuncName(t *testing.T) {
	cases := map[string]string{
		"example.com/p.Parse(...)":                               "example.com/p.Parse",
		"example.com/p.FuzzParse.func1(0x1, {0x2, 0x3})":         "example.com/p.FuzzParse.func1",
		"example.com/q.(*T).Set(0x4000, {0x1, 0x2})":             "example.com/q.(*T).Set",
		"panic({0x3d2c60?, 0x75fa4eaf6030?})":                    "panic",
		"testing.tRunner.func1.2({0x3d2c60, 0x75fa4eaf6030})":    "testing.tRunner.func1.2",
		"github.com/a/b%2ec.(*x).y(...)":                         "github.com/a/b%2ec.(*x).y",
		"example.com/q.rec(0x1ffff39?)":                          "example.com/q.rec",
		"created by testing.(*F).Fuzz.func1 in goroutine 6":      "created by testing.(*F).Fuzz.func1 in goroutine 6",
		"example.com/p.Parse":                                    "example.com/p.Parse",
		"example.com/p.Parse[...](0x1)":                          "example.com/p.Parse[...]",
		"example.com/p.(*List[go.shape.int]).Push(0x1, 0x2)":     "example.com/p.(*List[go.shape.int]).Push",
		"example.com/p.FuzzX.func1(0x1, {0x2, 0x3}, {0x4, ...})": "example.com/p.FuzzX.func1",
	}
	for in, want := range cases {
		if got := funcName(in); got != want {
			t.Errorf("funcName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsStd(t *testing.T) {
	ctx := frameContext{goroot: "/usr/local/go", modDir: "/src/shop", modPath: "shop"}
	cases := []struct {
		fn, file string
		std      bool
	}{
		{"runtime.gopanic", "/usr/local/go/src/runtime/panic.go", true},
		{"testing.tRunner", "testing/testing.go", true},
		{"testing.tRunner", "/home/u/sdk/go1.22.12/src/testing/testing.go", true},
		{"shop/size.Parse", "/src/shop/size/size.go", false},
		{"shop/size.Parse", "shop/size/size.go", false},
		{"example.com/p.Parse", "example.com/p/p.go", false},
		{"github.com/x/y.F", "/home/u/go/pkg/mod/github.com/x/y@v1.0.0/y.go", false},
		{"strings.Repeat", "strings/strings.go", true},
	}
	for _, c := range cases {
		if got := isStd(frame{fn: c.fn, file: c.file}, ctx); got != c.std {
			t.Errorf("isStd(%s, %s) = %v", c.fn, c.file, got)
		}
	}
}

func TestRunPattern(t *testing.T) {
	cases := map[string]string{
		"8643a79bab153d1b": "^8643a79bab153d1b$",
		"a (copy)":         `^a\x{20}\(copy\)$`,
		"tab\there":        `^tab\x{9}here$`,
		"x.y+z":            `^x\.y\+z$`,
	}
	for in, want := range cases {
		if got := runPattern(in); got != want {
			t.Errorf("runPattern(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReproduce(t *testing.T) {
	e := &Entry{Target: "FuzzParse", Name: "56a4d1e974615dc0", Path: "ci/FuzzParse/56a4d1e974615dc0", inPackage: true}
	if got := reproduce(e, "./size", config{}); got != "go test -run='^FuzzParse$/^56a4d1e974615dc0$' ./size" {
		t.Errorf("in package: %s", got)
	}
	e.inPackage = false
	want := "mkdir -p size/testdata/fuzz/FuzzParse/ && cp ci/FuzzParse/56a4d1e974615dc0 size/testdata/fuzz/FuzzParse/ && go test -tags 'a b' -race -run='^FuzzParse$/^56a4d1e974615dc0$' ./size"
	if got := reproduce(e, "./size", config{tags: "a b", race: true}); got != want {
		t.Errorf("outside:\n%s\nwant\n%s", got, want)
	}
}

func TestPrintable(t *testing.T) {
	if got := printable("a\x00b\xffcé", 100); got != `a\x00b\xffcé` {
		t.Errorf("got %q", got)
	}
	if got := printable(strings.Repeat("x", 10), 4); got != "xxxx..." {
		t.Errorf("got %q", got)
	}
}

func TestPkgArg(t *testing.T) {
	cases := map[string]string{".": ".", "size": "./size", "./size/": "./size", "../x": "../x", "/abs/p": "/abs/p"}
	for in, want := range cases {
		if got := pkgArg(in); got != want {
			t.Errorf("pkgArg(%q) = %q, want %q", in, got, want)
		}
	}
}
