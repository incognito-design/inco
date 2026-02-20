package inco

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile is a test helper that creates a file with the given content.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// Basic counts
// ---------------------------------------------------------------------------

func TestAudit_BasicCounts(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "main.go"), `package main

import "fmt"

func Guarded(x int, name string) {
	// @inco: x > 0
	// @inco: len(name) > 0
	if x > 10 {
		fmt.Println("big")
	}
}

func Unguarded(y int) {
	if y < 0 {
		fmt.Println("neg")
	}
}

type DB struct{}
func (db *DB) Query(q string) (string, error) { return "", nil }
`)

	result := Audit(dir)

	if result.TotalFiles != 1 {
		t.Errorf("TotalFiles = %d, want 1", result.TotalFiles)
	}
	if result.TotalFuncs != 3 { // Guarded, Unguarded, Query
		t.Errorf("TotalFuncs = %d, want 3", result.TotalFuncs)
	}
	if result.GuardedFuncs != 1 { // only Guarded
		t.Errorf("GuardedFuncs = %d, want 1", result.GuardedFuncs)
	}
	if result.TotalRequires != 2 {
		t.Errorf("TotalRequires = %d, want 2", result.TotalRequires)
	}
	if result.TotalDirectives != 2 {
		t.Errorf("TotalDirectives = %d, want 2", result.TotalDirectives)
	}
	if result.TotalIfs != 2 { // x>10, y<0
		t.Errorf("TotalIfs = %d, want 2", result.TotalIfs)
	}
}

// ---------------------------------------------------------------------------
// Multiple files
// ---------------------------------------------------------------------------

func TestAudit_MultipleFiles(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "a.go"), `package main

func A(x int) {
	// @inco: x > 0
}

func B(y int) {
	if y < 0 {}
}
`)

	writeFile(t, filepath.Join(dir, "b.go"), `package main

func C(z int) {
	// @inco: z != 0
	if z > 100 {}
}
`)

	result := Audit(dir)

	if result.TotalFiles != 2 {
		t.Errorf("TotalFiles = %d, want 2", result.TotalFiles)
	}
	if result.TotalFuncs != 3 {
		t.Errorf("TotalFuncs = %d, want 3", result.TotalFuncs)
	}
	if result.GuardedFuncs != 2 { // A and C
		t.Errorf("GuardedFuncs = %d, want 2", result.GuardedFuncs)
	}
	if result.TotalRequires != 2 {
		t.Errorf("TotalRequires = %d, want 2", result.TotalRequires)
	}
	if result.TotalIfs != 2 {
		t.Errorf("TotalIfs = %d, want 2", result.TotalIfs)
	}
}

// ---------------------------------------------------------------------------
// Skips hidden dirs and test files
// ---------------------------------------------------------------------------

func TestAudit_SkipsHiddenAndTestFiles(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "main.go"), `package main

func X(a int) {
	// @inco: a > 0
}
`)
	// Test file — should be skipped.
	writeFile(t, filepath.Join(dir, "main_test.go"), `package main

func TestX() {
	// @inco: true
}
`)
	// Hidden dir — should be skipped.
	hidden := filepath.Join(dir, ".cache")
	os.MkdirAll(hidden, 0o755)
	writeFile(t, filepath.Join(hidden, "cached.go"), `package cache

func Y(b int) {
	// @inco: b > 0
}
`)

	result := Audit(dir)

	if result.TotalFiles != 1 {
		t.Errorf("TotalFiles = %d, want 1", result.TotalFiles)
	}
	if result.TotalRequires != 1 {
		t.Errorf("TotalRequires = %d, want 1", result.TotalRequires)
	}
}

// ---------------------------------------------------------------------------
// Closures counted as functions
// ---------------------------------------------------------------------------

