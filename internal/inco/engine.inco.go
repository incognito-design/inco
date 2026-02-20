package inco

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
)

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

// Engine scans Go source files for @inco: directives and produces an
// overlay that injects the corresponding if-statements at compile time.
type Engine struct {
	Root    string
	Overlay Overlay
	fset    *token.FileSet
}

// NewEngine creates an engine rooted at the given directory.
func NewEngine(root string) *Engine {
	// @inco: root != "", -panic("NewEngine: root must not be empty")
	return &Engine{
		Root:    root,
		Overlay: Overlay{Replace: make(map[string]string)},
		fset:    token.NewFileSet(),
	}
}

// ---------------------------------------------------------------------------
// Run — top-level entry point
// ---------------------------------------------------------------------------

// Run scans all Go source files under Root, processes @inco: directives,
// and writes the overlay + shadow files into .inco_cache/.
func (e *Engine) Run() {
	// @inco: e != nil, -panic("Run: nil engine")
	// @inco: e.Root != "", -panic("Run: root must not be empty")
	packages := e.scanPackages()
	for _, pkg := range packages {
		e.processPackage(pkg)
	}

	if len(e.Overlay.Replace) > 0 {
		e.writeOverlay()
		fmt.Fprintf(os.Stderr, "inco: overlay written to %s (%d file(s) mapped)\n",
			filepath.Join(e.Root, ".inco_cache", "overlay.json"),
			len(e.Overlay.Replace))
	}
}

// ---------------------------------------------------------------------------
// Package scanning
// ---------------------------------------------------------------------------

type pkgBundle struct {
	Dir   string
	Files map[string]*ast.File // absolute path → AST
	Paths []string             // sorted absolute paths (deterministic iteration)
}

// scanPackages walks Root, parses every non-test .go file, and groups them
// by directory (= Go package).
func (e *Engine) scanPackages() []*pkgBundle {
	dirFiles := make(map[string]map[string]*ast.File)

	walkFn := func(path string, d os.DirEntry, err error) error {
		// @inco: err == nil, -panic(err)
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(e.fset, path, nil, parser.ParseComments)
		if err != nil {
			panic(err)
		}
		dir := filepath.Dir(path)
		if dirFiles[dir] == nil {
			dirFiles[dir] = make(map[string]*ast.File)
		}
		dirFiles[dir][path] = f
		return nil
	}
	if err := filepath.WalkDir(e.Root, walkFn); err != nil {
		panic(err)
	}

	// Sort directories for deterministic order.
	dirs := make([]string, 0, len(dirFiles))
	for d := range dirFiles {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	result := make([]*pkgBundle, 0, len(dirs))
	for _, dir := range dirs {
		files := dirFiles[dir]
		paths := make([]string, 0, len(files))
		for p := range files {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		result = append(result, &pkgBundle{Dir: dir, Files: files, Paths: paths})
	}
	return result
}

// ---------------------------------------------------------------------------
// Package & file processing
// ---------------------------------------------------------------------------

func (e *Engine) processPackage(pkg *pkgBundle) {
	// @inco: pkg != nil
	for _, path := range pkg.Paths {
		e.processFile(path, pkg.Files[path])
	}
}

// processFile scans a single source file for directives,
// generates injected if-blocks via text replacement, and writes a shadow.
func (e *Engine) processFile(path string, f *ast.File) {
	// @inco: path != "", -panic("processFile: empty path")
	// @inco: f != nil, -panic("processFile: nil AST")
	// 1. Collect directive lines from AST comments.
	directives := make(map[int]*Directive) // 1-based line → Directive
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			d := ParseDirective(c.Text)
			if d != nil {
				line := e.fset.Position(c.Pos()).Line
				directives[line] = d
			}
		}
	}

	// 2. Read source as lines.
	src, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	lines := strings.Split(string(src), "\n")

	// 3. Collect standalone @inco: directives.
	standalone := make(map[int]*Directive)

	for lineNum, d := range directives {
		idx := lineNum - 1
		if idx < 0 || idx >= len(lines) {
			continue
		}
		trimmed := strings.TrimSpace(lines[idx])
		isStandaloneLine := strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*")
		if isStandaloneLine {
			standalone[lineNum] = d
		}
	}

	// 4. Build output: replace directive lines with if-blocks, add //line
	//    directives to preserve source mapping.
	var output []string
	prevWasDirective := false

	for idx, line := range lines {
		lineNum := idx + 1

		if d, ok := standalone[lineNum]; ok {
			// Standalone @inco:: replace comment line with if-block.
			indent := extractIndent(line)
			output = append(output, fmt.Sprintf("%s//line %s:%d", indent, path, lineNum))
			output = append(output, e.generateIfBlock(d, indent, path, lineNum))
			prevWasDirective = true
		} else {
			if prevWasDirective {
				indent := extractIndent(line)
				output = append(output, fmt.Sprintf("%s//line %s:%d", indent, path, lineNum))
				prevWasDirective = false
			}
			output = append(output, line)
		}
	}

	// 5. Collect package references from injected code, add missing imports.
	content := strings.Join(output, "\n")
	content = e.addMissingImports(content, f, directives)

	// 6. Write shadow file.
	e.writeShadow(path, []byte(content))
}

// ---------------------------------------------------------------------------
// Code generation
// ---------------------------------------------------------------------------

