package inco

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// Overlay represents the go build -overlay JSON format.
type Overlay struct {
	Replace map[string]string `json:"Replace"`
}

// Engine is the core processor that scans Go source files,
// parses contract directives, injects assertion code, and
// produces overlay mappings for `go build -overlay`.
type Engine struct {
	Root     string // project root directory
	CacheDir string // .inco_cache directory path
	Overlay  Overlay
}

// NewEngine creates a new Engine rooted at the given directory.
func NewEngine(root string) *Engine {
	// @require len(root) > 0, "root must not be empty"
	cache := filepath.Join(root, ".inco_cache")
	return &Engine{
		Root:     root,
		CacheDir: cache,
		Overlay:  Overlay{Replace: make(map[string]string)},
	}
}

// Run executes the full pipeline: scan -> parse -> inject -> write overlay.
func (e *Engine) Run() error {
	if err := os.MkdirAll(e.CacheDir, 0o755); err != nil {
		return fmt.Errorf("inco: create cache dir: %w", err)
	}

	err := filepath.Walk(e.Root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Skip hidden dirs, vendor, testdata, and cache itself
		if info.IsDir() {
			base := info.Name()
			if strings.HasPrefix(base, ".") || base == "vendor" || base == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		return e.processFile(path)
	})
	if err != nil {
		return err
	}

	return e.writeOverlay()
}

// processFile scans a single Go file for contract directives.
// If any are found, it generates a shadow file and registers it in the overlay.
func (e *Engine) processFile(path string) error {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("inco: parse %s: %w", path, err)
	}

	directives := e.collectDirectives(f, fset)
	if len(directives) == 0 {
		return nil // nothing to do
	}

	// Read original source lines for //line mapping
	absPath, _ := filepath.Abs(path) // @must
	origLines, err := readLines(absPath)
	if err != nil {
		return fmt.Errorf("inco: read original %s: %w", path, err)
	}

	// Inject assertions into AST
	e.injectAssertions(f, fset, directives)

	// Strip all comments to prevent go/printer from displacing them
	// into injected code. The shadow file is for compilation only.
	f.Comments = nil

	// Generate shadow file content
	var buf strings.Builder
	cfg := printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
	if err := cfg.Fprint(&buf, fset, f); err != nil {
		return fmt.Errorf("inco: print shadow for %s: %w", path, err)
	}

	// Post-process: inject //line directives to map back to original source
	shadowContent := injectLineDirectives(buf.String(), origLines, absPath)

	// Compute content hash for stable cache filenames
	hash := contentHash(shadowContent)
	base := strings.TrimSuffix(filepath.Base(path), ".go")
	shadowName := fmt.Sprintf("%s_%s.go", base, hash[:12])
	shadowPath := filepath.Join(e.CacheDir, shadowName)

	if err := os.WriteFile(shadowPath, []byte(shadowContent), 0o644); err != nil {
		return fmt.Errorf("inco: write shadow %s: %w", shadowPath, err)
	}

	e.Overlay.Replace[absPath] = shadowPath
	return nil
}

// directiveInfo associates a parsed Directive with its position in the AST.
type directiveInfo struct {
	Directive *Directive
	Pos       token.Pos
	Comment   *ast.Comment
}

// collectDirectives walks the AST comment map and extracts all contract directives.
func (e *Engine) collectDirectives(f *ast.File, fset *token.FileSet) []directiveInfo {
	var result []directiveInfo
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			d := ParseDirective(c.Text)
			if d != nil {
				result = append(result, directiveInfo{
					Directive: d,
					Pos:       c.Pos(),
					Comment:   c,
				})
			}
		}
	}
	return result
}

