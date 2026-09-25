package main

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// The languages this skill measures, and the file extensions each one covers.
const (
	golang     = "go"
	typescript = "typescript"
)

var extensions = map[string][]string{
	golang:     {".go"},
	typescript: {".ts"},
}

// languageOf names the language of a file by its extension, or returns "".
func languageOf(file string) string {
	if strings.HasSuffix(file, ".d.ts") {
		return ""
	}
	for lang, exts := range extensions {
		if slices.Contains(exts, path.Ext(file)) {
			return lang
		}
	}
	return ""
}

// Rules says which files are measured and which of them are tests. Include holds git
// pathspecs such as "*.go" or "ui/*.ts"; Exclude holds path prefixes.
type Rules struct {
	Include []string `json:"include"`
	Exclude []string `json:"exclude"`
	Tests   []string `json:"tests"`
	tests   []*regexp.Regexp
}

func (r *Rules) compile() error {
	r.tests = nil
	for _, t := range r.Tests {
		re, err := regexp.Compile(t)
		if err != nil {
			return fmt.Errorf("test pattern %q: %w", t, err)
		}
		r.tests = append(r.tests, re)
	}
	return nil
}

func (r *Rules) scope(path string) string {
	for _, re := range r.tests {
		if re.MatchString(path) {
			return tests
		}
	}
	return production
}

func (r *Rules) excluded(path string) bool {
	for _, prefix := range r.Exclude {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// checkLanguages refuses a baseline that names no language or one this skill cannot
// measure, since either would let a check pass by measuring nothing.
func checkLanguages(langs []string) error {
	if len(langs) == 0 {
		return fmt.Errorf("%s lists no languages; add \"languages\": [\"go\"] or similar", baselinePath)
	}
	for _, l := range langs {
		if extensions[l] == nil {
			return fmt.Errorf("%s lists the language %q; this skill measures %s", baselinePath, l, strings.Join(sortedKeys(extensions), " and "))
		}
	}
	return nil
}

// listFiles returns, for each listed language, the files git knows about under the
// include pathspecs, tracked or new but not ignored, so a file does not escape the
// check by being left out of a commit.
func listFiles(r *Rules, langs []string) (map[string][]string, error) {
	if len(r.Include) == 0 {
		return nil, fmt.Errorf("files.include in %s is empty; list the paths to measure, such as \"*.go\"", baselinePath)
	}
	out, err := gitFiles(r.Include)
	if err != nil {
		return nil, err
	}
	files := map[string][]string{}
	for _, p := range out {
		lang := languageOf(p)
		if !slices.Contains(langs, lang) || r.excluded(p) {
			continue
		}
		if _, err := os.Stat(p); err != nil {
			continue // deleted in the working tree
		}
		files[lang] = append(files[lang], p)
	}
	for _, list := range files {
		sort.Strings(list)
	}
	return files, nil
}

func gitFiles(pathspecs []string) ([]string, error) {
	args := append([]string{"ls-files", "-z", "--cached", "--others", "--exclude-standard", "--"}, pathspecs...)
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	var paths []string
	seen := map[string]bool{}
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" && !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// detectFiles fills in the languages and include paths of a repository that has no
// baseline yet: every language with a file under the include paths, or anywhere when
// there are none, and every file of those languages.
func detectFiles(b *Baseline) error {
	if len(b.Languages) > 0 && len(b.Files.Include) > 0 {
		return nil
	}
	all, err := gitFiles(b.Files.Include)
	if err != nil {
		return err
	}
	found := map[string]bool{}
	for _, p := range all {
		if lang := languageOf(p); lang != "" && !b.Files.excluded(p) {
			found[lang] = true
		}
	}
	if len(b.Languages) == 0 {
		b.Languages = sortedKeys(found)
	}
	if len(b.Files.Include) == 0 {
		for _, lang := range b.Languages {
			for _, ext := range extensions[lang] {
				b.Files.Include = append(b.Files.Include, "*"+ext)
			}
		}
	}
	if len(b.Languages) == 0 {
		return fmt.Errorf("found no files in a language this skill measures (%s)", strings.Join(sortedKeys(extensions), ", "))
	}
	return nil
}
