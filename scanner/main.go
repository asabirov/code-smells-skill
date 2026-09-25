// Command smells measures how long, complex, repeated and tangled a repository's code
// is, and compares the result with the committed .smells.json. Code that is over a
// limit today may shrink but not grow, and new code must stay within the limits.
//
//	smells check                  compare the code with .smells.json
//	smells report [dir]           print the totals and the worst functions
//	smells baseline               write .smells.candidate.json: the first baseline, or lower caps
//	smells review [--base ref]    show what a branch changed against ref; never fails on smells
package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
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

const usage = "usage: smells check | report [dir] | baseline | review [--base ref]"

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
	switch {
	case len(args) == 1 && args[0] == "check":
		return ".", runCheck, nil
	case len(args) == 1 && args[0] == "baseline":
		return ".", runBaseline, nil
	case len(args) == 1 && args[0] == "report":
		return ".", runReport, nil
	case len(args) == 2 && args[0] == "report":
		return args[1], runReport, nil
	case len(args) == 1 && args[0] == "review":
		return ".", func() error { return review(os.Stdout, "origin/main") }, nil
	case len(args) == 3 && args[0] == "review" && args[1] == "--base":
		return ".", func() error { return review(os.Stdout, args[2]) }, nil
	}
	return "", nil, fmt.Errorf("%s", usage)
}

// chdirRoot moves to the top of the git checkout holding dir, so every path is
// relative to the repository.
func chdirRoot(dir string) error {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return fmt.Errorf("%s is not inside a git checkout: %w", dir, err)
	}
	return os.Chdir(strings.TrimSpace(string(out)))
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
		return fmt.Errorf("the smell check failed with %d problem(s); the limits are in %s", len(problems), baselinePath)
	}
	fmt.Printf("smell check passed (code-smells %s): nothing grew past its limit in %s\n", version, baselinePath)
	return nil
}

// check measures the code and lists what breaks the baseline, or loosens it compared
// with main.
func check() (problems, notes []string, err error) {
	b, err := loadBaseline()
	if err != nil {
		return nil, nil, err
	}
	s, err := scan(b)
	if err != nil {
		return nil, nil, err
	}
	problems, notes = compare(b, s)
	_, looser, note := guard(b, "origin/main")
	return append(problems, looser...), append(notes, note...), nil
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
