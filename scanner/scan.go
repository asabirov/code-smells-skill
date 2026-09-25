package main

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"runtime"
	"runtime/debug"
	"slices"
	"sort"
	"strings"
)

// Scopes and metrics. Every measurement is an item (a function, file, package or
// import cycle) with a whole-number value in one scope and one metric.
const (
	production = "production"
	tests      = "tests"

	functionLines   = "function_lines"
	complexity      = "complexity"
	fileLines       = "file_lines"
	fanOut          = "fan_out"
	duplicateLines  = "duplicate_lines"
	duplicateGroups = "duplicate_groups"
	cycles          = "cycles"

	dupTokens = 100
)

// The measuring code is built into the binary, and .smells.json records its hash as
// "definitions". Any change to how something is measured, even by accident, fails the
// check until the baseline is re-measured on purpose. README.md defines each metric.
//
//go:embed scan.go files.go golang.go typescript.go typescript/ts-scan.ts
var measuringCode embed.FS

var definitions = hashMeasuringCode()

func hashMeasuringCode() string {
	h := sha256.New()
	for _, name := range []string{"scan.go", "files.go", "golang.go", "typescript.go", "typescript/ts-scan.ts"} {
		src, _ := measuringCode.ReadFile(name)
		fmt.Fprintf(h, "%s %d\n", name, len(src))
		h.Write(src)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// Scan holds every measurement: scope -> metric -> item -> value.
type Scan struct {
	Tools  map[string]string                    `json:"tools"`
	Values map[string]map[string]map[string]int `json:"values"`
}

func (s *Scan) set(scope, metric, id string, v int) {
	if s.Values[scope] == nil {
		s.Values[scope] = map[string]map[string]int{}
	}
	if s.Values[scope][metric] == nil {
		s.Values[scope][metric] = map[string]int{}
	}
	s.Values[scope][metric][id] = v
}

func scan(b *Baseline) (*Scan, error) {
	if err := checkLanguages(b.Languages); err != nil {
		return nil, err
	}
	rules := b.Files
	if err := rules.compile(); err != nil {
		return nil, err
	}
	files, err := listFiles(&rules, b.Languages)
	if err != nil {
		return nil, err
	}
	s := &Scan{Tools: toolVersions(), Values: map[string]map[string]map[string]int{}}
	graph := map[string]map[string]bool{}
	if slices.Contains(b.Languages, golang) {
		if err := scanGo(s, &rules, files[golang], graph); err != nil {
			return nil, err
		}
	}
	if slices.Contains(b.Languages, typescript) {
		version, err := scanTS(s, &rules, files[typescript], graph)
		if err != nil {
			return nil, err
		}
		s.Tools[typescript] = version
	}
	addGraph(s, graph)
	return s, nil
}

func toolVersions() map[string]string {
	tools := map[string]string{"definitions": definitions}
	// Only the language version matters to the parser; patch releases do not change it.
	parts := strings.SplitN(runtime.Version(), ".", 3)
	tools["go"] = strings.Join(parts[:min(2, len(parts))], ".")
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, d := range info.Deps {
			switch d.Path {
			case "github.com/uudashr/gocognit":
				tools["gocognit"] = d.Version
			case "github.com/mibk/dupl":
				tools["dupl"] = d.Version
			}
		}
	}
	return tools
}

func physicalLines(src []byte) int {
	n := bytes.Count(src, []byte("\n"))
	if len(src) > 0 && src[len(src)-1] != '\n' {
		n++
	}
	return n
}

// uniqueID adds #2, #3 to a name that already appeared in the same file.
func uniqueID(seen map[string]int, id string) string {
	seen[id]++
	if n := seen[id]; n > 1 {
		return fmt.Sprintf("%s#%d", id, n)
	}
	return id
}

// setGroup records a duplicate group, named by its sorted distinct files and the first
// twelve characters of its fingerprint.
func setGroup(s *Scan, r *Rules, members []string, fingerprint string, copies int) {
	sort.Strings(members)
	members = slices.Compact(members)
	scope := tests
	if slices.ContainsFunc(members, func(m string) bool { return r.scope(m) == production }) {
		scope = production
	}
	s.set(scope, duplicateGroups, fmt.Sprintf("%s @%.12s", strings.Join(members, " + "), fingerprint), copies)
}

// addGraph records each production module's fan-out, counting imports of excluded
// local packages such as internal/api/static, and every import cycle.
func addGraph(s *Scan, graph map[string]map[string]bool) {
	for node, targets := range graph {
		n := len(targets)
		if targets[node] {
			n--
		}
		s.set(production, fanOut, node, n)
	}
	for _, members := range stronglyConnected(graph) {
		s.set(production, cycles, strings.Join(members, " + "), 1)
	}
}

// stronglyConnected returns each group of two or more modules that import each other,
// directly or through others (Tarjan's algorithm), with members sorted.
func stronglyConnected(graph map[string]map[string]bool) [][]string {
	t := tarjan{graph: graph, index: map[string]int{}, low: map[string]int{}, on: map[string]bool{}}
	for _, node := range sortedKeys(graph) {
		if _, done := t.index[node]; !done {
			t.visit(node)
		}
	}
	return t.groups
}

type tarjan struct {
	graph  map[string]map[string]bool
	index  map[string]int
	low    map[string]int
	on     map[string]bool
	stack  []string
	groups [][]string
}

func (t *tarjan) visit(v string) {
	t.index[v], t.low[v] = len(t.index), len(t.index)
	t.stack = append(t.stack, v)
	t.on[v] = true
	for _, w := range sortedKeys(t.graph[v]) {
		if t.graph[w] == nil {
			continue
		}
		if _, done := t.index[w]; !done {
			t.visit(w)
			t.low[v] = min(t.low[v], t.low[w])
		} else if t.on[w] {
			t.low[v] = min(t.low[v], t.index[w])
		}
	}
	if t.low[v] != t.index[v] {
		return
	}
	var group []string
	for {
		w := t.stack[len(t.stack)-1]
		t.stack = t.stack[:len(t.stack)-1]
		t.on[w] = false
		group = append(group, w)
		if w == v {
			break
		}
	}
	if len(group) > 1 {
		sort.Strings(group)
		t.groups = append(t.groups, group)
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
