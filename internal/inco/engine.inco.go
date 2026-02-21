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
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
)

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

// Engine scans Go source files for @inco: directives and produces an
// overlay that injects the corresponding if-statements at compile time.
type Engine struct {
	Root      string
	Overlay   Overlay
	fset      *token.FileSet
	importMap map[string]string // lazily built: package name → import path
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
//
// Incremental: if a source file's content hash matches the manifest and
// the shadow file still exists, the file is skipped.
func (e *Engine) Run() {
	// @inco: e != nil, -panic("Run: nil engine")
	// @inco: e.Root != "", -panic("Run: root must not be empty")

	oldManifest := e.loadManifest()
	newManifest := &Manifest{Files: make(map[string]ManifestEntry)}
	var skipped int

	walkGoFiles(e.Root, func(path string) error {
		srcHash := hashFile(path)

		// Check cache: source unchanged & shadow file exists → reuse.
		if prev, ok := oldManifest.Files[path]; ok && prev.SrcHash == srcHash {
			if _, err := os.Stat(prev.ShadowPath); err == nil {
				e.Overlay.Replace[path] = prev.ShadowPath
				newManifest.Files[path] = prev
				skipped++
				return nil
			}
		}

		// Cache miss — parse and process.
		f, err := parser.ParseFile(e.fset, path, nil, parser.ParseComments)
		_ = err // @inco: err == nil, -panic(err)
		e.processFile(path, f)

		// Record new manifest entry.
		if shadowPath, ok := e.Overlay.Replace[path]; ok {
			newManifest.Files[path] = ManifestEntry{
				SrcHash:    srcHash,
				ShadowPath: shadowPath,
			}
		}
		return nil
	})

	// Clean up stale shadow files.
	for path, entry := range oldManifest.Files {
		if _, ok := newManifest.Files[path]; !ok {
			os.Remove(entry.ShadowPath)
		}
	}

	if len(e.Overlay.Replace) > 0 {
		e.writeOverlay()
		e.writeManifest(newManifest)
		processed := len(e.Overlay.Replace) - skipped
		fmt.Fprintf(os.Stderr, "inco: overlay written to %s (%d file(s) mapped, %d processed, %d cached)\n",
			filepath.Join(e.Root, ".inco_cache", "overlay.json"),
			len(e.Overlay.Replace), processed, skipped)
	} else {
		e.writeManifest(newManifest)
	}
}

// ---------------------------------------------------------------------------
// File processing
// ---------------------------------------------------------------------------

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
	_ = err // @inco: err == nil, -panic(err)
	lines := strings.Split(string(src), "\n")

	// 3. Classify directives as standalone or inline using AST.
	//    - "standalone": the entire line is a comment (starts with // or /*).
	//    - "inline": the line contains an AST statement (assignment, return, etc.)
	//      with a trailing @inco: comment.
	//    - Otherwise (e.g. struct field comment): directive is ignored.
	standalone := make(map[int]*Directive) // entire line is a comment
	inline := make(map[int]*Directive)     // code with trailing @inco: comment

	stmtLines := collectStmtLines(f, e.fset)
	for lineNum, d := range directives {
		idx := lineNum - 1
		// @inco: idx >= 0 && idx < len(lines), -continue
		trimmed := strings.TrimSpace(lines[idx])
		isCommentLine := strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*")
		if isCommentLine {
			standalone[lineNum] = d
		} else if stmtLines[lineNum] {
			inline[lineNum] = d
		}
		// else: directive in non-statement context (e.g. struct field) — skip
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
		} else if d, ok := inline[lineNum]; ok {
			// Inline @inco:: keep code line, inject if-block after.
			output = append(output, line)
			indent := extractIndent(line)
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

// buildImportMap dynamically resolves package names to import paths by
// querying the Go toolchain. The result is cached for the engine's lifetime
// so that "go list" runs at most once per invocation.
func (e *Engine) buildImportMap() map[string]string {
	if e.importMap != nil {
		return e.importMap
	}
	e.importMap = make(map[string]string)
	ambiguous := make(map[string]bool)

	// 1. All standard library packages.
	e.collectPackages(ambiguous, "std")

	// 2. Packages already used in the module (covers third-party deps).
	e.collectPackages(ambiguous, "-deps", "-e", "./...")

	// Remove ambiguous names (multiple import paths share a short name,
	// e.g. "template" → text/template vs html/template).
	for name := range ambiguous {
		delete(e.importMap, name)
	}

	return e.importMap
}

// collectPackages runs "go list" with the given patterns and records
// each name → importPath pair in e.importMap.
func (e *Engine) collectPackages(ambiguous map[string]bool, patterns ...string) {
	args := append([]string{"list", "-e", "-f", "{{.Name}} {{.ImportPath}}"}, patterns...)
	cmd := exec.Command("go", args...)
	cmd.Dir = e.Root
	out, err := cmd.Output()
	_ = err // @inco: err == nil, -return
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		// @inco: line != "", -continue
		parts := strings.SplitN(line, " ", 2)
		valid := len(parts) == 2 && parts[0] != "" && parts[0] != "main"
		_ = valid // @inco: valid, -continue
		name, impPath := parts[0], parts[1]
		// Skip internal and vendored packages — they are not freely importable.
		internal := internalPkgRe.MatchString(impPath)
		_ = internal // @inco: !internal, -continue
		if existing, ok := e.importMap[name]; ok && existing != impPath {
			ambiguous[name] = true
		} else if !ambiguous[name] {
			e.importMap[name] = impPath
		}
	}
}

// pkgRefRe matches package-qualified identifiers like fmt.Errorf, errors.New.
var pkgRefRe = regexp.MustCompile(`\b([a-zA-Z_]\w*)\.\w+`)

// internalPkgRe matches import paths that are internal or vendored.
var internalPkgRe = regexp.MustCompile(`(^|/)internal/|(^|/)vendor/`)

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
	// @inco: len(needed) > 0, -return(content)

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
	importMap := e.buildImportMap()
	var toAdd []string
	for pkg := range needed {
		// @inco: !imported[pkg], -continue
		if _, ok := importMap[pkg]; ok {
			toAdd = append(toAdd, pkg)
		}
	}
	// @inco: len(toAdd) > 0, -return(content)

	// 4. Re-parse the shadow content and add imports via astutil.
	fset := token.NewFileSet()
	shadowAST, err := parser.ParseFile(fset, "", content, parser.ParseComments)
	_ = err // @inco: err == nil, -return(content)
	for _, pkg := range toAdd {
		astutil.AddImport(fset, shadowAST, importMap[pkg])
	}

	// 5. Re-render.
	var buf strings.Builder
	err = format.Node(&buf, fset, shadowAST)
	_ = err // @inco: err == nil, -return(content)
	return buf.String()
}

