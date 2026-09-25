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
	"runtime"
	"slices"
	"strings"
)

// Baseline is .smells.json: the limits, and a cap for every item already over one.
// MeasuredBy names the skill build and Go that last wrote it, for people only.
// Fingerprints holds a hash of each capped item's content, so its cap can follow it.
type Baseline struct {
	Tools        map[string]string                    `json:"tools"`
	MeasuredBy   string                               `json:"measured_by,omitempty"`
	Languages    []string                             `json:"languages"`
	Files        Rules                                `json:"files"`
	Budgets      map[string]map[string]int            `json:"budgets"`
	Aggregate    map[string]map[string]Aggregate      `json:"aggregate"`
	Caps         map[string]map[string]map[string]int `json:"caps"`
	Fingerprints map[string]string                    `json:"fingerprints,omitempty"`
	Exceptions   []Exception                          `json:"exceptions"`
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
	moved := moves(caps, values, s.Content, b.Fingerprints)
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
			notes = append(notes, fmt.Sprintf("%s %s: %s moved to %s; smells baseline moves its cap", scope, metric, id, to))
		} else if values[id] < caps[id] {
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

// moves pairs each item in now that caps does not cap with a capped item that is gone
// from now and had the same content (fingerprints), so a cap follows code that was
// moved or renamed without growing.
func moves(caps, now map[string]int, content, fingerprints map[string]string) map[string]string {
	gone := map[string][]string{}
	for _, id := range sortedKeys(caps) {
		if _, ok := now[id]; !ok && fingerprints[id] != "" {
			gone[fingerprints[id]] = append(gone[fingerprints[id]], id)
		}
	}
	pairs := map[string]string{}
	for _, id := range sortedKeys(now) {
		from := gone[content[id]]
		if _, ok := caps[id]; ok || len(from) == 0 {
			continue
		}
		pairs[id], gone[content[id]] = from[0], from[1:]
	}
	return pairs
}

// capsFor caps every item now over its limit at its current value, and records its
// fingerprint. When lowering, an item keeps a cap only if it had one or moved from one;
// compare has already refused anything new.
func capsFor(b *Baseline, s *Scan, lowering bool) (map[string]map[string]map[string]int, map[string]map[string]Aggregate, map[string]string) {
	caps := map[string]map[string]map[string]int{}
	aggregates := map[string]map[string]Aggregate{}
	var fingerprints map[string]string
	for scope, budgets := range b.Budgets {
		caps[scope], aggregates[scope] = map[string]map[string]int{}, map[string]Aggregate{}
		for metric, limit := range budgets {
			values := s.Values[scope][metric]
			caps[scope][metric] = map[string]int{}
			aggregates[scope][metric] = aggregate(values, limit)
			moved := moves(b.Caps[scope][metric], values, s.Content, b.Fingerprints)
			for id, v := range values {
				_, had := b.Caps[scope][metric][id]
				_, isMove := moved[id]
				if v <= limit || lowering && !had && !isMove {
					continue
				}
				caps[scope][metric][id] = v
				if s.Content[id] != "" {
					if fingerprints == nil {
						fingerprints = map[string]string{}
					}
					fingerprints[id] = s.Content[id]
				}
			}
		}
	}
	return caps, aggregates, fingerprints
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
	candidate.MeasuredBy = measuredBy()
	switch {
	case !measured:
		candidate.Tools = s.Tools
		candidate.Caps, candidate.Aggregate, candidate.Fingerprints = capsFor(b, s, false)
	case !maps.Equal(b.Tools, s.Tools):
		candidate.Tools = s.Tools
		candidate.Caps, candidate.Aggregate, candidate.Fingerprints = capsFor(b, s, false)
		fmt.Printf("re-measured with %s; %s was measured with %s\n", formatMap(s.Tools), baselinePath, formatMap(b.Tools))
		fmt.Printf("Against %s: ", baselinePath)
		printChanges(os.Stdout, b, &candidate)
		fmt.Println("Commit only this file, on a branch from the latest default branch: the check refuses a re-measure whose measured code differs from the default branch.")
	default:
		problems, notes := compare(b, s)
		if len(problems) > 0 {
			for _, p := range problems {
				fmt.Fprintln(os.Stderr, "smell:", p)
			}
			return fmt.Errorf("did not write %s, because the code is over %s; fix the code, or raise the cap by hand with an exception", candidatePath, baselinePath)
		}
		candidate.Caps, candidate.Aggregate, candidate.Fingerprints = capsFor(b, s, true)
		if reflect.DeepEqual([]any{b.Caps, b.Aggregate, b.Fingerprints}, []any{candidate.Caps, candidate.Aggregate, candidate.Fingerprints}) {
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

func measuredBy() string {
	return fmt.Sprintf("code-smells %s, %s", version, runtime.Version())
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
// exception. A baseline measured by other tools than ref's is a re-measure: its caps
// and totals cannot be compared with ref's, so it is accepted only when they are what
// scan s measures and the measured code matches ref's. It reads what was last
// fetched, so the check needs no network.
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
	// A cap follows only content that is really there, never a fingerprint written by
	// hand: the guard pairs moves by the code's own hashes, and check pairs them by
	// fingerprints that must be the code's or ref's.
	for _, id := range sortedKeys(b.Fingerprints) {
		if f := b.Fingerprints[id]; f != s.Content[id] && f != old.Fingerprints[id] {
			problems = append(problems, fmt.Sprintf("fingerprints/%s matches neither the code nor %s; only smells baseline writes fingerprints", id, ref))
		}
	}
	actual := *b
	actual.Fingerprints = s.Content
	keys := loosenings(&actual, old)
	if !maps.Equal(b.Tools, old.Tools) {
		changed, err := codeChanged(b, ref)
		if err != nil {
			return old, []string{err.Error()}, nil
		}
		if changed {
			return old, []string{fmt.Sprintf("this branch re-measures %s, but its measured code differs from %s; re-measure in a change of its own, on top of %s", baselinePath, ref, ref)}, nil
		}
		if caps, aggregates, _ := capsFor(b, s, false); !reflect.DeepEqual([]any{caps, aggregates}, []any{b.Caps, b.Aggregate}) {
			return old, []string{fmt.Sprintf("this branch re-measures %s, but its caps are not what smells baseline measures; run smells baseline and move the candidate into place", baselinePath)}, nil
		}
		notes = append(notes, fmt.Sprintf("this branch re-measures %s with %s (on %s: %s); smells review --base %s shows the old and new numbers",
			baselinePath, formatMap(b.Tools), ref, formatMap(old.Tools), ref))
		keys = slices.DeleteFunc(keys, func(k string) bool {
			return k == "tools" || strings.HasPrefix(k, "caps/") || strings.HasPrefix(k, "aggregate/")
		})
	}
	for _, key := range keys {
		if !excepted(b, key) {
			problems = append(problems, fmt.Sprintf("%s is looser than on %s; tighten it, or add an exception for it with a reason and an issue", key, ref))
		}
	}
	return old, problems, notes
}

// codeChanged says whether any file under files.include, or go.mod, which decides what
// counts as a local import, differs from ref, including files git does not track yet.
// go.mod is checked on its own, so an exclude pathspec in files.include cannot hide it.
func codeChanged(b *Baseline, ref string) (bool, error) {
	for _, paths := range [][]string{{"go.mod"}, b.Files.Include} {
		if exec.Command("git", append([]string{"diff", "--quiet", ref, "--"}, paths...)...).Run() != nil {
			return true, nil
		}
	}
	args := append([]string{"ls-files", "--others", "--exclude-standard", "--"}, b.Files.Include...)
	out, err := exec.Command("git", args...).Output()
	return len(out) > 0, err
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
	oldCaps, caps := old.Caps[scope][metric], b.Caps[scope][metric]
	moved := moves(oldCaps, caps, b.Fingerprints, old.Fingerprints)
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
