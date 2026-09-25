// Command smells measures how large, branchy, repeated and entangled a repository's
// code is, and holds the result to the committed .smells.json: code that is over a limit
// today may shrink but not grow, and new code must stay within the limits.
//
//	smells check                  compare the code with .smells.json
//	smells report [dir]           print the totals and the worst production hotspots
//	smells baseline               write .smells.candidate.json with lowered caps
//	smells review [--base ref]    show what a branch changed against ref; never fails on smells
//	smells scan                   print every measurement as JSON
//	smells init                   write the first .smells.json; refuses if one is measured
package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	baselinePath  = ".smells.json"
	candidatePath = ".smells.candidate.json"
)

// version names the skill build that runs, as bin/smells passes it.
var version = cmp.Or(os.Getenv("CODE_SMELLS_VERSION"), "dev")

const usage = "usage: smells check | report [dir] | baseline | review [--base ref] | scan | init"

func main() {
	dir, run, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	err = chdirRoot(dir)
	if err == nil {
		err = run()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "smells (code-smells %s): %v\n", version, err)
		os.Exit(1)
	}
}

// parseArgs picks the command and the directory it runs in.
func parseArgs(args []string) (dir string, run func() error, err error) {
	commands := map[string]func() error{
		"check":    runCheck,
		"baseline": runBaseline,
		"scan":     runScan,
		"init":     runInit,
	}
	switch {
	case len(args) == 1 && commands[args[0]] != nil:
		return ".", commands[args[0]], nil
	case len(args) <= 2 && len(args) > 0 && args[0] == "report":
		dir = "."
		if len(args) == 2 {
			dir = args[1]
		}
		return dir, runReport, nil
	case len(args) == 1 && args[0] == "review":
		return ".", func() error { return review(os.Stdout, "origin/main") }, nil
	case len(args) == 3 && args[0] == "review" && args[1] == "--base":
		return ".", func() error { return review(os.Stdout, args[2]) }, nil
	}
	return "", nil, fmt.Errorf("%s", usage)
}

// chdirRoot moves to the top of the git checkout holding dir, so every path is
// repository-relative.
func chdirRoot(dir string) error {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return fmt.Errorf("%s is not inside a git checkout: %w", dir, err)
	}
	return os.Chdir(strings.TrimSpace(string(out)))
}

func runScan() error {
	b, err := loadBaseline(baselinePath)
	if err != nil {
		return err
	}
	s, err := scan(b)
	if err != nil {
		return err
	}
	return writeJSON(os.Stdout, s)
}

func runCheck() error {
	problems, notes, err := check()
	if err != nil {
		return err
	}
	for _, n := range notes {
		fmt.Println("note:", n)
	}
	if len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, "smell:", p)
		}
		return fmt.Errorf("smell-check failed with %d problem(s); see %s for the limits", len(problems), baselinePath)
	}
	fmt.Printf("smell-check passed (code-smells %s): nothing grew past its limit in %s\n", version, baselinePath)
	return nil
}

// check scans the code and lists what breaks the baseline or loosens it against main.
func check() (problems, notes []string, err error) {
	b, err := loadBaseline(baselinePath)
	if err != nil {
		return nil, nil, err
	}
	s, err := scan(b)
	if err != nil {
		return nil, nil, err
	}
	problems, notes = compare(b, s)
	guard, guardNote := guardAgainstMain(b)
	return append(problems, guard...), append(notes, guardNote...), nil
}

// runInit measures the code and writes the first .smells.json. It starts from a
// .smells.json that holds only languages and files, when there is one, and otherwise
// measures every file in a language this skill knows.
func runInit() error {
	b := defaultBaseline()
	if src, err := os.ReadFile(baselinePath); err == nil {
		if err := json.Unmarshal(src, b); err != nil {
			return fmt.Errorf("%s: %w", baselinePath, err)
		}
		if b.Tools != nil || b.Caps != nil {
			return fmt.Errorf("%s is already measured; run smells baseline to lower it", baselinePath)
		}
	}
	b.Schema, b.Metrics = 1, metricDefinitions
	if err := detectFiles(b); err != nil {
		return err
	}
	s, err := scan(b)
	if err != nil {
		return err
	}
	b.Tools = s.Tools
	b.Caps, b.Aggregate = capsFor(b, s, nil)
	if err := saveBaseline(baselinePath, b); err != nil {
		return err
	}
	fmt.Printf("wrote %s measuring %s in %s; review it and commit it\n", baselinePath, strings.Join(b.Languages, " and "), strings.Join(b.Files.Include, " "))
	return nil
}

func writeJSON(f *os.File, v any) error {
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
