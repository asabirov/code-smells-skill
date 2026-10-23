package main

import (
	"fmt"
	"maps"
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
	_, notes := compare(b, testScan(map[string]int{"b.go:Other": 30}))
	if got := strings.Join(notes, "\n"); !strings.Contains(got, "a.go:Big is gone; smells baseline drops its cap of 40") {
		t.Errorf("a freed cap nothing took: notes %q", got)
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

func TestShrunkCaps(t *testing.T) {
	for _, c := range []struct {
		name           string
		source, target int
		pass           bool
	}{
		{"below", 14, 40, true},
		{"at limit", 15, 40, true},
		{"still above", 16, 20, false},
		{"larger recipient", 15, 41, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := testBaseline()
			s := testScan(map[string]int{"a.go:Big": c.source, "b.go:New": c.target})
			problems, _ := compare(b, s)
			if (len(problems) == 0) != c.pass {
				t.Fatalf("problems %q, want pass %v", problems, c.pass)
			}
			branch := testBaseline()
			branch.Caps[production][complexity] = map[string]int{"b.go:New": c.target}
			if got := loosenings(branch, b, s); (len(got) == 0) != c.pass {
				t.Fatalf("loosenings %q, want pass %v", got, c.pass)
			}
		})
	}
	b := testBaseline()
	s := testScan(map[string]int{"a.go:Big": 15, "b.go:New": 40})
	branch := testBaseline()
	branch.Caps[production][complexity]["b.go:New"] = 40
	if got := loosenings(branch, b, s); len(got) == 0 {
		t.Fatal("keeping the freed cap while adding another passed")
	}
}

func TestShrunkCapChain(t *testing.T) {
	original, b := testBaseline(), testBaseline()
	values := map[string]int{"a.go:Big": 40}
	previous := "a.go:Big"
	for i, size := range []int{35, 30, 20, 16} {
		id := fmt.Sprintf("b.go:Step%d", i)
		values[previous], values[id] = 15, size
		s := testScan(values)
		if problems, _ := compare(b, s); len(problems) != 0 {
			t.Fatalf("step %d: %q", i, problems)
		}
		next := testBaseline()
		next.Caps, next.Aggregate = capsFor(b, s, true)
		if got := loosenings(next, b, s); len(got) != 0 {
			t.Fatalf("step %d loosens prior baseline: %q", i, got)
		}
		if got := loosenings(next, original, s); len(got) != 0 {
			t.Fatalf("step %d loosens original baseline: %q", i, got)
		}
		was, now := b.Aggregate[production][complexity], next.Aggregate[production][complexity]
		if now.Violations > was.Violations || now.Excess > was.Excess || len(next.Caps[production][complexity]) != 1 || next.Caps[production][complexity][id] != size {
			t.Fatalf("step %d grew caps or totals: %+v", i, next)
		}
		values[id] = 15
		values["c.go:TooBig"] = size + 1
		if problems, _ := compare(next, testScan(values)); len(problems) == 0 {
			t.Fatalf("step %d regained spent capacity", i)
		}
		delete(values, "c.go:TooBig")
		b, previous = next, id
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

// tsSQL and tsTemplates hold functions whose lines trip a token scanner that does not track
// template literals: substitutions, nested templates, comment markers inside strings
// and a doc comment inside a body. The comment on each gives its code lines.
const tsSQL = "export function sql(name: string) { // 6\n" +
	"  // who asks\n  const q = `select *\n    from ${name}\n    where 1`;\n\n  /* then */\n  return q;\n}\n\n"

const tsTemplates = tsSQL +
	"/** Doc. */\nexport function after() { // 3\n  // note\n\n  return 1;\n}\n\n" +
	"export function nested(xs: string[]) { // 6, and map 2\n  return `<ul>\n${xs.map((x) => `<li>${x}\n</li>`).join(\"\")}\n</ul>`;\n}\n\n" +
	"export function strs() { // 8\n  const a = \"// not a comment\";\n  const b = '/* nor';\n  const c = `this */\n// still a string\n`;\n  return a + b + c;\n}\n\n" +
	"export function docs() { // 4\n  /**\n   * Inner doc.\n   */\n  const x = 1;\n  return x;\n}\n\n" +
	"export function last() { // 3\n  // done\n  return 0;\n}\n"

// TestTypeScriptLinesAfterTemplates counts code lines around multi-line templates with
// substitutions, which once made every later comment and blank line count as code.
func TestTypeScriptLinesAfterTemplates(t *testing.T) {
	s, err := scanSources(t, t.TempDir(), map[string]string{"a.ts": tsTemplates})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"sql": 6, "after": 3, "nested": 6, "nested/map": 2, "strs": 8, "docs": 4, "last": 3}
	got := map[string]int{}
	for id, v := range s.Values[production][functionLines] {
		got[id[strings.LastIndex(id, ":")+1:]] = v
	}
	if !maps.Equal(got, want) {
		t.Fatalf("code lines %v, want %v", got, want)
	}
}

// TestTypeScriptDuplicatesAfterTemplates finds a clone that follows a multi-line template
// with a substitution in one file and stands alone in another.
func TestTypeScriptDuplicatesAfterTemplates(t *testing.T) {
	clone := "export function f(x: number) {\n"
	for i := range 40 {
		clone += fmt.Sprintf("\tx = x + %d;\n", i)
	}
	clone += "}\n"
	s, err := scanSources(t, t.TempDir(), map[string]string{"a.ts": tsSQL + clone, "b.ts": clone})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Values[production][duplicateGroups]) != 1 {
		t.Fatalf("duplicate groups %v, want one", s.Values[production][duplicateGroups])
	}
}

// TestTSXCountsLikeTypeScript measures JSX with the same rules as other code: element
// lines are code lines, and && and ?: inside JSX add to complexity, so the component
// below is 3 (1, one &&, one ?:) and 7 lines.
func TestTSXCountsLikeTypeScript(t *testing.T) {
	src := "export function C({ a }: { a: number }) {\n  return (\n    <div>\n      {a && <b>{a > 1 ? 1 : 2}</b>}\n    </div>\n  );\n}\n"
	s, err := scanSources(t, t.TempDir(), map[string]string{"c.tsx": src})
	if err != nil {
		t.Fatal(err)
	}
	for metric, want := range map[string]int{complexity: 3, functionLines: 7} {
		for id, got := range s.Values[production][metric] {
			if strings.HasSuffix(id, "c.tsx:C") && got != want {
				t.Errorf("%s of %s is %d, want %d", metric, id, got, want)
			}
		}
		if len(s.Values[production][metric]) != 1 {
			t.Errorf("%s: %v, want only the component", metric, s.Values[production][metric])
		}
	}
}
