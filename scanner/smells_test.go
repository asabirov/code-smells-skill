package main

import (
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
		{"grew by one", map[string]int{"a.go:Big": 41}, []string{"grew from 40 to 41", "more than the baseline"}},
		{"new item", map[string]int{"a.go:Big": 40, "b.go:New": 16}, []string{"b.go:New is 16, over the limit of 15", "more than the baseline"}},
		{"shrink pays for nothing", map[string]int{"a.go:Big": 20, "b.go:New": 16}, []string{"b.go:New is 16", "more than the baseline"}},
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
	if problems, _ := compare(testBaseline(), s); len(problems) != 1 {
		t.Fatalf("problems %q, want one about the tools", problems)
	}
}

func TestCapsForOnlyLowers(t *testing.T) {
	b := testBaseline()
	caps, agg := capsFor(b, testScan(map[string]int{"a.go:Big": 30, "c.go:Fine": 3}), b.Caps)
	if got := caps[production][complexity]; len(got) != 1 || got["a.go:Big"] != 30 {
		t.Fatalf("caps %v, want a.go:Big lowered to 30", got)
	}
	if got := agg[production][complexity]; got != (Aggregate{1, 15}) {
		t.Fatalf("aggregate %v", got)
	}
	caps, _ = capsFor(b, testScan(map[string]int{"a.go:Big": 12}), b.Caps)
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
	if got := loosenings(branch, main); !slices.Equal(got, want) {
		t.Fatalf("loosenings %q, want %q", got, want)
	}
	if loosenings(main, branch) != nil {
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
