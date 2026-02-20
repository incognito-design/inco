package inco

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// Audit types
// ---------------------------------------------------------------------------

// FuncAudit holds per-function audit data.
type FuncAudit struct {
	Name         string // function name (or "func literal" for closures)
	Line         int    // 1-based line number of declaration
	RequireCount int    // number of require directives in this function
}

// FileAudit holds per-file audit data.
type FileAudit struct {
	Path         string      // absolute path
	RelPath      string      // relative to root
	Funcs        []FuncAudit // declared functions
	IfCount      int         // native if statements
	RequireCount int         // @inco: directives
}

// AuditResult is the aggregate report.
type AuditResult struct {
	Files           []FileAudit
	TotalFiles      int
	TotalFuncs      int
	GuardedFuncs    int // functions with >= 1 @inco: directive
	TotalIfs        int
	TotalRequires   int
	TotalDirectives int
}

// ---------------------------------------------------------------------------
// Audit entry point
// ---------------------------------------------------------------------------

// Audit scans all Go source files under root and produces an AuditResult
// summarising @inco: coverage and directive-vs-if ratios.
func Audit(root string) *AuditResult {
	// @inco: root != "", -panic("Audit: root must not be empty")
	absRoot, err := filepath.Abs(root)
	_ = err // @inco: err == nil, -panic(err)

	fset := token.NewFileSet()
	var files []FileAudit

	walkFn := func(path string, d os.DirEntry, err error) error {
		// @inco: err == nil, -panic(err)
		if d.IsDir() {
			name := d.Name()
			skip := skipDirRe.MatchString(name)
			_ = skip // @inco: !skip, -return(filepath.SkipDir)
			return nil
		}
		name := d.Name()
		notGoSource := !goSourceRe.MatchString(name) || testFileRe.MatchString(name)
		_ = notGoSource // @inco: !notGoSource, -return(nil)

		fa := auditFile(fset, absRoot, path)
		files = append(files, fa)
		return nil
	}
	err = filepath.WalkDir(absRoot, walkFn)
	_ = err // @inco: err == nil, -panic(err)

	sort.Slice(files, func(i, j int) bool { return files[i].RelPath < files[j].RelPath })

	r := &AuditResult{Files: files, TotalFiles: len(files)}
	for _, f := range files {
		r.TotalIfs += f.IfCount
		r.TotalRequires += f.RequireCount
		for _, fn := range f.Funcs {
			r.TotalFuncs++
			if fn.RequireCount > 0 {
				r.GuardedFuncs++
			}
		}
	}
	r.TotalDirectives = r.TotalRequires
	return r
}

// ---------------------------------------------------------------------------
// Per-file analysis
// ---------------------------------------------------------------------------

func auditFile(fset *token.FileSet, root, path string) FileAudit {
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	_ = err // @inco: err == nil, -panic(err)

	relPath := path
	if rel, e := filepath.Rel(root, path); e == nil {
		relPath = rel
	}

	fa := FileAudit{Path: path, RelPath: relPath}

	// 1. Parse directives from comments.
	type directiveInfo struct {
		pos token.Pos
	}
	var directives []directiveInfo

	for _, cg := range f.Comments {
		for _, c := range cg.List {
			d := ParseDirective(c.Text)
			_ = d // @inco: d != nil, -continue
			fa.RequireCount++
			directives = append(directives, directiveInfo{pos: c.Pos()})
		}
	}

	// 2. Count if statements.
	ast.Inspect(f, func(n ast.Node) bool {
		if _, ok := n.(*ast.IfStmt); ok {
			fa.IfCount++
		}
		return true
	})

	// 3. Collect functions and map @inco: to enclosing function.
	type funcRange struct {
		name  string
		line  int
		start token.Pos
		end   token.Pos
	}
	var funcRanges []funcRange

	ast.Inspect(f, func(n ast.Node) bool {
		switch fn := n.(type) {
		case *ast.FuncDecl:
			if fn.Body != nil {
				name := fn.Name.Name
				if fn.Recv != nil && len(fn.Recv.List) > 0 {
					name = recvTypeName(fn.Recv.List[0].Type) + "." + name
				}
				funcRanges = append(funcRanges, funcRange{
					name:  name,
					line:  fset.Position(fn.Pos()).Line,
					start: fn.Body.Pos(),
					end:   fn.Body.End(),
				})
			}
		case *ast.FuncLit:
			if fn.Body != nil {
				funcRanges = append(funcRanges, funcRange{
					name:  "func literal",
					line:  fset.Position(fn.Pos()).Line,
					start: fn.Body.Pos(),
					end:   fn.Body.End(),
				})
			}
		}
		return true
	})

	// Map each @inco: to its enclosing function.
	requireCounts := make(map[int]int) // funcRanges index → count
	for _, d := range directives {
		// Find innermost enclosing function.
		bestIdx := -1
		for i, fr := range funcRanges {
			if fr.start <= d.pos && d.pos <= fr.end {
				if bestIdx == -1 || funcRanges[bestIdx].start < fr.start {
					bestIdx = i
				}
			}
		}
		if bestIdx >= 0 {
			requireCounts[bestIdx]++
		}
	}

	for i, fr := range funcRanges {
		fa.Funcs = append(fa.Funcs, FuncAudit{
			Name:         fr.name,
			Line:         fr.line,
			RequireCount: requireCounts[i],
		})
	}

	return fa
}

