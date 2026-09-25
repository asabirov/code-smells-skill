package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func testBaseline() *Baseline {
	b := defaultBaseline()
	b.Tools = map[string]string{"definitions": definitions}
	b.Caps = map[string]map[string]map[string]int{production: {complexity: {"a.go:Big": 40}}}
	b.Aggregate = map[string]map[string]Aggregate{production: {complexity: {Violations: 1, Excess: 25}}}
	return b
}

func testScan(values map[string]int) *Scan {
	s := &Scan{Tools: map[string]string{"definitions": definitions}, Values: map[string]map[string]map[string]int{}}
	for id, v := range values {
		s.set(production, complexity, id, v)
	}
	return s
}

func TestCompare(t *testing.T) {
	cases := []struct {
		name   string
		values map[string]int
		want   []string
	}{
		{"within caps", map[string]int{"a.go:Big": 40, "a.go:Small": 15}, nil},
		{"shrunk", map[string]int{"a.go:Big": 30}, nil},
		{"grew by one", map[string]int{"a.go:Big": 41}, []string{"grew from 40 to 41", "the baseline allows"}},
		{"new item", map[string]int{"a.go:Big": 40, "b.go:New": 16}, []string{"b.go:New is 16, over the limit of 15", "the baseline allows"}},
		{"shrink pays for nothing", map[string]int{"a.go:Big": 20, "b.go:New": 16}, []string{"b.go:New is 16", "the baseline allows"}},
	}
	for _, c := range cases {
		problems, _ := compare(testBaseline(), testScan(c.values))
		if len(problems) != len(c.want) {
			t.Errorf("%s: problems %q, want %d", c.name, problems, len(c.want))
			continue
		}
		for i, w := range c.want {
			if !strings.Contains(problems[i], w) {
				t.Errorf("%s: problem %q lacks %q", c.name, problems[i], w)
			}
		}
	}
}

func TestCompareRefusesOtherTools(t *testing.T) {
	s := testScan(nil)
	s.Tools["gocognit"] = "v9"
	if problems, _ := compare(testBaseline(), s); len(problems) != 1 || !strings.Contains(problems[0], "gocognit v9") {
		t.Fatalf("problems %q, want one naming the tools", problems)
	}
}

func TestCapsForOnlyLowers(t *testing.T) {
	b := testBaseline()
	caps, agg := capsFor(b, testScan(map[string]int{"a.go:Big": 30, "c.go:Fine": 3}), true)
	if got := caps[production][complexity]; len(got) != 1 || got["a.go:Big"] != 30 {
		t.Fatalf("caps %v, want a.go:Big lowered to 30", got)
	}
	if got := agg[production][complexity]; got != (Aggregate{1, 15}) {
		t.Fatalf("aggregate %v", got)
	}
	caps, _ = capsFor(b, testScan(map[string]int{"a.go:Big": 12}), true)
	if len(caps[production][complexity]) != 0 {
		t.Fatalf("a fixed item kept its cap: %v", caps)
	}
}

func TestLooseningsNeedAnException(t *testing.T) {
	main := testBaseline()
	branch := testBaseline()
	branch.Caps[production][complexity]["a.go:Big"] = 41
	branch.Budgets[tests][functionLines] = 200
	want := []string{"caps/production/complexity/a.go:Big", "budgets/tests/function_lines"}
	if got := loosenings(branch, main, testScan(nil)); !slices.Equal(got, want) {
		t.Fatalf("loosenings %q, want %q", got, want)
	}
	if loosenings(main, branch, testScan(nil)) != nil {
		t.Fatal("a tighter baseline counted as looser")
	}
	branch.Exceptions = []Exception{{Key: want[0], Reason: "split in #1", Issue: ""}}
	if excepted(branch, want[0]) {
		t.Fatal("an exception without an issue was accepted")
	}
	branch.Exceptions[0].Issue = "asabirov/amberstone#1"
	if !excepted(branch, want[0]) {
		t.Fatal("a complete exception was refused")
	}
}

func TestCycles(t *testing.T) {
	graph := map[string]map[string]bool{
		"a": {"b": true}, "b": {"c": true}, "c": {"a": true}, "d": {"a": true, "x": true},
	}
	got := stronglyConnected(graph)
	if len(got) != 1 || !slices.Equal(got[0], []string{"a", "b", "c"}) {
		t.Fatalf("cycles %q", got)
	}
}

// TestMain points the TypeScript scan at the source folder, where make test installs
// the compiler.
func TestMain(m *testing.M) {
	dir, err := filepath.Abs("typescript")
	if err != nil {
		panic(err)
	}
	tsDir = dir
	os.Exit(m.Run())
}

// scanSources writes files into dir and scans them as one language.
func scanSources(t *testing.T, dir string, files map[string]string) (*Scan, error) {
	t.Helper()
	var paths []string
	for name, src := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	rules := defaultBaseline().Files
	if err := rules.compile(); err != nil {
		t.Fatal(err)
	}
	s := &Scan{Tools: map[string]string{"definitions": definitions}, Values: map[string]map[string]map[string]int{}}
	graph := map[string]map[string]bool{}
	if strings.HasSuffix(paths[0], ".go") {
		return s, scanGo(s, &rules, paths, graph)
	}
	_, err := scanTS(s, &rules, paths, graph)
	return s, err
}

func mustFail(t *testing.T, files map[string]string, want string) {
	t.Helper()
	_, err := scanSources(t, t.TempDir(), files)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("scan error %v, want one containing %q", err, want)
	}
}

