package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"slices"
)

// Baseline is .smells.json: the limits, and a cap for every item already over one.
type Baseline struct {
	Schema     int                                  `json:"schema"`
	Tools      map[string]string                    `json:"tools"`
	Metrics    map[string]string                    `json:"metrics"`
	Languages  []string                             `json:"languages"`
	Files      Rules                                `json:"files"`
	Budgets    map[string]map[string]int            `json:"budgets"`
	Aggregate  map[string]map[string]Aggregate      `json:"aggregate"`
	Caps       map[string]map[string]map[string]int `json:"caps"`
	Exceptions []Exception                          `json:"exceptions"`
}

// Aggregate is how many items in a scope and metric are over the limit, and by how much
// in total, so a hotspot that shrinks cannot pay for a new one.
type Aggregate struct {
	Violations int `json:"violations"`
	Excess     int `json:"excess"`
}

// Exception allows one loosening relative to the default branch, such as a raised cap,
// once a reviewer has agreed to it. Key names the loosened entry, as smell-check prints it.
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
		Schema:  1,
		Metrics: metricDefinitions,
		Files: Rules{
			Exclude: []string{"vendor/", "node_modules/"},
			Tests:   []string{`_test\.go$`, `\.test\.ts$`, `\.spec\.ts$`},
		},
		Budgets:    map[string]map[string]int{production: prod, tests: limits},
		Exceptions: []Exception{},
	}
}

func loadBaseline(path string) (*Baseline, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read the baseline: %w", err)
	}
	return parseBaseline(src, path)
}

func parseBaseline(src []byte, name string) (*Baseline, error) {
	var b Baseline
	if err := json.Unmarshal(src, &b); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if b.Schema != 1 {
		return nil, fmt.Errorf("%s: schema %d is not 1", name, b.Schema)
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
	if b.Tools["definitions"] != definitions {
		problems = append(problems, fmt.Sprintf(
			"%s was measured by code whose definitions hash is %s, but code-smells %s measures with %s, so the numbers cannot be compared; use the skill version that wrote the baseline, or re-measure the baseline deliberately, with an issue and a review",
			baselinePath, b.Tools["definitions"], version, definitions))
	} else if !maps.Equal(b.Tools, s.Tools) || !maps.Equal(b.Metrics, metricDefinitions) {
		problems = append(problems, fmt.Sprintf(
			"the measuring tools or metric definitions differ from %s (baseline %v, now %v); change the baseline deliberately, with an issue and a review",
			baselinePath, b.Tools, s.Tools))
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
			notes = append(notes, fmt.Sprintf("%s %s: %s is %d, below its cap of %d; make smell-baseline can lower it", scope, metric, id, values[id], caps[id]))
		}
	}
	now, was := aggregate(values, limit), b.Aggregate[scope][metric]
	if now.Violations > was.Violations || now.Excess > was.Excess {
		problems = append(problems, fmt.Sprintf("%s %s: %d items over the limit by %d in total, more than the baseline's %d items by %d",
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

// runBaseline writes a candidate baseline with caps lowered to today's values and fixed
// items removed. It never raises a cap, adds an item, or changes limits, files or tools:
// if the code is over the baseline it refuses, and the candidate must be moved into
// place by hand.
func runBaseline() error {
	b, err := loadBaseline(baselinePath)
	if err != nil {
		return err
	}
	s, err := scan(b)
	if err != nil {
		return err
	}
	if problems, _ := compare(b, s); len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, "smell:", p)
		}
		return fmt.Errorf("refusing to write a candidate: the code is over %s; fix it, or raise the cap by hand with an issue and a review", baselinePath)
	}
	candidate := *b
	candidate.Caps, candidate.Aggregate = capsFor(b, s, b.Caps)
	lowered := loosenings(b, &candidate)
	if len(lowered) == 0 {
		fmt.Println("nothing to lower:", baselinePath, "already matches the code")
		return nil
	}
	if err := saveBaseline(candidatePath, &candidate); err != nil {
		return err
	}
	for _, key := range lowered {
		fmt.Println("lower:", key)
	}
	fmt.Printf("wrote %s; review it, then: mv %s %s\n", candidatePath, candidatePath, baselinePath)
	return nil
}

// guardAgainstMain stops a branch from hiding growth by raising its own baseline:
// anything looser than origin/main's .smells.json needs an exception. It reads the last
// fetched origin/main, since the check runs without the network.
func guardAgainstMain(b *Baseline) (problems, notes []string) {
	if exec.Command("git", "rev-parse", "--verify", "--quiet", "origin/main").Run() != nil {
		return []string{"origin/main is unknown, so the baseline cannot be compared with main's; run git fetch origin"}, nil
	}
	src, err := exec.Command("git", "show", "origin/main:"+baselinePath).Output()
	if err != nil {
		return nil, []string{"no " + baselinePath + " on origin/main yet, so the branch's own baseline is the only one checked"}
	}
	main, err := parseBaseline(src, "origin/main:"+baselinePath)
	if err != nil {
		return []string{err.Error()}, nil
	}
	for _, key := range loosenings(b, main) {
		if !excepted(b, key) {
			problems = append(problems, fmt.Sprintf("%s is looser than on origin/main; tighten it, or add an exception for it with a reason and an issue", key))
		}
	}
	return problems, nil
}

func excepted(b *Baseline, key string) bool {
	return slices.ContainsFunc(b.Exceptions, func(e Exception) bool {
		return e.Key == key && e.Reason != "" && e.Issue != ""
	})
}

// loosenings lists every way b allows more than old does.
func loosenings(b, old *Baseline) []string {
	var keys []string
	if !maps.Equal(b.Tools, old.Tools) || !maps.Equal(b.Metrics, old.Metrics) {
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
