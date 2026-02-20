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

// Engine scans Go source files for @require directives and produces an
// overlay that injects the corresponding if-statements at compile time.
type Engine struct {
	Root    string
	Overlay Overlay
	fset    *token.FileSet
}

// NewEngine creates an engine rooted at the given directory.
func NewEngine(root string) *Engine {
	// @require root != "" panic("NewEngine: root must not be empty")
	return &Engine{
		Root:    root,
		Overlay: Overlay{Replace: make(map[string]string)},
		fset:    token.NewFileSet(),
	}
}

// ---------------------------------------------------------------------------
// Run — top-level entry point
// ---------------------------------------------------------------------------

// Run scans all Go source files under Root, processes @require directives,
// and writes the overlay + shadow files into .inco_cache/.
func (e *Engine) Run() {
	// @require e != nil panic("Run: nil engine")
	// @require e.Root != "" panic("Run: root must not be empty")
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
		// @require err == nil panic(err)
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") ||
			strings.HasSuffix(name, "_test.go") ||
			strings.HasSuffix(name, "_inco.go") {
			return nil
		}
		f, _ := parser.ParseFile(e.fset, path, nil, parser.ParseComments) // @must
		dir := filepath.Dir(path)
		if dirFiles[dir] == nil {
			dirFiles[dir] = make(map[string]*ast.File)
		}
		dirFiles[dir][path] = f
		return nil
	}
	_ = filepath.WalkDir(e.Root, walkFn) // @must

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
	// @require pkg != nil
	for _, path := range pkg.Paths {
		e.processFile(path, pkg.Files[path])
	}
}