func TestEmptyScanFails(t *testing.T) {
	s := &Scan{Values: map[string]map[string]map[string]int{}}
	rules := defaultBaseline().Files
	if err := scanGo(s, &rules, nil, nil); err == nil {
		t.Error("a Go scan of no files passed")
	}
	if _, err := scanTS(s, &rules, nil, nil); err == nil {
		t.Error("a TypeScript scan of no files passed")
	}
	mustFail(t, map[string]string{"a.go": "package a\n\nconst probe = 1\n"}, "no Go functions")
	mustFail(t, map[string]string{"a.ts": "export const probe = 1;\n"}, "no TypeScript functions")
}

func TestUnparsableFileFails(t *testing.T) {
	mustFail(t, map[string]string{"a.go": "package a\n\nfunc broken( {\n"}, "a.go")
	mustFail(t, map[string]string{"a.ts": "export function broken( {\n"}, "a.ts")
}

// repeat builds a function body of n copies of a statement.
func repeat(stmt string, n int) string {
	return strings.Repeat("\t"+stmt+"\n", n)
}

// TestNewDuplicateGroupFails replaces a baselined clone with a different one that has
// more copies: the removed group's cap is too small for it, so it must fail.
func TestNewDuplicateGroupFails(t *testing.T) {
	cases := []struct {
		name, ext, old, new string
	}{
		{"go", ".go",
			"package a\n\nfunc F(x int) {\n" + repeat("x = x + 1", 40) + "}\n",
			"package a\n\nfunc F() {\n" + repeat("report(nil)", 30) + "}\n"},
		{"ts", ".ts",
			"export function f(x: number) {\n" + repeat("x = x + 1;", 40) + "}\n",
			"export function f() {\n" + repeat("report(null);", 30) + "}\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			old, err := scanSources(t, dir, map[string]string{"a" + c.ext: c.old, "b" + c.ext: c.old})
			if err != nil {
				t.Fatal(err)
			}
			b := defaultBaseline()
			b.Tools = old.Tools
			b.Caps, b.Aggregate = capsFor(b, old, false)
			if problems, _ := compare(b, old); len(problems) != 0 {
				t.Fatalf("the baselined code fails: %q", problems)
			}
			now, err := scanSources(t, dir, map[string]string{"a" + c.ext: c.new, "b" + c.ext: c.new, "c" + c.ext: c.new})
			if err != nil {
				t.Fatal(err)
			}
			problems, _ := compare(b, now)
			if !strings.Contains(strings.Join(problems, "\n"), "duplicate_groups") {
				t.Fatalf("a new duplicate group passed; problems %q", problems)
			}
		})
	}
}

// TestFreedCapsGoBySize removes capped functions and adds others: each new one may take
// a removed one's cap that is not smaller than it, whatever its name or body.
func TestFreedCapsGoBySize(t *testing.T) {
	b := testBaseline()
	b.Caps[production][complexity]["a.go:Other"] = 30
	b.Aggregate[production][complexity] = Aggregate{Violations: 2, Excess: 40}
	cases := []struct {
		name   string
		values map[string]int
		want   string
	}{
		{"pure move", map[string]int{"b.go:Big": 40, "b.go:Other": 30}, ""},
		{"move and shrink", map[string]int{"b.go:Big": 35, "b.go:Other": 20}, ""},
		{"split to fit the freed caps", map[string]int{"b.go:One": 28, "b.go:Two": 35}, ""},
		{"growth", map[string]int{"b.go:Big": 41, "b.go:Other": 30}, "b.go:Big is 41, over the limit"},
		{"a cap still in use", map[string]int{"a.go:Big": 40, "a.go:Other": 30, "b.go:New": 20}, "b.go:New is 20, over the limit"},
	}
	for _, c := range cases {
		problems, _ := compare(b, testScan(c.values))
		if got := strings.Join(problems, "\n"); c.want == "" && got != "" || !strings.Contains(got, c.want) {
			t.Errorf("%s: problems %q, want %q", c.name, got, c.want)
		}
	}
	branch := testBaseline()
	branch.Caps[production][complexity] = map[string]int{"b.go:One": 28, "b.go:Two": 35}
	s := testScan(map[string]int{"b.go:One": 28, "b.go:Two": 35})
	if got := loosenings(branch, b, s); got != nil {
		t.Fatalf("caps that took freed ones counted as looser: %q", got)
	}
	branch.Caps[production][complexity]["b.go:Two"] = 41
	if got := loosenings(branch, b, s); len(got) != 1 {
		t.Fatalf("a cap larger than any freed one: loosenings %q", got)
	}
}

// TestFunctionLinesCountOnlyCode measures a function of 104 lines, 72 of them code, with
// a doc comment above: only the code lines count, so it stays under the limit of 80.
func TestFunctionLinesCountOnlyCode(t *testing.T) {
	body := strings.Repeat("\t// why the next line is here\n\tx = x + 1 // and a trailing note\n\n", 15) + repeat("x = x + 1", 55)
	cases := []struct{ name, src string }{
		{"a.go", "package a\n\n// F is long.\nfunc F(x int) {\n" + body + "\t/* the end\n\t   of it */\n}\n"},
		{"a.ts", "/** f is long. */\nexport function f(x: number) {\n" + body + "\t/* the end\n\t   of it */\n}\n"},
	}
	for _, c := range cases {
		s, err := scanSources(t, t.TempDir(), map[string]string{c.name: c.src})
		if err != nil {
			t.Fatal(err)
		}
		for id, v := range s.Values[production][functionLines] {
			if v != 72 {
				t.Errorf("%s is %d lines, want 72", id, v)
			}
		}
	}
}
