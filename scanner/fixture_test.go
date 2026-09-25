package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// fixture copies testdata/<name> into a new git repository, commits it as origin/main,
// the default branch, and moves into it.
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
	git(t, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
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
	b, err := loadBaseline()
	if err != nil {
		t.Fatal(err)
	}
	edit(b)
	if err := saveBaseline(baselinePath, b); err != nil {
		t.Fatal(err)
	}
}

// firstBaseline writes the first baseline and moves it into place.
func firstBaseline(t *testing.T) {
	t.Helper()
	if err := runBaseline(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(candidatePath, baselinePath); err != nil {
		t.Fatal(err)
	}
}

// measured is the mixed fixture with its first baseline merged.
func measured(t *testing.T) {
	t.Helper()
	fixture(t, "mixed")
	firstBaseline(t)
	commit(t)
}

func TestFixturePasses(t *testing.T) {
	measured(t)
	b, err := loadBaseline()
	if err != nil {
		t.Fatal(err)
	}
	if b.Caps[production][complexity]["grade/grade.go:Letter"] == 0 {
		t.Fatalf("grade.go:Letter is not capped: %v", b.Caps)
	}
	if problems, _, err := check(); err != nil || len(problems) > 0 {
		t.Fatalf("the fixture fails its own baseline: %v %q", err, problems)
	}
	if err := runBaseline(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(candidatePath); err == nil {
		t.Fatal("baseline wrote a candidate though nothing can be lowered")
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
		{"other measuring code", "definitions 0123456789abcdef, dupl v1.0.0", func(t *testing.T) {
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
	b, measured, err := startBaseline()
	if err != nil || measured {
		t.Fatal(measured, err)
	}
	if strings.Join(b.Languages, ",") != "go,typescript" || strings.Join(b.Files.Include, ",") != "*.go,*.ts" {
		t.Fatalf("detected %v in %v", b.Languages, b.Files.Include)
	}
	if _, err := scan(b); err != nil {
		t.Fatal(err)
	}
}

func TestFirstBaselineDetectsLanguagesUnderInclude(t *testing.T) {
	fixture(t, "mixed")
	write(t, baselinePath, `{"files": {"include": ["*.go"], "exclude": ["vendor/"]}}`)
	if _, _, err := check(); err == nil || !strings.Contains(err.Error(), "no measurements yet") {
		t.Fatalf("check of an unmeasured baseline: %v", err)
	}
	firstBaseline(t)
	b, err := loadBaseline()
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

// TestProjectBunfigDoesNotRun checks that measuring TypeScript does not start Bun in the
// project, where a bunfig.toml could preload the project's own code.
func TestProjectBunfigDoesNotRun(t *testing.T) {
	measured(t)
	marker := filepath.Join(t.TempDir(), "ran")
	write(t, "bunfig.toml", "preload = [\"./pwn.js\"]\n")
	write(t, "pwn.js", "require(\"node:fs\").writeFileSync("+strconv.Quote(marker)+", \"\");\n")
	if problems, _, err := check(); err != nil || len(problems) > 0 {
		t.Fatalf("check: %v %q", err, problems)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the project's bunfig.toml preload ran inside the scanner")
	}
}

// TestShrinkTwice lowers Letter's cap with baseline, then shrinks it again without
// running baseline: its fingerprint is stale, which must not fail the check.
func TestShrinkTwice(t *testing.T) {
	measured(t)
	grade, err := os.ReadFile("grade/grade.go")
	if err != nil {
		t.Fatal(err)
	}
	once := strings.Replace(string(grade), "case score > 85 && curved:", "case score > 85:", 1)
	write(t, "grade/grade.go", once)
	firstBaseline(t)
	write(t, "grade/grade.go", strings.Replace(once, "\t} else if score >= 70 {\n\t\treturn \"C\"\n", "", 1))
	if b, _ := loadBaseline(); b.Caps[production][complexity]["grade/grade.go:Letter"] != 17 {
		t.Fatalf("Letter's cap is %d, want 17", b.Caps[production][complexity]["grade/grade.go:Letter"])
	}
	if problems, _, err := check(); err != nil || len(problems) > 0 {
		t.Fatalf("check after shrinking twice: %v %q", err, problems)
	}
}

// TestCapFollowsMovedCode moves the capped grade.go:Letter to another file, or renames
// it, without changing its body: the check passes, baseline moves the cap, and the
// moved cap is not looser than main's. Growing it after the move still fails.
func TestCapFollowsMovedCode(t *testing.T) {
	cases := []struct{ name, file, id string }{
		{"move", "grade/letter.go", "grade/letter.go:Letter"},
		{"rename", "grade/grade.go", "grade/grade.go:Mark"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			measured(t)
			grade, err := os.ReadFile("grade/grade.go")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove("grade/grade.go"); err != nil {
				t.Fatal(err)
			}
			write(t, c.file, strings.Replace(string(grade), "func Letter(", "func "+strings.TrimPrefix(c.id, c.file+":")+"(", 1))
			problems, notes, err := check()
			if err != nil || len(problems) > 0 {
				t.Fatalf("check after a %s: %v %q", c.name, err, problems)
			}
			if !strings.Contains(strings.Join(notes, "\n"), "grade/grade.go:Letter moved to "+c.id) {
				t.Fatalf("notes %q do not name the move", notes)
			}
			var out bytes.Buffer
			if err := review(&out, "origin/main"); err != nil || strings.Contains(out.String(), "new: production complexity "+c.id) {
				t.Fatalf("review calls the moved code new: %v\n%s", err, out.String())
			}
			firstBaseline(t)
			b, err := loadBaseline()
			if err != nil {
				t.Fatal(err)
			}
			if b.Caps[production][complexity][c.id] == 0 || b.Caps[production][complexity]["grade/grade.go:Letter"] != 0 {
				t.Fatalf("the cap did not move to %s: %v", c.id, b.Caps[production][complexity])
			}
			if problems, _, err := check(); err != nil || len(problems) > 0 {
				t.Fatalf("check with the moved cap: %v %q", err, problems)
			}
			moved, err := os.ReadFile(c.file)
			if err != nil {
				t.Fatal(err)
			}
			write(t, c.file, strings.Replace(string(moved), "\tif curved {\n", "\tif score < 0 {\n\t\tscore = 0\n\t}\n\tif curved {\n", 1))
			if problems, _, _ := check(); !strings.Contains(strings.Join(problems, "\n"), c.id+" grew") {
				t.Fatalf("growth after the %s passed: %q", c.name, problems)
			}
		})
	}
}

// TestRemeasure changes the tools main's baseline was measured with, as a new skill
// version does: baseline writes a re-measured candidate, and the check accepts it only
// in a branch that changes no measured code.
func TestRemeasure(t *testing.T) {
	measured(t)
	editBaseline(t, func(b *Baseline) {
		b.Tools["definitions"] = "0123456789abcdef"
		b.Caps[production][complexity]["grade/grade.go:Letter"] -= 3
	})
	commit(t)
	if err := runBaseline(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(candidatePath, baselinePath); err != nil {
		t.Fatal(err)
	}
	b, err := loadBaseline()
	if err != nil {
		t.Fatal(err)
	}
	if b.Tools["definitions"] != definitions || !strings.Contains(b.MeasuredBy, "code-smells") {
		t.Fatalf("the candidate was not re-measured: %v, %q", b.Tools, b.MeasuredBy)
	}
	problems, notes, err := check()
	if err != nil || len(problems) > 0 {
		t.Fatalf("check of a re-measure: %v %q", err, problems)
	}
	if !strings.Contains(strings.Join(notes, "\n"), "re-measures .smells.json") {
		t.Fatalf("notes %q do not name the re-measure", notes)
	}
	var out bytes.Buffer
	if err := review(&out, "origin/main"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "looser: caps/production/complexity/grade/grade.go:Letter (") {
		t.Fatalf("review does not show the old and new cap:\n%s", out.String())
	}
	editBaseline(t, func(b *Baseline) { b.Caps[production][complexity]["grade/grade.go:Letter"]++ })
	if problems, _, _ := check(); !strings.Contains(strings.Join(problems, "\n"), "not what smells baseline measures") {
		t.Fatalf("a re-measure with a raised cap passed: %q", problems)
	}
	editBaseline(t, func(b *Baseline) { b.Caps[production][complexity]["grade/grade.go:Letter"]-- })
	write(t, "grade/extra.go", "package grade\n\nfunc Extra() int { return 1 }\n")
	if problems, _, _ := check(); !strings.Contains(strings.Join(problems, "\n"), "differs from origin/main") {
		t.Fatalf("a re-measure with a code change passed: %q", problems)
	}
	if err := os.Remove("grade/extra.go"); err != nil {
		t.Fatal(err)
	}
	editBaseline(t, func(b *Baseline) { b.Files.Include = append(b.Files.Include, ":(exclude)go.mod") })
	commit(t)
	if err := runBaseline(); err != nil {
		t.Fatal(err)
	}
	editBaseline(t, func(b *Baseline) { b.Tools["definitions"] = "0123456789abcdef" })
	commit(t)
	if err := runBaseline(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(candidatePath, baselinePath); err != nil {
		t.Fatal(err)
	}
	if problems, _, err := check(); err != nil || len(problems) > 0 {
		t.Fatalf("check of a re-measure: %v %q", err, problems)
	}
	write(t, "go.mod", "module example.com/other\n\ngo 1.26\n")
	if problems, _, _ := check(); !strings.Contains(strings.Join(problems, "\n"), "differs from origin/main") {
		t.Fatalf("a re-measure with a go.mod change passed: %q", problems)
	}
}

func TestNoDefaultBranch(t *testing.T) {
	measured(t)
	git(t, "symbolic-ref", "--delete", "refs/remotes/origin/HEAD")
	if problems, _, _ := check(); !strings.Contains(strings.Join(problems, "\n"), "origin/HEAD is not set") {
		t.Fatalf("problems %q do not say the default branch is unknown", problems)
	}
}

// TestForgedMoveFails deletes the capped Letter, adds a different function over the
// limit, and hands it Letter's cap and fingerprint by hand: that is not a move.
func TestForgedMoveFails(t *testing.T) {
	measured(t)
	if err := os.Remove("grade/grade.go"); err != nil {
		t.Fatal(err)
	}
	write(t, "grade/other.go", "package grade\n\nfunc Other(a bool) int {\n"+repeat("if a {\n\t\ta = !a\n\t}", 16)+"\treturn 0\n}\n")
	editBaseline(t, func(b *Baseline) {
		caps := b.Caps[production][complexity]
		caps["grade/other.go:Other"] = caps["grade/grade.go:Letter"]
		delete(caps, "grade/grade.go:Letter")
		b.Fingerprints["grade/other.go:Other"] = b.Fingerprints["grade/grade.go:Letter"]
	})
	if problems, _, _ := check(); !strings.Contains(strings.Join(problems, "\n"), "caps/production/complexity/grade/other.go:Other is looser") {
		t.Fatalf("a forged move passed: %q", problems)
	}
}

// TestForgedFingerprintFails keeps Letter's cap key after deleting it, and rewrites its
// fingerprint to a different function's hash, so check would take that one for a move.
func TestForgedFingerprintFails(t *testing.T) {
	measured(t)
	if err := os.Remove("grade/grade.go"); err != nil {
		t.Fatal(err)
	}
	write(t, "grade/other.go", "package grade\n\nfunc Other(a bool) int {\n"+repeat("if a {\n\t\ta = !a\n\t}", 16)+"\treturn 0\n}\n")
	b, err := loadBaseline()
	if err != nil {
		t.Fatal(err)
	}
	s, err := scan(b)
	if err != nil {
		t.Fatal(err)
	}
	editBaseline(t, func(b *Baseline) { b.Fingerprints["grade/grade.go:Letter"] = s.Content["grade/other.go:Other"] })
	if problems, _, _ := check(); !strings.Contains(strings.Join(problems, "\n"), "fingerprints/grade/grade.go:Letter is for code that is gone") {
		t.Fatalf("a forged fingerprint passed: %q", problems)
	}
}
