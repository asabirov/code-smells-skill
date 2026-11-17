package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"reflect"
	"slices"
	"sort"
	"strings"
)

// Baseline is .smells.json: the limits, and a cap for every item already over one.
type Baseline struct {
	Tools      map[string]string                    `json:"tools"`
	Languages  []string                             `json:"languages"`
	Files      Rules                                `json:"files"`
	Budgets    map[string]map[string]int            `json:"budgets"`
	Caps       map[string]map[string]map[string]int `json:"caps"`
	Exceptions []Exception                          `json:"exceptions"`
	legacy     bool
}

// Exception allows one loosening compared with the default branch, such as a raised
// cap, once a reviewer has agreed to it. Key names the loosened entry as the check
// prints it.
type Exception struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
	Issue  string `json:"issue"`
}

func defaultBaseline() *Baseline {
	limits := map[string]int{functionLines: 80, complexity: 15, fileLines: 500, duplicateLines: 0, duplicateGroups: 0}
	prod := maps.Clone(limits)
	prod[fanOut], prod[cycles] = 8, 0
	return &Baseline{
		Files: Rules{
			Exclude: []string{"vendor/", "node_modules/"},
			Tests:   []string{`_test\.go$`, `\.test\.ts$`, `\.spec\.ts$`},
		},
		Budgets:    map[string]map[string]int{production: prod, tests: limits},
		Exceptions: []Exception{},
	}
}

// loadBaseline reads .smells.json, which must already hold measurements.
func loadBaseline() (*Baseline, error) {
	b, measured, err := startBaseline()
	if err == nil && !measured {
		err = fmt.Errorf("%s is missing or has no measurements yet; run smells baseline to create it", baselinePath)
	}
	return b, err
}

// startBaseline reads .smells.json. When there is none, or it holds only languages
// and files, it returns the default limits, with the languages and files found in the
// repository filling any gaps, and measured is false.
func startBaseline() (b *Baseline, measured bool, err error) {
	src, err := os.ReadFile(baselinePath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, false, err
	}
	if src != nil {
		if b, err = parseBaseline(src, baselinePath); err != nil {
			return nil, false, err
		}
		if b.Tools != nil {
			b.legacy = hasLegacyKeys(src)
			return b, true, nil
		}
	}
	b = defaultBaseline()
	if src != nil {
		if err := json.Unmarshal(src, b); err != nil {
			return nil, false, err
		}
	}
	return b, false, detectFiles(b)
}

// hasLegacyKeys reports whether src holds aggregate or measured_by, which an older
// skill wrote and the next baseline drops.
func hasLegacyKeys(src []byte) bool {
	var raw map[string]json.RawMessage
	if json.Unmarshal(src, &raw) != nil {
		return false
	}
	_, aggregate := raw["aggregate"]
	_, measuredBy := raw["measured_by"]
	return aggregate || measuredBy
}

func parseBaseline(src []byte, name string) (*Baseline, error) {
	var b Baseline
	if err := json.Unmarshal(src, &b); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return &b, nil
}

func saveBaseline(path string, b *Baseline) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return writeJSON(f, b)
}

// compare lists what in the scan breaks the baseline, and notes caps that could come down.
func compare(b *Baseline, s *Scan) (problems, notes []string) {
	if !maps.Equal(b.Tools, s.Tools) {
		problems = append(problems, fmt.Sprintf(
			"%s was measured with %s, but code-smells %s measures with %s, so the numbers cannot be compared; run the skill version that wrote %s, or re-measure it with smells baseline in a change of its own",
			baselinePath, formatMap(b.Tools), version, formatMap(s.Tools), baselinePath))
	}
	for _, scope := range sortedKeys(b.Budgets) {
		for _, metric := range sortedKeys(b.Budgets[scope]) {
			p, n := compareMetric(b, s, scope, metric)
			problems = append(problems, p...)
			notes = append(notes, n...)
		}
	}
	return problems, notes
}

