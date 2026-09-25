package main

import (
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strings"
)

// review shows a reviewer what a branch did to the smells against base: whether the
// check passes, how the baseline moved and why, which hotspots are new, and the worst
// functions. It fails only when the code cannot be measured, never on what it finds.
func review(w io.Writer, base string) error {
	if exec.Command("git", "rev-parse", "--verify", "--quiet", base+"^{commit}").Run() != nil {
		return fmt.Errorf("%s is not a commit here; fetch it or pass --base with another ref", base)
	}
	b, err := loadBaseline(baselinePath)
	if err != nil {
		return err
	}
	s, err := scan(b)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Smells review against %s (code-smells %s)\n\n", base, version)
	problems, _ := compare(b, s)
	old, err := baseBaseline(base)
	if err != nil {
		return err
	}
	if old != nil {
		for _, key := range loosenings(b, old) {
			if !excepted(b, key) {
				problems = append(problems, key+" is looser than on "+base+" and has no exception")
			}
		}
	}
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
	fmt.Fprintln(w, "\nWorst production functions, by the larger of lines/limit and complexity/limit:")
	return reportHotspots(w, b, s)
}

func baseBaseline(base string) (*Baseline, error) {
	src, err := exec.Command("git", "show", base+":"+baselinePath).Output()
	if err != nil {
		return nil, nil
	}
	return parseBaseline(src, base+":"+baselinePath)
}

// reviewBaseline lists how the branch's baseline differs from base's, and the reasons
// given for any loosening.
func reviewBaseline(w io.Writer, base string, old, b *Baseline) {
	looser, tighter := loosenings(b, old), loosenings(old, b)
	fmt.Fprintf(w, "\nBaseline against %s: %d looser, %d tighter.\n", base, len(looser), len(tighter))
	for _, key := range looser {
		fmt.Fprintf(w, "  looser: %s%s\n", key, change(key, old, b))
	}
	for _, key := range tighter {
		fmt.Fprintf(w, "  tighter: %s%s\n", key, change(key, old, b))
	}
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

// change shows the old and new value of a cap, budget or aggregate key.
func change(key string, old, b *Baseline) string {
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

// reviewHotspots lists items over a limit that base's baseline did not cap, and items
// that grew past base's cap.
func reviewHotspots(w io.Writer, base string, old, b *Baseline, s *Scan) {
	var found []string
	for _, scope := range sortedKeys(b.Budgets) {
		for _, metric := range sortedKeys(b.Budgets[scope]) {
			limit := b.Budgets[scope][metric]
			for _, id := range sortedKeys(s.Values[scope][metric]) {
				v := s.Values[scope][metric][id]
				was, capped := old.Caps[scope][metric][id]
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
