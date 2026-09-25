package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fixture copies testdata/<name> into a new git repository, commits it as origin/main,
// and moves into it.
func fixture(t *testing.T, name string) {
	t.Helper()
	src, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	git(t, "init", "-q")
	commit(t)
}

// commit commits everything and makes it origin/main, as if it had merged.
func commit(t *testing.T) {
	t.Helper()
	git(t, "add", "-A")
	git(t, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-qm", "fixture")
	git(t, "update-ref", "refs/remotes/origin/main", "HEAD")
}

func git(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, name, src string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

func editBaseline(t *testing.T, edit func(*Baseline)) {
	t.Helper()
	b, err := loadBaseline(baselinePath)
	if err != nil {
		t.Fatal(err)
	}
	edit(b)
	if err := saveBaseline(baselinePath, b); err != nil {
		t.Fatal(err)
	}
}

// measured is the mixed fixture with its first baseline merged.
func measured(t *testing.T) {
	t.Helper()
	fixture(t, "mixed")
	if err := runInit(); err != nil {
		t.Fatal(err)
	}
	commit(t)
}

func TestFixturePasses(t *testing.T) {
	measured(t)
	b, err := loadBaseline(baselinePath)
	if err != nil {
		t.Fatal(err)
	}
	if b.Caps[production][complexity]["grade/grade.go:Letter"] == 0 {
		t.Fatalf("grade.go:Letter is not capped: %v", b.Caps)
	}
	if problems, _, err := check(); err != nil || len(problems) > 0 {
		t.Fatalf("the fixture fails its own baseline: %v %q", err, problems)
	}
	if err := runInit(); err == nil || !strings.Contains(err.Error(), "already measured") {
		t.Fatalf("init over a measured baseline: %v", err)
	}
}

func TestFixtureFailures(t *testing.T) {
	describe, err := os.ReadFile("testdata/mixed/grade/describe.go")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, want string
		change     func(t *testing.T)
	}{
		{"go syntax error", "cannot measure grade/broken.go", func(t *testing.T) {
			write(t, "grade/broken.go", "package grade\n\nfunc broken( {\n")
		}},
		{"typescript syntax error", "web/broken.ts", func(t *testing.T) {
			write(t, "web/broken.ts", "export function broken( {\n")
		}},
		{"a language measures nothing", "found no TypeScript files", func(t *testing.T) {
			editBaseline(t, func(b *Baseline) { b.Files.Include = []string{"*.go", "ui/*.ts"} })
		}},
		{"an unknown language", `the language "cobol"`, func(t *testing.T) {
			editBaseline(t, func(b *Baseline) { b.Languages = append(b.Languages, "cobol") })
		}},
		{"new duplicate group", "duplicate_groups", func(t *testing.T) {
			write(t, "grade/copy.go", strings.Replace(string(describe), "func Describe(", "func Summarize(", 1))
		}},
		{"other measuring code", "definitions hash is 0123456789abcdef, but code-smells dev measures with " + definitions, func(t *testing.T) {
			editBaseline(t, func(b *Baseline) { b.Tools["definitions"] = "0123456789abcdef" })
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			measured(t)
			c.change(t)
			problems, _, err := check()
			got := strings.Join(problems, "\n")
			if err != nil {
				got = err.Error()
			}
			if !strings.Contains(got, c.want) {
				t.Fatalf("got %q, want it to contain %q", got, c.want)
			}
		})
	}
}

func TestReviewShowsWithoutFailing(t *testing.T) {
	measured(t)
	write(t, "grade/more.go", "package grade\n\nfunc Worse(a, b, c bool) int {\n"+
		strings.Repeat("\tif a {\n\t\tif b {\n\t\t\tif c {\n\t\t\t\treturn 1\n\t\t\t}\n\t\t}\n\t}\n", 3)+"\treturn 0\n}\n")
	editBaseline(t, func(b *Baseline) {
		b.Caps[production][complexity]["grade/grade.go:Letter"] += 5
		b.Exceptions = append(b.Exceptions, Exception{
			Key: "caps/production/complexity/grade/grade.go:Letter", Reason: "curving needs two more rules", Issue: "example/mixed#7",
		})
	})
	var out bytes.Buffer
	if err := review(&out, "origin/main"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"The check fails with 2 problem(s)",
		"looser: caps/production/complexity/grade/grade.go:Letter (",
		"curving needs two more rules (example/mixed#7)",
		"new: production complexity grade/more.go:Worse is",
		"grade/grade.go:Letter",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("review lacks %q:\n%s", want, out.String())
		}
	}
}

func TestReportWithoutBaseline(t *testing.T) {
	fixture(t, "mixed")
	if err := os.Remove(baselinePath); err != nil {
		t.Fatal(err)
	}
	b, err := reportBaseline()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(b.Languages, ",") != "go,typescript" || strings.Join(b.Files.Include, ",") != "*.go,*.ts" {
		t.Fatalf("detected %v in %v", b.Languages, b.Files.Include)
	}
	if _, err := scan(b); err != nil {
		t.Fatal(err)
	}
}

func TestInitDetectsLanguagesUnderInclude(t *testing.T) {
	fixture(t, "mixed")
	write(t, baselinePath, `{"files": {"include": ["*.go"], "exclude": ["vendor/"]}}`)
	if err := runInit(); err != nil {
		t.Fatal(err)
	}
	b, err := loadBaseline(baselinePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(b.Languages, ",") != "go" {
		t.Fatalf("languages %v, want only go", b.Languages)
	}
}

func TestReviewUsesBaseLimits(t *testing.T) {
	measured(t)
	grade, err := os.ReadFile("grade/grade.go")
	if err != nil {
		t.Fatal(err)
	}
	write(t, "grade/grade.go", strings.Replace(string(grade), "\tif curved {\n", "\tif score < 0 {\n\t\tscore = 0\n\t}\n\tif curved {\n", 1))
	editBaseline(t, func(b *Baseline) { b.Budgets[production][complexity] = 100 })
	var out bytes.Buffer
	if err := review(&out, "origin/main"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "grew: production complexity grade/grade.go:Letter") {
		t.Fatalf("a raised limit hid growth:\n%s", out.String())
	}
}