func TestAudit_ClosureCounted(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "main.go"), `package main

func Outer() {
	// @inco: true
	inner := func(x int) {
		// @inco: x > 0
	}
	_ = inner
}
`)

	result := Audit(dir)

	if result.TotalFuncs != 2 { // Outer + func literal
		t.Errorf("TotalFuncs = %d, want 2", result.TotalFuncs)
	}
	if result.GuardedFuncs != 2 {
		t.Errorf("GuardedFuncs = %d, want 2", result.GuardedFuncs)
	}
}

// ---------------------------------------------------------------------------
// Empty project
// ---------------------------------------------------------------------------

func TestAudit_EmptyProject(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "main.go"), `package main

func main() {}
`)

	result := Audit(dir)

	if result.TotalFuncs != 1 {
		t.Errorf("TotalFuncs = %d, want 1", result.TotalFuncs)
	}
	if result.GuardedFuncs != 0 {
		t.Errorf("GuardedFuncs = %d, want 0", result.GuardedFuncs)
	}
	if result.TotalDirectives != 0 {
		t.Errorf("TotalDirectives = %d, want 0", result.TotalDirectives)
	}
}

// ---------------------------------------------------------------------------
// Method receiver
// ---------------------------------------------------------------------------

func TestAudit_MethodReceiver(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "main.go"), `package main

type Svc struct{}

func (s *Svc) Do(x int) {
	// @inco: x > 0
}
`)

	result := Audit(dir)

	if result.TotalFuncs != 1 {
		t.Errorf("TotalFuncs = %d, want 1", result.TotalFuncs)
	}
	if result.GuardedFuncs != 1 {
		t.Errorf("GuardedFuncs = %d, want 1", result.GuardedFuncs)
	}
	if len(result.Files) != 1 || len(result.Files[0].Funcs) != 1 {
		t.Fatal("unexpected file/func count")
	}
	if result.Files[0].Funcs[0].Name != "Svc.Do" {
		t.Errorf("func name = %q, want %q", result.Files[0].Funcs[0].Name, "Svc.Do")
	}
}

// ---------------------------------------------------------------------------
// PrintReport
// ---------------------------------------------------------------------------

func TestAudit_PrintReport(t *testing.T) {
	r := &AuditResult{
		TotalFiles:      2,
		TotalFuncs:      5,
		GuardedFuncs:    3,
		TotalIfs:        10,
		TotalRequires:   4,
		TotalDirectives: 4,
		Files: []FileAudit{
			{RelPath: "a.go", RequireCount: 3, IfCount: 6,
				Funcs: []FuncAudit{{Name: "A", Line: 3, RequireCount: 2}, {Name: "B", Line: 8, RequireCount: 1}}},
			{RelPath: "b.go", RequireCount: 1, IfCount: 4,
				Funcs: []FuncAudit{{Name: "C", Line: 3, RequireCount: 0}, {Name: "D", Line: 8, RequireCount: 0}, {Name: "E", Line: 13, RequireCount: 1}}},
		},
	}

	var buf bytes.Buffer
	r.PrintReport(&buf)
	out := buf.String()

	for _, want := range []string{
		"contract coverage report",
		"@inco: coverage:",
		"3 / 5",
		"60.0%",
		"Directive vs if:",
		"@inco:",
		"Total directives:",
		"Native if stmts:",
		"0.40",
		"Per-file breakdown:",
		"a.go",
		"b.go",
		"Functions without @inco:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q\n\nFull output:\n%s", want, out)
		}
	}
}

// ---------------------------------------------------------------------------
// Directive with action — still counted as @inco:
// ---------------------------------------------------------------------------

func TestAudit_DirectiveWithAction(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "main.go"), `package main

import "fmt"

func Check(x int) {
	// @inco: x > 0, -return
	fmt.Println(x)
}
`)

	result := Audit(dir)

	if result.TotalRequires != 1 {
		t.Errorf("TotalRequires = %d, want 1", result.TotalRequires)
	}
	if result.TotalDirectives != 1 {
		t.Errorf("TotalDirectives = %d, want 1", result.TotalDirectives)
	}
}