// generateIfBlock returns the text of the injected if-statement.
//
//	if !(expr) {
//	    panic(...)
//	}
func (e *Engine) generateIfBlock(d *Directive, indent, path string, line int) string {
	cond := fmt.Sprintf("!(%s)", d.Expr)
	body := e.buildPanicBody(d, path, line)
	return fmt.Sprintf("%sif %s {\n%s\t%s\n%s}", indent, cond, indent, body, indent)
}

// buildPanicBody generates the action statement for @inco:.
//
//   - ActionReturn + args → return arg0, arg1, ...
//   - ActionReturn bare   → return
//   - ActionContinue      → continue
//   - ActionBreak         → break
//   - ActionPanic + args  → panic(arg)
//   - ActionPanic default → panic("inco violation: <expr> (at file:line)")
func (e *Engine) buildPanicBody(d *Directive, path string, line int) string {
	switch d.Action {
	case ActionReturn:
		if len(d.ActionArgs) > 0 {
			return "return " + strings.Join(d.ActionArgs, ", ")
		}
		return "return"
	case ActionContinue:
		return "continue"
	case ActionBreak:
		return "break"
	default: // ActionPanic
		if len(d.ActionArgs) > 0 {
			return "panic(" + d.ActionArgs[0] + ")"
		}
		relPath := path
		if rel, err := filepath.Rel(e.Root, path); err == nil {
			relPath = rel
		}
		msg := fmt.Sprintf("inco violation: %s (at %s:%d)", d.Expr, relPath, line)
		return fmt.Sprintf("panic(%q)", msg)
	}
}

// ---------------------------------------------------------------------------
// Import management
// ---------------------------------------------------------------------------

// stdlibPackages is a set of commonly used standard library packages that
// might appear in directive action arguments.
var stdlibPackages = map[string]string{
	"fmt":     "fmt",
	"errors":  "errors",
	"strings": "strings",
	"strconv": "strconv",
	"log":     "log",
	"os":      "os",
	"io":      "io",
	"math":    "math",
	"time":    "time",
	"context": "context",
	"sync":    "sync",
	"sort":    "sort",
	"bytes":   "bytes",
	"regexp":  "regexp",
	"path":    "path",
	"net":     "net",
	"http":    "net/http",
	"json":    "encoding/json",
	"xml":     "encoding/xml",
	"sql":     "database/sql",
	"reflect": "reflect",
	"unsafe":  "unsafe",
}

// pkgRefRe matches package-qualified identifiers like fmt.Errorf, errors.New.
var pkgRefRe = regexp.MustCompile(`\b([a-zA-Z_]\w*)\.\w+`)

// addMissingImports re-parses the shadow content, detects package references
// in directive action args, and adds missing imports via astutil.AddImport.
func (e *Engine) addMissingImports(content string, origFile *ast.File, directives map[int]*Directive) string {
	// 1. Collect all package-qualified identifiers from directives.
	needed := make(map[string]bool)
	for _, d := range directives {
		sources := d.ActionArgs
		if d.Expr != "" {
			sources = append(sources, d.Expr)
		}
		for _, s := range sources {
			for _, match := range pkgRefRe.FindAllStringSubmatch(s, -1) {
				needed[match[1]] = true
			}
		}
	}
	if len(needed) == 0 {
		return content
	}

	// 2. Determine which packages are already imported.
	imported := make(map[string]bool)
	for _, imp := range origFile.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		// Use local name if aliased, otherwise last segment.
		var name string
		if imp.Name != nil {
			name = imp.Name.Name
		} else {
			parts := strings.Split(path, "/")
			name = parts[len(parts)-1]
		}
		imported[name] = true
	}

	// 3. Find which needed packages are missing.
	var toAdd []string
	for pkg := range needed {
		if imported[pkg] {
			continue
		}
		if _, ok := stdlibPackages[pkg]; ok {
			toAdd = append(toAdd, pkg)
		}
	}
	if len(toAdd) == 0 {
		return content
	}

	// 4. Re-parse the shadow content and add imports via astutil.
	fset := token.NewFileSet()
	shadowAST, err := parser.ParseFile(fset, "", content, parser.ParseComments)
	if err != nil {
		// If parsing fails, return content as-is.
		return content
	}
	for _, pkg := range toAdd {
		astutil.AddImport(fset, shadowAST, stdlibPackages[pkg])
	}

	// 5. Re-render.
	var buf strings.Builder
	if err := format.Node(&buf, fset, shadowAST); err != nil {
		return content
	}
	return buf.String()
}

// ---------------------------------------------------------------------------
// Shadow & overlay I/O
// ---------------------------------------------------------------------------

func (e *Engine) writeShadow(origPath string, content []byte) {
	cacheDir := filepath.Join(e.Root, ".inco_cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		panic(err)
	}

	hash := sha256.Sum256(content)
	shadowName := fmt.Sprintf("%s_%x.go",
		strings.TrimSuffix(filepath.Base(origPath), ".go"),
		hash[:8])
	shadowPath := filepath.Join(cacheDir, shadowName)

	if err := os.WriteFile(shadowPath, content, 0o644); err != nil {
		panic(err)
	}
	e.Overlay.Replace[origPath] = shadowPath
}

func (e *Engine) writeOverlay() {
	cacheDir := filepath.Join(e.Root, ".inco_cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		panic(err)
	}
	data, err := json.MarshalIndent(e.Overlay, "", "  ")
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "overlay.json"), data, 0o644); err != nil {
		panic(err)
	}
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

// extractIndent returns the leading whitespace of a line.
func extractIndent(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}