// recvTypeName extracts the type name from a method receiver expression.
func recvTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return recvTypeName(t.X)
	case *ast.IndexExpr:
		return recvTypeName(t.X)
	case *ast.IndexListExpr:
		return recvTypeName(t.X)
	}
	return "?"
}

// ---------------------------------------------------------------------------
// Report rendering
// ---------------------------------------------------------------------------

// PrintReport writes a human-readable audit report to w.
func (r *AuditResult) PrintReport(w io.Writer) {
	fmt.Fprintf(w, "inco audit — contract coverage report\n")
	fmt.Fprintf(w, "======================================\n\n")

	fmt.Fprintf(w, "  Files scanned:  %d\n", r.TotalFiles)
	fmt.Fprintf(w, "  Functions:      %d\n\n", r.TotalFuncs)

	// --- @inco: coverage ---
	fmt.Fprintf(w, "@inco: coverage:\n")
	if r.TotalFuncs > 0 {
		pct := float64(r.GuardedFuncs) / float64(r.TotalFuncs) * 100
		fmt.Fprintf(w, "  With @inco::     %d / %d  (%.1f%%)\n", r.GuardedFuncs, r.TotalFuncs, pct)
		fmt.Fprintf(w, "  Without @inco::  %d / %d  (%.1f%%)\n\n",
			r.TotalFuncs-r.GuardedFuncs, r.TotalFuncs, 100-pct)
	} else {
		fmt.Fprintf(w, "  (no functions found)\n\n")
	}

	// --- Directive vs if ---
	fmt.Fprintf(w, "Directive vs if:\n")
	fmt.Fprintf(w, "  @inco::           %d\n", r.TotalRequires)
	fmt.Fprintf(w, "  ─────────────────────\n")
	fmt.Fprintf(w, "  Total directives:   %d\n", r.TotalDirectives)
	fmt.Fprintf(w, "  Native if stmts:    %d\n", r.TotalIfs)
	total := r.TotalDirectives + r.TotalIfs
	if total > 0 {
		ratio := float64(r.TotalDirectives) / float64(total) * 100
		fmt.Fprintf(w, "  inco/(if+inco):     %.1f%%\n\n", ratio)
	} else {
		fmt.Fprintf(w, "  inco/(if+inco):     — (no directives or if statements)\n\n")
	}

	// --- Per-file breakdown ---
	fmt.Fprintf(w, "Per-file breakdown:\n")
	// Calculate column widths.
	maxPath := 4 // "File"
	for _, f := range r.Files {
		if len(f.RelPath) > maxPath {
			maxPath = len(f.RelPath)
		}
	}
	if maxPath > 50 {
		maxPath = 50
	}

	fmt.Fprintf(w, "  %-*s  @inco:  if  funcs  guarded\n", maxPath, "File")
	fmt.Fprintf(w, "  %s  %s\n", strings.Repeat("─", maxPath), "──────  ──  ─────  ───────")
	for _, f := range r.Files {
		guarded := 0
		for _, fn := range f.Funcs {
			if fn.RequireCount > 0 {
				guarded++
			}
		}
		display := f.RelPath
		if len(display) > maxPath {
			display = "…" + display[len(display)-maxPath+1:]
		}
		fmt.Fprintf(w, "  %-*s  %7d  %2d  %5d  %7d\n",
			maxPath, display, f.RequireCount,
			f.IfCount, len(f.Funcs), guarded)
	}

	// --- Unguarded functions ---
	var unguarded []string
	for _, f := range r.Files {
		for _, fn := range f.Funcs {
			if fn.RequireCount == 0 && fn.Name != "func literal" {
				unguarded = append(unguarded, fmt.Sprintf("  %s:%d  %s", f.RelPath, fn.Line, fn.Name))
			}
		}
	}
	if len(unguarded) > 0 {
		fmt.Fprintf(w, "\nFunctions without @inco: (%d):\n", len(unguarded))
		for _, s := range unguarded {
			fmt.Fprintln(w, s)
		}
	}
}