// injectAssertions modifies the AST by inserting assertion statements
// after each contract directive comment.
func (e *Engine) injectAssertions(f *ast.File, fset *token.FileSet, directives []directiveInfo) {
	// Build a position -> directive lookup
	dirMap := make(map[token.Pos]*directiveInfo)
	for i := range directives {
		dirMap[directives[i].Pos] = &directives[i]
	}

	ast.Inspect(f, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.BlockStmt:
			if node != nil {
				node.List = e.processStmtList(node.List, node.Lbrace, fset, dirMap)
			}
		case *ast.CaseClause:
			if node != nil {
				node.Body = e.processStmtList(node.Body, node.Colon, fset, dirMap)
			}
		case *ast.CommClause:
			if node != nil {
				node.Body = e.processStmtList(node.Body, node.Colon, fset, dirMap)
			}
		}
		return true
	})
}

// processStmtList inspects a statement list and injects assertions where directives are found.
// startPos is the position of the opening brace or colon that precedes the first statement.
func (e *Engine) processStmtList(stmts []ast.Stmt, startPos token.Pos, fset *token.FileSet, dirMap map[token.Pos]*directiveInfo) []ast.Stmt {
	var newList []ast.Stmt
	for i, stmt := range stmts {
		// Check if any directive is associated with this statement
		// by looking at comments that appear before this statement's position
		// and after the previous statement (or block/clause start).
		var prevEnd token.Pos
		if i > 0 {
			prevEnd = stmts[i-1].End()
		} else {
			prevEnd = startPos
		}

		// Collect directives that appear between prevEnd and this statement
		var pendingMust *directiveInfo
		var pendingMustPos token.Pos
		for pos, di := range dirMap {
			if pos > prevEnd && pos < stmt.Pos() {
				if di.Directive.Kind == Must {
					// Block-mode @must: directive on its own line, applies to next statement
					pendingMust = di
					pendingMustPos = pos
				} else {
					generated := e.generateAssertion(di, fset)
					newList = append(newList, generated...)
					delete(dirMap, pos) // consumed
				}
			}
		}

		// Handle block-mode @must: applies to the next assignment statement
		if pendingMust != nil {
			if assign, ok := stmt.(*ast.AssignStmt); ok {
				mustStmts := e.generateMustForAssign(assign, fset, pendingMust)
				newList = append(newList, stmt)
				newList = append(newList, mustStmts...)
				stmt = nil                     // mark as handled
				delete(dirMap, pendingMustPos) // consumed
			}
		}

		// Handle inline // @must on assignment statements (same line)
		if stmt != nil {
			if assign, ok := stmt.(*ast.AssignStmt); ok {
				for pos, di := range dirMap {
					stmtLine := fset.Position(stmt.Pos()).Line
					commentLine := fset.Position(pos).Line
					if di.Directive.Kind == Must && commentLine == stmtLine {
						mustStmts := e.generateMustForAssign(assign, fset, di)
						newList = append(newList, stmt)
						newList = append(newList, mustStmts...)
						stmt = nil          // mark as handled
						delete(dirMap, pos) // consumed
						break
					}
				}
			}
		}

		if stmt != nil {
			newList = append(newList, stmt)
		}
	}
	return newList
}

// generateAssertion creates assertion statements from a directive.
func (e *Engine) generateAssertion(di *directiveInfo, fset *token.FileSet) []ast.Stmt {
	pos := fset.Position(di.Pos)
	loc := fmt.Sprintf("%s:%d", pos.Filename, pos.Line)

	switch di.Directive.Kind {
	case Require:
		return e.generateRequire(di.Directive, loc)
	case Ensure:
		return e.generateEnsure(di.Directive, loc)
	default:
		return nil
	}
}

// generateRequire generates `if <cond> { panic(...) }` statements for require directives.
func (e *Engine) generateRequire(d *Directive, loc string) []ast.Stmt {
	if d.ND {
		return e.generateNDChecks(d.Vars, loc, "require")
	}
	if d.Expr != "" {
		msg := d.Message
		if msg == "" {
			msg = fmt.Sprintf("inco // require violation: %s", d.Expr)
		}
		return []ast.Stmt{makeIfPanicStmt(
			fmt.Sprintf("!(%s)", d.Expr),
			fmt.Sprintf("%s at %s", msg, loc),
		)}
	}
	return nil
}