func compareMetric(b *Baseline, s *Scan, scope, metric string) (problems, notes []string) {
	limit := b.Budgets[scope][metric]
	caps := b.Caps[scope][metric]
	values := s.Values[scope][metric]
	moved := inherit(caps, uncapped(values, caps, limit), s, values, limit)
	for _, id := range sortedKeys(values) {
		v := values[id]
		limitCap, capped := caps[id]
		if from, ok := moved[id]; ok {
			limitCap, capped = caps[from], true
		}
		switch {
		case v <= limit:
		case !capped:
			problems = append(problems, fmt.Sprintf("%s %s: %s is %d, over the limit of %d", scope, metric, id, v, limit))
		case v > limitCap:
			problems = append(problems, fmt.Sprintf("%s %s: %s grew from %d to %d", scope, metric, id, limitCap, v))
		}
	}
	movedTo := map[string]string{}
	for to, from := range moved {
		movedTo[from] = to
	}
	for _, id := range sortedKeys(caps) {
		if to, ok := movedTo[id]; ok {
			notes = append(notes, fmt.Sprintf("%s %s: %s %s, and %s takes its cap of %d; smells baseline moves the cap", scope, metric, id, capSource(values, id), to, caps[id]))
		} else if _, ok := values[id]; !ok {
			notes = append(notes, fmt.Sprintf("%s %s: %s is gone; smells baseline drops its cap of %d", scope, metric, id, caps[id]))
		} else if values[id] < caps[id] {
			notes = append(notes, fmt.Sprintf("%s %s: %s is %d, below its cap of %d; smells baseline can lower the cap", scope, metric, id, values[id], caps[id]))
		}
	}
	return problems, notes
}

// inherit gives uncapped items in want caps freed by removal or reaching the limit.
// An item may take a cap no smaller than its value. Smaller items choose first and each takes the smallest cap that fits,
// so a function split in two can use two freed caps. The match is by size only, so a
// new function may take a freed cap; each freed cap covers one new item, and review
// names it.
func inherit(caps, want map[string]int, s *Scan, values map[string]int, limit int) map[string]string {
	var free []string
	for _, id := range sortedKeys(caps) {
		if v, present := values[id]; !s.measures(id) || present && v <= limit {
			free = append(free, id)
		}
	}
	sort.SliceStable(free, func(i, j int) bool { return caps[free[i]] < caps[free[j]] })
	ids := sortedKeys(want)
	sort.SliceStable(ids, func(i, j int) bool { return want[ids[i]] < want[ids[j]] })
	pairs := map[string]string{}
	for _, id := range ids {
		if i := slices.IndexFunc(free, func(f string) bool { return caps[f] >= want[id] }); i >= 0 {
			pairs[id] = free[i]
			free = slices.Delete(free, i, i+1)
		}
	}
	return pairs
}

func capSource(values map[string]int, id string) string {
	if _, present := values[id]; present {
		return "is at or below the limit"
	}
	return "is gone"
}

// uncapped keeps the values over limit that caps does not cap.
func uncapped(values, caps map[string]int, limit int) map[string]int {
	out := map[string]int{}
	for id, v := range values {
		if _, ok := caps[id]; !ok && v > limit {
			out[id] = v
		}
	}
	return out
}

// measures says whether any scope or metric of the scan still measures id.
func (s *Scan) measures(id string) bool {
	for _, metrics := range s.Values {
		for _, values := range metrics {
			if _, ok := values[id]; ok {
				return true
			}
		}
	}
	return false
}

// capsFor caps every item now over its limit at its current value. When lowering, an
// item keeps a cap only if it had one or took a freed cap; compare has already
// refused anything new.
func capsFor(b *Baseline, s *Scan, lowering bool) map[string]map[string]map[string]int {
	caps := map[string]map[string]map[string]int{}
	for scope, budgets := range b.Budgets {
		caps[scope] = map[string]map[string]int{}
		for metric, limit := range budgets {
			values := s.Values[scope][metric]
			caps[scope][metric] = map[string]int{}
			moved := inherit(b.Caps[scope][metric], uncapped(values, b.Caps[scope][metric], limit), s, values, limit)
			for id, v := range values {
				_, had := b.Caps[scope][metric][id]
				_, isMove := moved[id]
				if v <= limit || lowering && !had && !isMove {
					continue
				}
				caps[scope][metric][id] = v
			}
		}
	}
	return caps
}

// runBaseline writes .smells.candidate.json for a person to review and move into place.
// Without a measured baseline, it caps everything already over a limit. With one
// measured by other tools, it re-measures: it caps everything over a limit under the
// new tools, keeps the limits and files, and prints the old and new numbers. Otherwise
// it only lowers caps, moves them with moved code and removes fixed items; it never
// raises a cap or adds an item, and it refuses while the code is over the baseline.
func runBaseline() error {
	b, measured, err := startBaseline()
	if err != nil {
		return err
	}
	s, err := scan(b)
	if err != nil {
		return err
	}
	candidate := *b
	switch {
	case !measured:
		candidate.Tools = s.Tools
		candidate.Caps = capsFor(b, s, false)
	case !maps.Equal(b.Tools, s.Tools):
		candidate.Tools = s.Tools
		candidate.Caps = capsFor(b, s, false)
		fmt.Printf("re-measured with %s; %s was measured with %s\n", formatMap(s.Tools), baselinePath, formatMap(b.Tools))
		fmt.Printf("Against %s: ", baselinePath)
		printChanges(os.Stdout, b, &candidate, s)
	default:
		problems, notes := compare(b, s)
		if len(problems) > 0 {
			for _, p := range problems {
				fmt.Fprintln(os.Stderr, "smell:", p)
			}
			return fmt.Errorf("did not write %s, because the code is over %s; fix the code, or raise the cap by hand with an exception", candidatePath, baselinePath)
		}
		candidate.Caps = capsFor(b, s, true)
		if reflect.DeepEqual(b.Caps, candidate.Caps) && !b.legacy {
			fmt.Println("nothing to change:", baselinePath, "already matches the code")
			return nil
		}
		for _, n := range notes {
			fmt.Println("note:", n)
		}
	}
	if err := saveBaseline(candidatePath, &candidate); err != nil {
		return err
	}
	fmt.Printf("wrote %s for %v in %v; review it, then run: mv %s %s\n", candidatePath, b.Languages, b.Files.Include, candidatePath, baselinePath)
	return nil
}

