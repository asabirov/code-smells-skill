package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
)

// runReport prints the totals for each language and scope, the production import
// graph, and the ten worst production functions. A repository without .smells.json is
// measured with the default limits, so it can be surveyed before it has a baseline.
func runReport() error {
	b, err := reportBaseline()
	if err != nil {
		return err
	}
	s, err := scan(b)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(w, "\tfiles\tlines\tfunctions\tlong functions\tlong files\tmost complex\tcomplex functions\tduplicate lines\t")
	for _, lang := range b.Languages {
		for _, scope := range []string{production, tests} {
			reportRow(w, b, s, lang, scope)
		}
	}
	w.Flush()
	fan := only(s.Values[production][fanOut], "")
	fmt.Printf("\nProduction imports: %d modules, %d local edges, %d cycles.\n", len(fan), sum(fan), len(s.Values[production][cycles]))
	fmt.Println("Limits:", formatLimits(b.Budgets[production]))
	fmt.Println("\nWorst production functions, by the larger of lines/limit and complexity/limit:")
	return reportHotspots(os.Stdout, b, s)
}

func reportBaseline() (*Baseline, error) {
	if _, err := os.Stat(baselinePath); err == nil {
		return loadBaseline(baselinePath)
	}
	fmt.Printf("No %s here, so this uses the default limits and every file in a known language.\n\n", baselinePath)
	b := defaultBaseline()
	return b, detectFiles(b)
}

func reportRow(w *tabwriter.Writer, b *Baseline, s *Scan, lang, scope string) {
	values, limits := s.Values[scope], b.Budgets[scope]
	files := only(values[fileLines], lang)
	funcs := only(values[functionLines], lang)
	cx := only(values[complexity], lang)
	fmt.Fprintf(w, "%s %s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t\n", lang, scope,
		len(files), sum(files), len(funcs), over(funcs, limits[functionLines]), over(files, limits[fileLines]),
		most(cx), over(cx, limits[complexity]), sum(only(values[duplicateLines], lang)))
}

func reportHotspots(out io.Writer, b *Baseline, s *Scan) error {
	lines, cx := s.Values[production][functionLines], s.Values[production][complexity]
	lineLimit, cxLimit := float64(b.Budgets[production][functionLines]), float64(b.Budgets[production][complexity])
	score := func(id string) float64 {
		return max(float64(lines[id])/lineLimit, float64(cx[id])/cxLimit)
	}
	ids := sortedKeys(lines)
	sort.SliceStable(ids, func(i, j int) bool { return score(ids[i]) > score(ids[j]) })
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  function\tlines\tcomplexity")
	for _, id := range ids[:min(10, len(ids))] {
		fmt.Fprintf(w, "  %s\t%d\t%d\n", id, lines[id], cx[id])
	}
	return w.Flush()
}

// only keeps the items of one language; an empty language keeps them all.
func only(values map[string]int, lang string) map[string]int {
	out := map[string]int{}
	for id, v := range values {
		file, _, _ := strings.Cut(id, ":")
		if lang == "" || languageOf(file) == lang {
			out[id] = v
		}
	}
	return out
}

func sum(values map[string]int) int {
	n := 0
	for _, v := range values {
		n += v
	}
	return n
}

func over(values map[string]int, limit int) int {
	return aggregate(values, limit).Violations
}

func most(values map[string]int) int {
	n := 0
	for _, v := range values {
		n = max(n, v)
	}
	return n
}

func formatLimits(limits map[string]int) string {
	var parts []string
	for _, metric := range sortedKeys(limits) {
		parts = append(parts, fmt.Sprintf("%s %d", metric, limits[metric]))
	}
	return strings.Join(parts, ", ")
}