// generateEnsure wraps the check in a defer for postcondition checking.
func (e *Engine) generateEnsure(d *Directive, loc string) []ast.Stmt {
	if d.ND {
		inner := e.generateNDChecks(d.Vars, loc, "ensure")
		return []ast.Stmt{makeDeferStmt(inner)}
	}
	if d.Expr != "" {
		msg := d.Message
		if msg == "" {
			msg = fmt.Sprintf("inco // ensure violation: %s", d.Expr)
		}
		inner := []ast.Stmt{makeIfPanicStmt(
			fmt.Sprintf("!(%s)", d.Expr),
			fmt.Sprintf("%s at %s", msg, loc),
		)}
		return []ast.Stmt{makeDeferStmt(inner)}
	}
	return nil
}

// generateNDChecks generates non-defaulted zero-value panic checks.
// For the MVP, we generate simple `== nil` checks (pointer/interface/slice/map/chan).
// Full type-aware checks (string=="", int==0) require go/types integration (Phase 2).
func (e *Engine) generateNDChecks(vars []string, loc string, protocol string) []ast.Stmt {
	var stmts []ast.Stmt
	for _, v := range vars {
		// MVP: generate nil check. Type-aware checks come later with go/types.
		msg := fmt.Sprintf("inco // %s -nd violation: [%s] is defaulted at %s", protocol, v, loc)
		stmts = append(stmts, makeIfPanicStmt(fmt.Sprintf("%s == nil", v), msg))
	}
	return stmts
}

// generateMustForAssign injects error checking after an assignment that uses _ for the error.
func (e *Engine) generateMustForAssign(assign *ast.AssignStmt, fset *token.FileSet, di *directiveInfo) []ast.Stmt {
	pos := fset.Position(di.Pos)
	loc := fmt.Sprintf("%s:%d", pos.Filename, pos.Line)

	// Find _ (blank identifier) in LHS and replace with _inco_err
	for i, lhs := range assign.Lhs {
		ident, ok := lhs.(*ast.Ident)
		if ok && ident.Name == "_" {
			errVar := fmt.Sprintf("_inco_err_%d", pos.Line)
			ident.Name = errVar

			// Ensure it's a short variable declaration so the new name is declared
			if assign.Tok == token.ASSIGN {
				assign.Tok = token.DEFINE
			}
			_ = i

			msg := fmt.Sprintf("inco // must violation at %s", loc)
			return []ast.Stmt{makeIfPanicErrStmt(
				errVar,
				msg,
			)}
		}
	}

	// If no blank identifier found, check for explicit err variable
	for _, lhs := range assign.Lhs {
		ident, ok := lhs.(*ast.Ident)
		if ok && ident.Name == "err" {
			msg := fmt.Sprintf("inco // must violation at %s", loc)
			return []ast.Stmt{makeIfPanicErrStmt(
				"err",
				msg,
			)}
		}
	}

	return nil
}

// writeOverlay writes the overlay.json file to the cache directory.
func (e *Engine) writeOverlay() error {
	if len(e.Overlay.Replace) == 0 {
		return nil
	}

	data, err := json.MarshalIndent(e.Overlay, "", "  ")
	if err != nil {
		return fmt.Errorf("inco: marshal overlay: %w", err)
	}

	path := filepath.Join(e.CacheDir, "overlay.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("inco: write overlay.json: %w", err)
	}

	fmt.Printf("inco: overlay written to %s (%d file(s) mapped)\n", path, len(e.Overlay.Replace))
	return nil
}

// --- AST construction helpers ---

// makeIfPanicStmt builds: if <cond> { panic("<msg>") }
func makeIfPanicStmt(cond string, msg string) *ast.IfStmt {
	return &ast.IfStmt{
		Cond: &ast.Ident{Name: cond},
		Body: &ast.BlockStmt{
			List: []ast.Stmt{
				&ast.ExprStmt{
					X: &ast.CallExpr{
						Fun:  &ast.Ident{Name: "panic"},
						Args: []ast.Expr{&ast.BasicLit{Kind: token.STRING, Value: fmt.Sprintf(`"%s"`, msg)}},
					},
				},
			},
		},
	}
}