// defaultBranch names origin's default branch as git last fetched it, such as
// origin/main, or returns "" when origin/HEAD is not set.
func defaultBranch() string {
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "origin/HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

const noDefaultBranch = "origin/HEAD is not set, so this repository's default branch is unknown; run git remote set-head origin --auto"

// guard compares b with the baseline committed at ref: anything looser needs an
// exception. A baseline measured by other tools than ref's is a re-measure, named in
// the notes, and its caps are compared with ref's by the same rule. It reads what was
// last fetched, so the check needs no network.
func guard(b *Baseline, s *Scan, ref string) (old *Baseline, problems, notes []string) {
	if ref == "" {
		return nil, []string{noDefaultBranch + ", so the baseline cannot be compared with it"}, nil
	}
	if exec.Command("git", "rev-parse", "--verify", "--quiet", ref+"^{commit}").Run() != nil {
		return nil, []string{ref + " is unknown here, so the baseline cannot be compared with it; run git fetch origin"}, nil
	}
	src, err := exec.Command("git", "show", ref+":"+baselinePath).Output()
	if err != nil {
		return nil, nil, []string{ref + " has no " + baselinePath + " yet, so only this branch's baseline is checked"}
	}
	old, err = parseBaseline(src, ref+":"+baselinePath)
	if err != nil {
		return nil, []string{err.Error()}, nil
	}
	if !maps.Equal(b.Tools, old.Tools) {
		notes = append(notes, fmt.Sprintf("this branch re-measures %s with %s (on %s: %s); smells review --base %s shows the old and new numbers",
			baselinePath, formatMap(b.Tools), ref, formatMap(old.Tools), ref))
	}
	for _, key := range loosenings(b, old, s) {
		if !excepted(b, key) {
			problems = append(problems, fmt.Sprintf("%s is looser than on %s; tighten it, or add an exception for it with a reason and an issue", key, ref))
		}
	}
	return old, problems, notes
}

func excepted(b *Baseline, key string) bool {
	return slices.ContainsFunc(b.Exceptions, func(e Exception) bool {
		return e.Key == key && e.Reason != "" && e.Issue != ""
	})
}

// loosenings lists every way b allows more than old does. A new cap is not looser when
// it takes the cap of an item that scan s no longer measures and is no larger.
func loosenings(b, old *Baseline, s *Scan) []string {
	var keys []string
	if !slices.Equal(b.Languages, old.Languages) || !slices.Equal(b.Files.Include, old.Files.Include) ||
		!slices.Equal(b.Files.Exclude, old.Files.Exclude) || !slices.Equal(b.Files.Tests, old.Files.Tests) {
		keys = append(keys, "files")
	}
	for _, scope := range sortedKeys(old.Budgets) {
		for _, metric := range sortedKeys(old.Budgets[scope]) {
			keys = append(keys, looserMetric(b, old, s, scope, metric)...)
		}
	}
	return keys
}

func looserMetric(b, old *Baseline, s *Scan, scope, metric string) []string {
	var keys []string
	path := scope + "/" + metric
	if limit, ok := b.Budgets[scope][metric]; !ok || limit > old.Budgets[scope][metric] {
		keys = append(keys, "budgets/"+path)
	}
	oldCaps, caps := old.Caps[scope][metric], b.Caps[scope][metric]
	freed := maps.Clone(oldCaps)
	maps.DeleteFunc(freed, func(id string, _ int) bool { _, kept := caps[id]; return kept })
	moved := inherit(freed, uncapped(caps, oldCaps, -1), s, s.Values[scope][metric], old.Budgets[scope][metric])
	for _, id := range sortedKeys(caps) {
		was, ok := oldCaps[id]
		if from, isMove := moved[id]; isMove {
			was, ok = oldCaps[from], true
		}
		if !ok || caps[id] > was {
			keys = append(keys, "caps/"+path+"/"+id)
		}
	}
	return keys
}
