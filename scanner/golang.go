package main

import (
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/mibk/dupl/job"
	"github.com/mibk/dupl/syntax"
	"github.com/uudashr/gocognit"
)

// scanGo measures every Go file and adds production packages to the import graph.
// A file that does not parse fails the scan rather than being skipped, and so does a
// scan that finds no files or no functions, since it would pass by measuring nothing.
func scanGo(s *Scan, r *Rules, paths []string, graph map[string]map[string]bool) error {
	if len(paths) == 0 {
		return fmt.Errorf("found no Go files to measure, but %s lists go; check files.include and files.exclude", baselinePath)
	}
	module, err := modulePath()
	if err != nil {
		return err
	}
	sources := map[string][]byte{}
	functions := 0
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sources[p] = src
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, src, 0)
		if err != nil {
			return fmt.Errorf("cannot measure %s: %w", p, err)
		}
		scope := r.scope(p)
		s.set(scope, fileLines, p, physicalLines(src))
		functions += goFunctions(s, scope, p, fset, f)
		if scope == production {
			goImports(graph, p, f, module)
		}
	}
	if functions == 0 {
		return fmt.Errorf("found no Go functions in %d files", len(paths))
	}
	goDuplicates(s, r, paths, sources)
	return nil
}

// goFunctions measures each function with a body and returns how many there were.
func goFunctions(s *Scan, scope, p string, fset *token.FileSet, f *ast.File) int {
	seen, n := map[string]int{}, 0
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		n++
		id := uniqueID(seen, p+":"+goName(fn))
		lines := fset.Position(fn.End()).Line - fset.Position(fn.Pos()).Line + 1
		s.set(scope, functionLines, id, lines)
		s.set(scope, complexity, id, gocognit.Complexity(fn))
	}
	return n
}

// goName is Name for a function and Type.Name for a method, whatever the receiver's
// pointer or type parameters.
func goName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	t := fn.Recv.List[0].Type
	for {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
		case *ast.IndexExpr:
			t = x.X
		case *ast.IndexListExpr:
			t = x.X
		case *ast.Ident:
			return x.Name + "." + fn.Name.Name
		default:
			return fn.Name.Name
		}
	}
}

func goImports(graph map[string]map[string]bool, p string, f *ast.File, module string) {
	dir := path.Dir(p)
	if graph[dir] == nil {
		graph[dir] = map[string]bool{}
	}
	for _, imp := range f.Imports {
		target, _ := strconv.Unquote(imp.Path.Value)
		if local, ok := strings.CutPrefix(target, module+"/"); ok {
			graph[dir][local] = true
		}
	}
}

func modulePath() (string, error) {
	src, err := os.ReadFile("go.mod")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(src), "\n") {
		if m, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(m), nil
		}
	}
	return "", fmt.Errorf("go.mod does not name a module")
}

// goDuplicates finds dupl's syntax clones of at least dupTokens tokens across all Go
// files, grouped the way the dupl command groups them. It records each group by its
// members and dupl's hash of the clone's syntax, and counts the lines each file has
// inside a clone.
func goDuplicates(s *Scan, r *Rules, paths []string, sources map[string][]byte) {
	files := make(chan string)
	go func() {
		for _, p := range paths {
			files <- p
		}
		close(files)
	}()
	tree, data, done := job.BuildTree(job.Parse(files))
	<-done
	tree.Update(&syntax.Node{Type: -1})
	groups := map[string][][]*syntax.Node{}
	for m := range tree.FindDuplOver(dupTokens) {
		match := syntax.FindSyntaxUnits(*data, m, dupTokens)
		groups[match.Hash] = append(groups[match.Hash], match.Frags...)
	}
	covered := map[string]map[int]bool{}
	for hash, frags := range groups {
		n := distinctFragments(frags)
		if n < 2 {
			continue
		}
		var members []string
		for _, frag := range frags {
			cover(covered, sources, frag)
			members = append(members, frag[0].Filename)
		}
		setGroup(s, r, members, hex.EncodeToString([]byte(hash)), n)
	}
	for file, lines := range covered {
		s.set(r.scope(file), duplicateLines, file, len(lines))
	}
}

// cover marks the lines a clone fragment spans in its file.
func cover(covered map[string]map[int]bool, sources map[string][]byte, frag []*syntax.Node) {
	file := frag[0].Filename
	first := lineAt(sources[file], frag[0].Pos)
	last := lineAt(sources[file], frag[len(frag)-1].End-1)
	if covered[file] == nil {
		covered[file] = map[int]bool{}
	}
	for l := first; l <= last; l++ {
		covered[file][l] = true
	}
}

func distinctFragments(frags [][]*syntax.Node) int {
	seen := map[string]bool{}
	for _, f := range frags {
		seen[fmt.Sprintf("%s:%d", f[0].Filename, f[0].Pos)] = true
	}
	return len(seen)
}

func lineAt(src []byte, offset int) int {
	return strings.Count(string(src[:offset]), "\n") + 1
}