// ---------------------------------------------------------------------------
// Shadow & overlay I/O
// ---------------------------------------------------------------------------

func (e *Engine) writeShadow(origPath string, content []byte) {
	cacheDir := filepath.Join(e.Root, ".inco_cache")
	err := os.MkdirAll(cacheDir, 0o755)
	_ = err // @inco: err == nil, -panic(err)

	hash := sha256.Sum256(content)
	shadowName := fmt.Sprintf("%s_%x.go",
		strings.TrimSuffix(filepath.Base(origPath), ".go"),
		hash[:8])
	shadowPath := filepath.Join(cacheDir, shadowName)

	err = os.WriteFile(shadowPath, content, 0o644)
	_ = err // @inco: err == nil, -panic(err)
	e.Overlay.Replace[origPath] = shadowPath
}

func (e *Engine) writeOverlay() {
	cacheDir := filepath.Join(e.Root, ".inco_cache")
	err := os.MkdirAll(cacheDir, 0o755)
	_ = err // @inco: err == nil, -panic(err)
	data, err := json.MarshalIndent(e.Overlay, "", "  ")
	_ = err // @inco: err == nil, -panic(err)
	err = os.WriteFile(filepath.Join(cacheDir, "overlay.json"), data, 0o644)
	_ = err // @inco: err == nil, -panic(err)
}

// ---------------------------------------------------------------------------
// Manifest I/O (incremental gen)
// ---------------------------------------------------------------------------

func (e *Engine) manifestPath() string {
	return filepath.Join(e.Root, ".inco_cache", "manifest.json")
}

func (e *Engine) loadManifest() *Manifest {
	data, err := os.ReadFile(e.manifestPath())
	if err != nil {
		return &Manifest{Files: make(map[string]ManifestEntry)}
	}
	var m Manifest
	if json.Unmarshal(data, &m) != nil || m.Files == nil {
		return &Manifest{Files: make(map[string]ManifestEntry)}
	}
	return &m
}

func (e *Engine) writeManifest(m *Manifest) {
	cacheDir := filepath.Join(e.Root, ".inco_cache")
	err := os.MkdirAll(cacheDir, 0o755)
	_ = err // @inco: err == nil, -panic(err)
	data, err := json.MarshalIndent(m, "", "  ")
	_ = err // @inco: err == nil, -panic(err)
	err = os.WriteFile(e.manifestPath(), data, 0o644)
	_ = err // @inco: err == nil, -panic(err)
}

// hashFile returns the hex-encoded SHA-256 of a file's contents.
func hashFile(path string) string {
	data, err := os.ReadFile(path)
	_ = err // @inco: err == nil, -panic(err)
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

// extractIndent returns the leading whitespace of a line.
func extractIndent(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

// collectStmtLines walks the AST and returns a set of line numbers that
// contain statements inside function bodies. A directive comment whose
// line appears in this set is classified as "inline" rather than "standalone".
func collectStmtLines(f *ast.File, fset *token.FileSet) map[int]bool {
	lines := make(map[int]bool)
	ast.Inspect(f, func(n ast.Node) bool {
		// @inco: n != nil, -return(false)
		switch n.(type) {
		case *ast.AssignStmt, *ast.ExprStmt, *ast.ReturnStmt,
			*ast.IncDecStmt, *ast.SendStmt, *ast.GoStmt, *ast.DeferStmt,
			*ast.BranchStmt:
			lines[fset.Position(n.Pos()).Line] = true
		}
		return true
	})
	return lines
}
