package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// tsDir holds ts-scan.ts and the TypeScript compiler its bun.lock pins. bin/smells
// installs it next to the scanner's own folder; the tests point it at the source.
var tsDir = func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(exe), "..", "typescript")
}()

type tsResult struct {
	TypeScript string `json:"typescript"`
	Files      []struct {
		File      string   `json:"file"`
		Lines     int      `json:"lines"`
		Imports   []string `json:"imports"`
		Functions []struct {
			Name       string `json:"name"`
			Lines      int    `json:"lines"`
			Complexity int    `json:"complexity"`
		} `json:"functions"`
	} `json:"files"`
	Duplicates map[string]int `json:"duplicates"`
	Groups     []struct {
		Fingerprint string   `json:"fingerprint"`
		Files       []string `json:"files"`
		Copies      int      `json:"copies"`
	} `json:"groups"`
}

// scanTS runs ts-scan.ts under Bun with the TypeScript compiler that its bun.lock pins,
// and returns that compiler's version. Like scanGo, it fails on a syntax error and on
// finding no files or no functions.
func scanTS(s *Scan, r *Rules, paths []string, graph map[string]map[string]bool) (string, error) {
	if len(paths) == 0 {
		return "", fmt.Errorf("found no TypeScript files to measure, but %s lists typescript; check files.include and files.exclude", baselinePath)
	}
	bun, err := exec.LookPath("bun")
	if err != nil {
		return "", fmt.Errorf("bun is not on PATH, and measuring TypeScript needs it")
	}
	if _, err := os.Stat(filepath.Join(tsDir, "node_modules", "typescript", "package.json")); err != nil {
		return "", fmt.Errorf("the TypeScript compiler is not installed in %s; install Bun, then run bin/smells again", tsDir)
	}
	input, _ := json.Marshal(paths)
	cmd := exec.Command(bun, filepath.Join(tsDir, "ts-scan.ts"))
	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("measuring TypeScript failed (%w): %s", err, bytes.TrimSpace(stderr.Bytes()))
	}
	var res tsResult
	if err := json.Unmarshal(out, &res); err != nil {
		return "", fmt.Errorf("could not read the TypeScript measurements: %w", err)
	}
	functions := 0
	for _, f := range res.Files {
		functions += len(f.Functions)
		scope := r.scope(f.File)
		s.set(scope, fileLines, f.File, f.Lines)
		for _, fn := range f.Functions {
			s.set(scope, functionLines, f.File+":"+fn.Name, fn.Lines)
			s.set(scope, complexity, f.File+":"+fn.Name, fn.Complexity)
		}
		if scope == production {
			tsImports(graph, r, f.File, f.Imports)
		}
	}
	if functions == 0 {
		return "", fmt.Errorf("found no TypeScript functions in %d files", len(paths))
	}
	for file, n := range res.Duplicates {
		s.set(r.scope(file), duplicateLines, file, n)
	}
	for _, g := range res.Groups {
		setGroup(s, r, g.Files, g.Fingerprint, g.Copies)
	}
	return res.TypeScript, nil
}

// tsImports adds a production module's imports of other production modules.
func tsImports(graph map[string]map[string]bool, r *Rules, file string, imports []string) {
	if graph[file] == nil {
		graph[file] = map[string]bool{}
	}
	for _, target := range imports {
		if r.scope(target) == production {
			graph[file][target] = true
		}
	}
}
