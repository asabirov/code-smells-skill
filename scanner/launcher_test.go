package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
)

// The build id names the scanner's cache folder, so the same source must give the same
// id on every host. GNU wc prints a bare size and BSD wc pads it, and locales sort file
// names differently (asabirov/code-smells-skill#9).
func TestBuildIDIgnoresHost(t *testing.T) {
	gnu := buildID(t, `/usr/bin/wc "$@" | tr -d ' '`, "C")
	bsd := buildID(t, `printf '%8s\n' "$(/usr/bin/wc "$@" | tr -d ' ')"`, "en_US.UTF-8")
	if gnu != bsd {
		t.Errorf("build id is %s with GNU wc and the C locale, but %s with BSD wc and en_US.UTF-8", gnu, bsd)
	}
}

// buildID runs bin/smells with the given wc and locale, and a go that fails at once, so
// the launcher prints the build id and stops before it builds anything.
func buildID(t *testing.T, wc, locale string) string {
	dir := t.TempDir()
	for name, body := range map[string]string{"wc": wc, "go": "exit 1"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("../bin/smells")
	cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "LC_ALL="+locale, "XDG_CACHE_HOME="+filepath.Join(dir, "cache"))
	out, _ := cmd.CombinedOutput()
	m := regexp.MustCompile(`first run of build ([0-9a-f]{16})`).FindSubmatch(out)
	if m == nil {
		t.Fatalf("bin/smells printed no build id:\n%s", out)
	}
	return string(m[1])
}