// makeIfPanicErrStmt builds: if <errVar> != nil { panic("<msg>: " + <errVar>.Error()) }
func makeIfPanicErrStmt(errVar string, msg string) *ast.IfStmt {
	return &ast.IfStmt{
		Cond: &ast.BinaryExpr{
			X:  &ast.Ident{Name: errVar},
			Op: token.NEQ,
			Y:  &ast.Ident{Name: "nil"},
		},
		Body: &ast.BlockStmt{
			List: []ast.Stmt{
				&ast.ExprStmt{
					X: &ast.CallExpr{
						Fun: &ast.Ident{Name: "panic"},
						Args: []ast.Expr{
							&ast.BinaryExpr{
								X:  &ast.BasicLit{Kind: token.STRING, Value: fmt.Sprintf(`"%s: "`, msg)},
								Op: token.ADD,
								Y: &ast.CallExpr{
									Fun: &ast.SelectorExpr{
										X:   &ast.Ident{Name: errVar},
										Sel: &ast.Ident{Name: "Error"},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// makeDeferStmt wraps statements in: defer func() { ... }()
func makeDeferStmt(stmts []ast.Stmt) *ast.DeferStmt {
	return &ast.DeferStmt{
		Call: &ast.CallExpr{
			Fun: &ast.FuncLit{
				Type: &ast.FuncType{Params: &ast.FieldList{}},
				Body: &ast.BlockStmt{List: stmts},
			},
		},
	}
}

// contentHash returns a hex-encoded SHA-256 hash of the content.
func contentHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

// readLines reads a file and returns its lines (without newlines).
func readLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

// injectLineDirectives compares the shadow output with the original source lines
// and inserts `//line` directives after injected blocks to restore correct line mapping.
//
// Strategy: walk shadow lines and original lines together. When a shadow line matches
// the next expected original line, they are "in sync". When shadow lines don't match
// (i.e. they are injected code), we let them pass. Once we re-sync, we emit a
// `//line original.go:N` directive to snap the compiler's line counter back.
func injectLineDirectives(shadow string, origLines []string, absPath string) string {
	shadowLines := strings.Split(shadow, "\n")

	origIdx := 0 // pointer into original lines
	var result []string
	needsLineDirective := false

	for _, sLine := range shadowLines {
		trimmed := strings.TrimSpace(sLine)

		// Try to match against the current original line
		if origIdx < len(origLines) {
			origTrimmed := strings.TrimSpace(origLines[origIdx])

			if trimmed == origTrimmed {
				// Lines match — we are in sync
				if needsLineDirective {
					// Emit //line to snap back to the correct original line number
					// (origIdx is 0-based, line numbers are 1-based)
					result = append(result, fmt.Sprintf("//line %s:%d", absPath, origIdx+1))
					needsLineDirective = false
				}
				result = append(result, sLine)
				origIdx++
				continue
			}

			// Check if this is a contract comment line we should skip in original
			if isContractComment(origTrimmed) {
				// The original had a contract comment here that was stripped;
				// advance past it and retry matching.
				origIdx++
				// Retry match with advanced origIdx
				if origIdx < len(origLines) {
					origTrimmed = strings.TrimSpace(origLines[origIdx])
					if trimmed == origTrimmed {
						if needsLineDirective {
							result = append(result, fmt.Sprintf("//line %s:%d", absPath, origIdx+1))
							needsLineDirective = false
						}
						result = append(result, sLine)
						origIdx++
						continue
					}
				}
			}
		}

		// This shadow line is injected code (no match in original)
		result = append(result, sLine)
		needsLineDirective = true
	}

	return strings.Join(result, "\n")
}

// isContractComment checks if a line is an inco contract comment that was stripped.
func isContractComment(line string) bool {
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "//") {
		return false
	}
	s = strings.TrimSpace(s[2:])
	return strings.HasPrefix(s, "@require") ||
		strings.HasPrefix(s, "@ensure") ||
		strings.HasPrefix(s, "@must")
}