// processFile scans a single source file for directives,
// generates injected if-blocks via text replacement, and writes a shadow.
func (e *Engine) processFile(path string, f *ast.File) {
	// @require path != "" panic("processFile: empty path")
	// @require f != nil panic("processFile: nil AST")
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
	if len(directives) == 0 {
		return
	}

	// 2. Read source as lines.
	src, _ := os.ReadFile(path) // @must
	lines := strings.Split(string(src), "\n")

	// 3. Classify directives: standalone (@require/@ensure) vs inline (@must/@expect).
	standalone := make(map[int]*Directive)
	inline := make(map[int]*Directive)

	for lineNum, d := range directives {
		idx := lineNum - 1
		if idx < 0 || idx >= len(lines) {
			continue
		}
		trimmed := strings.TrimSpace(lines[idx])
		isStandaloneLine := strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*")

		switch d.Kind {
		case KindRequire, KindEnsure:
			if isStandaloneLine {
				standalone[lineNum] = d
			}
		case KindMust, KindExpect:
			if !isStandaloneLine {
				inline[lineNum] = d
			}
		}
	}
	if len(standalone) == 0 && len(inline) == 0 {
		return
	}

	// 3.5. Compute function scopes for per-scope variable tracking.
	type funcScope struct{ startLine, endLine int }
	var funcScopes []funcScope
	ast.Inspect(f, func(n ast.Node) bool {
		var body *ast.BlockStmt
		switch fn := n.(type) {
		case *ast.FuncDecl:
			body = fn.Body
		case *ast.FuncLit:
			body = fn.Body
		}
		if body != nil {
			funcScopes = append(funcScopes, funcScope{
				startLine: e.fset.Position(body.Pos()).Line,
				endLine:   e.fset.Position(body.End()).Line,
			})
		}
		return true
	})
	scopeAt := func(lineNum int) int {
		best := -1
		for i, s := range funcScopes {
			if lineNum >= s.startLine && lineNum <= s.endLine {
				if best == -1 || s.startLine > funcScopes[best].startLine {
					best = i
				}
			}
		}
		return best
	}

	// 4. Build output: replace/augment directive lines, add //line directives
	//    to preserve source mapping.
	var output []string
	prevWasDirective := false
	declared := make(map[string]bool) // tracks __inco_err/__inco_ok declarations
	lastScope := -2                   // sentinel distinct from scopeAt's -1

	for idx, line := range lines {
		lineNum := idx + 1

		if d, ok := standalone[lineNum]; ok {
			// Standalone @require/@ensure: replace comment line with if-block (or defer).
			indent := extractIndent(line)
			output = append(output, fmt.Sprintf("%s//line %s:%d", indent, path, lineNum))
			if d.Kind == KindEnsure {
				output = append(output, e.generateDeferBlock(d, indent, path, lineNum))
			} else {
				output = append(output, e.generateIfBlock(d, indent, path, lineNum))
			}
			prevWasDirective = true
		} else if d, ok := inline[lineNum]; ok {
			// Reset declared map when crossing function boundaries.
			if scope := scopeAt(lineNum); scope != lastScope {
				declared = make(map[string]bool)
				lastScope = scope
			}
			// Inline @must/@expect: rewrite code line + inject if-block after.
			indent := extractIndent(line)
			varName, rewritten := e.rewriteInlineLine(line, d, declared)
			output = append(output, fmt.Sprintf("%s//line %s:%d", indent, path, lineNum))
			output = append(output, rewritten)
			output = append(output, e.generateInlineIfBlock(d, indent, path, lineNum, varName))
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

// generateDeferBlock returns the text of the injected defer for @ensure.
//
//	defer func() {
//	    if !(expr) {
//	        panic(...)
//	    }
//	}()
func (e *Engine) generateDeferBlock(d *Directive, indent, path string, line int) string {
	cond := fmt.Sprintf("!(%s)", d.Expr)
	body := e.buildEnsurePanicBody(d, path, line)
	return fmt.Sprintf("%sdefer func() {\n%s\tif %s {\n%s\t\t%s\n%s\t}\n%s}()", indent, indent, cond, indent, body, indent, indent)
}

// buildPanicBody generates a panic statement.
//
//   - With ActionArgs  → panic(arg)
//   - Default          → panic("require violation: <expr> (at file:line)")
func (e *Engine) buildPanicBody(d *Directive, path string, line int) string {
	if len(d.ActionArgs) > 0 {
		return "panic(" + d.ActionArgs[0] + ")"
	}
	relPath := path
	if rel, err := filepath.Rel(e.Root, path); err == nil {
		relPath = rel
	}
	msg := fmt.Sprintf("require violation: %s (at %s:%d)", d.Expr, relPath, line)
	return fmt.Sprintf("panic(%q)", msg)
}

// buildEnsurePanicBody generates a panic statement for @ensure.
//
//   - With ActionArgs  → panic(arg)
//   - Default          → panic("ensure violation: <expr> (at file:line)")
func (e *Engine) buildEnsurePanicBody(d *Directive, path string, line int) string {
	if len(d.ActionArgs) > 0 {
		return "panic(" + d.ActionArgs[0] + ")"
	}
	relPath := path
	if rel, err := filepath.Rel(e.Root, path); err == nil {
		relPath = rel
	}
	msg := fmt.Sprintf("ensure violation: %s (at %s:%d)", d.Expr, relPath, line)
	return fmt.Sprintf("panic(%q)", msg)
}

// ---------------------------------------------------------------------------
// Inline directive helpers (@must / @expect)
// ---------------------------------------------------------------------------

// rewriteInlineLine strips the inline @must/@expect comment and replaces the
// last blank identifier (_) with a generated variable name.
// If the blank was assigned with plain "=" and the variable hasn't been
// declared yet in this scope, the assignment is promoted to ":=".
func (e *Engine) rewriteInlineLine(line string, d *Directive, declared map[string]bool) (varName, rewritten string) {
	keyword := "@must"
	varName = "__inco_err"
	if d.Kind == KindExpect {
		keyword = "@expect"
		varName = "__inco_ok"
	}
	codePart := stripInlineComment(line, keyword)
	rewritten = replaceLastBlank(codePart, varName)
	// Promote "=" to ":=" only on first occurrence of this variable.
	if !declared[varName] {
		rewritten = promoteAssign(rewritten, varName)
		declared[varName] = true
	}
	return
}

// promoteAssign ensures the assignment containing varName uses := instead of =.
// Handles both "varName = expr" and "x, varName = expr" patterns:
//   - "_ = foo()"             → "__inco_err = foo()"  → needs ":="
//   - "v, _ = m[k]"           → "v, __inco_ok = m[k]" → needs ":="
//   - "v, _ := m[k]"          → "v, __inco_ok := m[k]" → already ok
func promoteAssign(code, varName string) string {
	// Find the position of varName in the code.
	idx := strings.Index(code, varName)
	if idx < 0 {
		return code
	}
	// Scan forward past varName and whitespace to find the assignment operator.
	pos := idx + len(varName)
	for pos < len(code) && (code[pos] == ' ' || code[pos] == '\t') {
		pos++
	}
	// Check if we're at "=" (but not ":=" or "==").
	if pos < len(code) && code[pos] == '=' {
		if pos+1 >= len(code) || code[pos+1] != '=' { // not "=="
			if pos == 0 || code[pos-1] != ':' { // not already ":="
				return code[:pos] + ":" + code[pos:]
			}
		}
	}
	return code
}

// inlineCondFmt maps DirectiveKind to the fmt pattern for the if-condition.
var inlineCondFmt = map[DirectiveKind]string{
	KindMust:   "%s != nil",
	KindExpect: "!%s",
}

// generateInlineIfBlock builds the if-block for an inline @must/@expect.
func (e *Engine) generateInlineIfBlock(d *Directive, indent, path string, line int, varName string) string {
	cond := fmt.Sprintf(inlineCondFmt[d.Kind], varName)
	body := e.buildInlinePanicBody(d, path, line, varName)
	return fmt.Sprintf("%sif %s {\n%s\t%s\n%s}", indent, cond, indent, body, indent)
}

// buildInlinePanicBody produces the panic statement inside the if-block for
// @must / @expect directives.
func (e *Engine) buildInlinePanicBody(d *Directive, path string, line int, varName string) string {
	if len(d.ActionArgs) > 0 {
		return "panic(" + substituteBlank(d.ActionArgs[0], varName) + ")"
	}
	if d.Kind == KindMust {
		return "panic(" + varName + ")"
	}
	// KindExpect — descriptive message.
	relPath := path
	if rel, err := filepath.Rel(e.Root, path); err == nil {
		relPath = rel
	}
	return fmt.Sprintf("panic(%q)", fmt.Sprintf("expect violation at %s:%d", relPath, line))
}

// stripInlineComment removes the inline directive comment from a code line.
func stripInlineComment(line, keyword string) string {
	for _, prefix := range []string{"// " + keyword, "//" + keyword, "/* " + keyword} {
		if idx := strings.Index(line, prefix); idx >= 0 {
			return strings.TrimRight(line[:idx], " \t")
		}
	}
	return line
}

// replaceLastBlank replaces the last standalone blank identifier (_) with
// replacement.  A blank is standalone when not adjacent to other identifier
// characters (letters, digits, _).
func replaceLastBlank(code, replacement string) string {
	for i := len(code) - 1; i >= 0; i-- {
		if code[i] != '_' {
			continue
		}
		prevOk := i == 0 || !isIdentChar(code[i-1])
		nextOk := i == len(code)-1 || !isIdentChar(code[i+1])
		if prevOk && nextOk {
			return code[:i] + replacement + code[i+1:]
		}
	}
	return code
}

// substituteBlank replaces all standalone _ identifiers in expr with varName.
// This allows users to reference the captured variable via _ in action args:
//
//	// @must return(nil, _)   →   return nil, __inco_err
func substituteBlank(expr, varName string) string {
	var buf strings.Builder
	for i := 0; i < len(expr); i++ {
		if expr[i] == '_' {
			prevOk := i == 0 || !isIdentChar(expr[i-1])
			nextOk := i == len(expr)-1 || !isIdentChar(expr[i+1])
			if prevOk && nextOk {
				buf.WriteString(varName)
				continue
			}
		}
		buf.WriteByte(expr[i])
	}
	return buf.String()
}

// isIdentChar reports whether b can appear in a Go identifier.
func isIdentChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
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
	_ = os.MkdirAll(cacheDir, 0o755) // @must

	hash := sha256.Sum256(content)
	shadowName := fmt.Sprintf("%s_%x.go",
		strings.TrimSuffix(filepath.Base(origPath), ".go"),
		hash[:8])
	shadowPath := filepath.Join(cacheDir, shadowName)

	_ = os.WriteFile(shadowPath, content, 0o644) // @must
	e.Overlay.Replace[origPath] = shadowPath
}

func (e *Engine) writeOverlay() {
	cacheDir := filepath.Join(e.Root, ".inco_cache")
	_ = os.MkdirAll(cacheDir, 0o755)                                       // @must
	data, _ := json.MarshalIndent(e.Overlay, "", "  ")                     // @must
	_ = os.WriteFile(filepath.Join(cacheDir, "overlay.json"), data, 0o644) // @must
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

// extractIndent returns the leading whitespace of a line.
func extractIndent(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}
