package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

// TestNewDuplicateGroupFails replaces a baselined clone with a different, smaller one in
// the same files: the covered lines shrink, but the new group must still fail.
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
			b.Caps, b.Aggregate = capsFor(b, old, nil)
			if problems, _ := compare(b, old); len(problems) != 0 {
				t.Fatalf("the baselined code fails: %q", problems)
			}
			now, err := scanSources(t, dir, map[string]string{"a" + c.ext: c.new, "b" + c.ext: c.new})
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
