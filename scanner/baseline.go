package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"slices"
)

// Baseline is .smells.json: the limits, and a cap for every item already over one.
type Baseline struct {
	Tools      map[string]string                    `json:"tools"`
	Languages  []string                             `json:"languages"`
	Files      Rules                                `json:"files"`
	Budgets    map[string]map[string]int            `json:"budgets"`
	Aggregate  map[string]map[string]Aggregate      `json:"aggregate"`
	Caps       map[string]map[string]map[string]int `json:"caps"`
	Exceptions []Exception                          `json:"exceptions"`
}

// Aggregate is how many items in a scope and metric are over the limit, and by how much
// in total, so a hotspot that shrinks cannot make room for a new one.
type Aggregate struct {
	Violations int `json:"violations"`
	Excess     int `json:"excess"`
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
		if b, err = parseBaseline(src, baselinePath); err != nil || b.Tools != nil {
			return b, err == nil, err
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
			"%s was measured with %s, but code-smells %s measures with %s, so the numbers cannot be compared; run the skill version that wrote %s, or re-measure it in its own reviewed change",
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
	for _, id := range sortedKeys(values) {
		v := values[id]
		limitCap, capped := caps[id]
		switch {
		case v <= limit:
		case !capped:
			problems = append(problems, fmt.Sprintf("%s %s: %s is %d, over the limit of %d", scope, metric, id, v, limit))
		case v > limitCap:
			problems = append(problems, fmt.Sprintf("%s %s: %s grew from %d to %d", scope, metric, id, limitCap, v))
		}
	}
	for _, id := range sortedKeys(caps) {
		if values[id] < caps[id] {
			notes = append(notes, fmt.Sprintf("%s %s: %s is %d, below its cap of %d; smells baseline can lower the cap", scope, metric, id, values[id], caps[id]))
		}
	}
	now, was := aggregate(values, limit), b.Aggregate[scope][metric]
	if now.Violations > was.Violations || now.Excess > was.Excess {
		problems = append(problems, fmt.Sprintf("%s %s: %d over the limit by %d in total; the baseline allows %d over by %d",
			scope, metric, now.Violations, now.Excess, was.Violations, was.Excess))
	}
	return problems, notes
}

func aggregate(values map[string]int, limit int) Aggregate {
	var a Aggregate
	for _, v := range values {
		if v > limit {
			a.Violations++
			a.Excess += v - limit
		}
	}
	return a
}

// capsFor caps every item now over its limit at its current value. With previous caps,
// an item keeps a cap only if it had one; compare has already refused anything new.
func capsFor(b *Baseline, s *Scan, previous map[string]map[string]map[string]int) (map[string]map[string]map[string]int, map[string]map[string]Aggregate) {
	caps := map[string]map[string]map[string]int{}
	aggregates := map[string]map[string]Aggregate{}
	for scope, budgets := range b.Budgets {
		caps[scope], aggregates[scope] = map[string]map[string]int{}, map[string]Aggregate{}
		for metric, limit := range budgets {
			values := s.Values[scope][metric]
			caps[scope][metric] = map[string]int{}
			aggregates[scope][metric] = aggregate(values, limit)
			for id, v := range values {
				if _, had := previous[scope][metric][id]; v > limit && (previous == nil || had) {
					caps[scope][metric][id] = v
				}
			}
		}
	}
	return caps, aggregates
}

// runBaseline writes .smells.candidate.json for a person to review and move into place.
// Without a measured baseline, it caps everything already over a limit. Otherwise it
// only lowers caps and removes fixed items; it never raises a cap, adds an item or
// changes the limits, files or tools, and it refuses while the code is over the baseline.
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
	if !measured {
		candidate.Tools = s.Tools
		candidate.Caps, candidate.Aggregate = capsFor(b, s, nil)
	} else {
		if problems, _ := compare(b, s); len(problems) > 0 {
			for _, p := range problems {
				fmt.Fprintln(os.Stderr, "smell:", p)
			}
			return fmt.Errorf("did not write %s, because the code is over %s; fix the code, or raise the cap by hand with an exception", candidatePath, baselinePath)
		}
		candidate.Caps, candidate.Aggregate = capsFor(b, s, b.Caps)
		lowered := loosenings(b, &candidate)
		if len(lowered) == 0 {
			fmt.Println("nothing to lower:", baselinePath, "already matches the code")
			return nil
		}
		for _, key := range lowered {
			fmt.Println("lower:", key)
		}
	}
	if err := saveBaseline(candidatePath, &candidate); err != nil {
		return err
	}
	fmt.Printf("wrote %s for %v in %v; review it, then run: mv %s %s\n", candidatePath, b.Languages, b.Files.Include, candidatePath, baselinePath)
	return nil
}

// guard compares b with the baseline committed at ref: anything looser needs an
// exception. It reads what was last fetched, so the check needs no network.
func guard(b *Baseline, ref string) (old *Baseline, problems, notes []string) {
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
	for _, key := range loosenings(b, old) {
		if !excepted(b, key) {
			problems = append(problems, fmt.Sprintf("%s is looser than on %s; tighten it, or add an exception for it with a reason and an issue", key, ref))
		}
	}
	return old, problems, nil
}
func excepted(b *Baseline, key string) bool {
	return slices.ContainsFunc(b.Exceptions, func(e Exception) bool {
		return e.Key == key && e.Reason != "" && e.Issue != ""
	})
}

// loosenings lists every way b allows more than old does.
func loosenings(b, old *Baseline) []string {
	var keys []string
	if !maps.Equal(b.Tools, old.Tools) {
		keys = append(keys, "tools")
	}
	if !slices.Equal(b.Languages, old.Languages) || !slices.Equal(b.Files.Include, old.Files.Include) ||
		!slices.Equal(b.Files.Exclude, old.Files.Exclude) || !slices.Equal(b.Files.Tests, old.Files.Tests) {
		keys = append(keys, "files")
	}
	for _, scope := range sortedKeys(old.Budgets) {
		for _, metric := range sortedKeys(old.Budgets[scope]) {
			keys = append(keys, looserMetric(b, old, scope, metric)...)
		}
	}
	return keys
}

func looserMetric(b, old *Baseline, scope, metric string) []string {
	var keys []string
	path := scope + "/" + metric
	if limit, ok := b.Budgets[scope][metric]; !ok || limit > old.Budgets[scope][metric] {
		keys = append(keys, "budgets/"+path)
	}
	now, was := b.Aggregate[scope][metric], old.Aggregate[scope][metric]
	if now.Violations > was.Violations || now.Excess > was.Excess {
		keys = append(keys, "aggregate/"+path)
	}
	oldCaps := old.Caps[scope][metric]
	for _, id := range sortedKeys(b.Caps[scope][metric]) {
		if was, ok := oldCaps[id]; !ok || b.Caps[scope][metric][id] > was {
			keys = append(keys, "caps/"+path+"/"+id)
		}
	}
	return keys
}
