package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"
)

// runReport prints the totals for each language and scope, the production import
// graph, and the ten worst production functions. A repository without a measured
// .smells.json is measured with the default limits, so it can be surveyed first.
func runReport() error {
	b, measured, err := startBaseline()
	if err != nil {
		return err
	}
	if !measured {
		fmt.Printf("%s has no measurements here, so this uses the default limits for %v in %v.\n\n", baselinePath, b.Languages, b.Files.Include)
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
	fmt.Println("Limits:", formatMap(b.Budgets[production]))
	fmt.Println(worstHeading)
	return reportHotspots(os.Stdout, b, s)
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

const worstHeading = "\nWorst production functions, ranked by the larger of lines ÷ line limit and complexity ÷ complexity limit:"

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

func formatMap[V any](m map[string]V) string {
	var parts []string
	for _, k := range sortedKeys(m) {
		parts = append(parts, fmt.Sprintf("%s %v", k, m[k]))
	}
	return strings.Join(parts, ", ")
}

// review shows a reviewer what a branch did to the smells against base: whether the
// check passes, how the baseline moved and why, which hotspots are new, and the worst
// functions. It fails only when the code cannot be measured, never on what it finds.
func review(w io.Writer, base string) error {
	if base == "" {
		return fmt.Errorf("%s; or pass --base with a ref", noDefaultBranch)
	}
	if exec.Command("git", "rev-parse", "--verify", "--quiet", base+"^{commit}").Run() != nil {
		return fmt.Errorf("%s is not a commit here; fetch it or pass --base with another ref", base)
	}
	b, err := loadBaseline()
	if err != nil {
		return err
	}
	s, err := scan(b)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Smells review against %s (code-smells %s)\n\n", base, version)
	problems, _ := compare(b, s)
	old, looser, _ := guard(b, s, base)
	problems = append(problems, looser...)
	if len(problems) == 0 {
		fmt.Fprintln(w, "The check passes.")
	} else {
		fmt.Fprintf(w, "The check fails with %d problem(s):\n", len(problems))
		list(w, problems)
	}
	if old == nil {
		fmt.Fprintf(w, "\n%s has no %s, so there is nothing to compare the baseline with.\n", base, baselinePath)
	} else {
		reviewBaseline(w, base, old, b)
		reviewHotspots(w, base, old, b, s)
	}
	fmt.Fprintln(w, worstHeading)
	return reportHotspots(w, b, s)
}

// reviewBaseline lists how the branch's baseline differs from base's, and the reasons
// given for any loosening.
func reviewBaseline(w io.Writer, base string, old, b *Baseline) {
	fmt.Fprintf(w, "\nBaseline against %s: ", base)
	printChanges(w, old, b)
	var added []string
	for _, e := range b.Exceptions {
		if !slices.ContainsFunc(old.Exceptions, func(o Exception) bool { return o.Key == e.Key }) {
			added = append(added, fmt.Sprintf("%s: %s (%s)", e.Key, e.Reason, e.Issue))
		}
	}
	if len(added) > 0 {
		fmt.Fprintln(w, "New exceptions:")
		list(w, added)
	}
}

// printChanges lists how b differs from old, with the old and new numbers.
func printChanges(w io.Writer, old, b *Baseline) {
	looser := loosenings(b, old)
	tighter := slices.DeleteFunc(loosenings(old, b), func(k string) bool { return k == "tools" })
	fmt.Fprintf(w, "%d looser, %d tighter.\n", len(looser), len(tighter))
	for _, key := range looser {
		fmt.Fprintf(w, "  looser: %s%s\n", key, change(key, old, b))
	}
	for _, key := range tighter {
		fmt.Fprintf(w, "  tighter: %s%s\n", key, change(key, old, b))
	}
}

// change shows the old and new value of a cap, budget, aggregate or tools key.
func change(key string, old, b *Baseline) string {
	if key == "tools" {
		return fmt.Sprintf(" (%s -> %s)", formatMap(old.Tools), formatMap(b.Tools))
	}
	parts := strings.SplitN(key, "/", 4)
	if len(parts) < 3 {
		return ""
	}
	scope, metric := parts[1], parts[2]
	show := func(bl *Baseline) string {
		switch parts[0] {
		case "budgets":
			if v, ok := bl.Budgets[scope][metric]; ok {
				return fmt.Sprint(v)
			}
		case "aggregate":
			a := bl.Aggregate[scope][metric]
			return fmt.Sprintf("%d items over by %d", a.Violations, a.Excess)
		case "caps":
			if v, ok := bl.Caps[scope][metric][parts[len(parts)-1]]; ok {
				return fmt.Sprint(v)
			}
		}
		return "none"
	}
	return fmt.Sprintf(" (%s -> %s)", show(old), show(b))
}

// reviewHotspots lists items over base's limit that base's baseline did not cap, and
// items that grew past base's cap, following caps of moved code, so a branch that
// raises a limit hides nothing.
func reviewHotspots(w io.Writer, base string, old, b *Baseline, s *Scan) {
	var found []string
	for _, scope := range sortedKeys(b.Budgets) {
		for _, metric := range sortedKeys(b.Budgets[scope]) {
			limit, ok := old.Budgets[scope][metric]
			if !ok {
				limit = b.Budgets[scope][metric]
			}
			moved := moves(old.Caps[scope][metric], s.Values[scope][metric], s.Content, old.Fingerprints)
			for _, id := range sortedKeys(s.Values[scope][metric]) {
				v := s.Values[scope][metric][id]
				was, capped := old.Caps[scope][metric][id]
				if from, ok := moved[id]; ok {
					was, capped = old.Caps[scope][metric][from], true
				}
				switch {
				case v <= limit:
				case !capped:
					found = append(found, fmt.Sprintf("new: %s %s %s is %d, limit %d", scope, metric, id, v, limit))
				case v > was:
					found = append(found, fmt.Sprintf("grew: %s %s %s is %d, capped at %d on %s", scope, metric, id, v, was, base))
				}
			}
		}
	}
	fmt.Fprintf(w, "\nHotspots new or grown since %s: %d.\n", base, len(found))
	list(w, found)
}

func list(w io.Writer, lines []string) {
	for _, l := range lines {
		fmt.Fprintln(w, "  -", l)
	}
}
