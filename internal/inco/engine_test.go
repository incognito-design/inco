package inco

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupDir creates a temp directory with Go source files and returns its path.
func setupDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// readShadow returns the content of the first shadow file in the overlay.
func readShadow(t *testing.T, e *Engine) string {
	t.Helper()
	for _, sp := range e.Overlay.Replace {
		data, err := os.ReadFile(sp)
		if err != nil {
			t.Fatalf("reading shadow: %v", err)
		}
		return string(data)
	}
	t.Fatal("no shadow files")
	return ""
}

// --- No directives ---

func TestEngine_NoDirectives(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
	})
	e := NewEngine(dir)
	e.Run()
	if len(e.Overlay.Replace) != 0 {
		t.Errorf("expected 0 overlay entries, got %d", len(e.Overlay.Replace))
	}
}

// --- Default action (panic) ---

func TestEngine_RequireExpr_DefaultPanic(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Greet(name string) {
	// @require len(name) > 0
	fmt.Println(name)
}
`,
	})
	e := NewEngine(dir)
	e.Run()
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "!(len(name) > 0)") {
		t.Errorf("shadow should contain negated condition, got:\n%s", shadow)
	}
	if !strings.Contains(shadow, "panic(") {
		t.Error("shadow should contain panic (default action)")
	}
	if !strings.Contains(shadow, "require violation") {
		t.Error("shadow should contain default violation message")
	}
}

// --- Panic with custom message ---

func TestEngine_RequirePanicCustomMsg(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Process(x int) {
	// @require x > 0 panic("x must be positive")
	fmt.Println(x)
}
`,
	})
	e := NewEngine(dir)
	e.Run()
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, `panic("x must be positive")`) {
		t.Errorf("shadow should contain custom panic message, got:\n%s", shadow)
	}
}

// --- Multiple directives ---

func TestEngine_MultipleDirectives(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Process(name string, age int) {
	// @require len(name) > 0
	// @require age > 0
	fmt.Println(name, age)
}
`,
	})
	e := NewEngine(dir)
	e.Run()
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "!(len(name) > 0)") {
		t.Error("missing first condition")
	}
	if !strings.Contains(shadow, "!(age > 0)") {
		t.Error("missing second condition")
	}
	// Verify order: name check before age check.
	nameIdx := strings.Index(shadow, "len(name)")
	ageIdx := strings.Index(shadow, "age > 0")
	if nameIdx > ageIdx {
		t.Error("directives not in source order")
	}
}

// --- Line directives ---

func TestEngine_LineDirectives(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Hello(name string) {
	// @require len(name) > 0
	fmt.Println(name)
}
`,
	})
	e := NewEngine(dir)
	e.Run()
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "//line") {
		t.Error("shadow should contain //line directives")
	}
}

// --- Overlay JSON ---

func TestEngine_OverlayJSON(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Do(x int) {
	// @require x > 0
	_ = x
}
`,
	})
	e := NewEngine(dir)
	e.Run()

	overlayPath := filepath.Join(dir, ".inco_cache", "overlay.json")
	data, err := os.ReadFile(overlayPath)
	if err != nil {
		t.Fatalf("overlay.json not found: %v", err)
	}

	var ov Overlay
	if err := json.Unmarshal(data, &ov); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(ov.Replace) != 1 {
		t.Errorf("overlay has %d entries, want 1", len(ov.Replace))
	}
	for _, sp := range ov.Replace {
		if _, err := os.Stat(sp); err != nil {
			t.Errorf("shadow file missing: %s", sp)
		}
	}
}

// --- Skips hidden dirs ---

func TestEngine_SkipsHiddenDirs(t *testing.T) {
	dir := setupDir(t, map[string]string{
		".hidden/main.go": `package hidden

func X(x int) {
	// @require x > 0
}
`,
		"main.go": "package main\n\nfunc main() {}\n",
	})
	e := NewEngine(dir)
	e.Run()
	if len(e.Overlay.Replace) != 0 {
		t.Errorf("should skip hidden dirs, got %d", len(e.Overlay.Replace))
	}
}

// --- Content hash stability ---

func TestEngine_ContentHashStable(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Do(x int) {
	// @require x > 0
	_ = x
}
`,
	})

	e1 := NewEngine(dir)
	e1.Run()
	var p1 string
	for _, p := range e1.Overlay.Replace {
		p1 = p
	}

	e2 := NewEngine(dir)
	e2.Run()
	var p2 string
	for _, p := range e2.Overlay.Replace {
		p2 = p
	}

	if filepath.Base(p1) != filepath.Base(p2) {
		t.Errorf("shadow names differ: %s vs %s", filepath.Base(p1), filepath.Base(p2))
	}
}

// --- Closure support ---

func TestEngine_Closure(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Outer() {
	f := func(x int) {
		// @require x > 0
		fmt.Println(x)
	}
	f(42)
}
`,
	})
	e := NewEngine(dir)
	e.Run()
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "!(x > 0)") {
		t.Error("should process directives inside closures")
	}
}

// ---------------------------------------------------------------------------
// @must tests
// ---------------------------------------------------------------------------

func TestEngine_Must_DefaultPanic(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Run() {
	result, _ := fmt.Println("hello") // @must
	_ = result
}
`,
	})
	e := NewEngine(dir)
	e.Run()
	shadow := readShadow(t, e)
	// _ should be replaced with __inco_err
	if !strings.Contains(shadow, "__inco_err") {
		t.Errorf("should contain __inco_err, got:\n%s", shadow)
	}
	// Should have if __inco_err != nil { panic(__inco_err) }
	if !strings.Contains(shadow, "__inco_err != nil") {
		t.Errorf("should contain error check, got:\n%s", shadow)
	}
	if !strings.Contains(shadow, "panic(__inco_err)") {
		t.Errorf("should panic with error var, got:\n%s", shadow)
	}
	// Inline comment should be stripped
	if strings.Contains(shadow, "// @must") {
		t.Error("inline comment should be stripped from shadow")
	}
}

func TestEngine_Must_Return_removed(t *testing.T) {
	// Return action removed — @must now only supports panic.
	// This test verifies that @must with no explicit action defaults to panic.
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func SafeRun() {
	_, _ = fmt.Println("hello") // @must
}
`,
	})
	e := NewEngine(dir)
	e.Run()
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "panic(__inco_err)") {
		t.Errorf("should contain panic, got:\n%s", shadow)
	}
}

func TestEngine_Must_PanicWithSubstitution(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Fetch() {
	_, _ = fmt.Println("hello") // @must panic(fmt.Sprintf("failed: %v", _))
}
`,
	})
	e := NewEngine(dir)
	e.Run()
	shadow := readShadow(t, e)
	// _ in action args should be replaced with __inco_err
	if !strings.Contains(shadow, `panic(fmt.Sprintf("failed: %v", __inco_err))`) {
		t.Errorf("_ should be substituted with __inco_err, got:\n%s", shadow)
	}
}

func TestEngine_Must_PanicCustomMsg(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Run() {
	_, _ = fmt.Println("hello") // @must panic("print failed")
	fmt.Println("done")
}
`,
	})
	e := NewEngine(dir)
	e.Run()
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, `panic("print failed")`) {
		t.Errorf("should contain custom panic message, got:\n%s", shadow)
	}
}

// ---------------------------------------------------------------------------
// @ensure tests
// ---------------------------------------------------------------------------

func TestEngine_Ensure_DefaultPanic(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Run() {
	m := map[string]int{"a": 1}
	v, _ := m["a"] // @ensure
	_ = v
}
`,
	})
	e := NewEngine(dir)
	e.Run()
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "__inco_ok") {
		t.Errorf("should contain __inco_ok, got:\n%s", shadow)
	}
	if !strings.Contains(shadow, "!__inco_ok") {
		t.Errorf("should contain !__inco_ok, got:\n%s", shadow)
	}
	if !strings.Contains(shadow, "ensure violation") {
		t.Errorf("should contain ensure violation message, got:\n%s", shadow)
	}
}

func TestEngine_Ensure_Panic(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Run() {
	m := map[string]int{"a": 1}
	v, _ := m["a"] // @ensure panic("key not found")
	_ = v
}
`,
	})
	e := NewEngine(dir)
	e.Run()
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "__inco_ok") {
		t.Errorf("should contain __inco_ok, got:\n%s", shadow)
	}
	if !strings.Contains(shadow, `panic("key not found")`) {
		t.Errorf("should contain custom panic msg, got:\n%s", shadow)
	}
}

// ---------------------------------------------------------------------------
// Cross-function @must scope tracking
// ---------------------------------------------------------------------------

func TestEngine_Must_MultipleFunctions(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "os"

func First() {
	_ = os.MkdirAll("/tmp/inco_test_1", 0o755) // @must
}

func Second() {
	_ = os.MkdirAll("/tmp/inco_test_2", 0o755) // @must
}
`,
	})
	e := NewEngine(dir)
	e.Run()
	shadow := readShadow(t, e)
	// Both functions should have := declarations (scope reset).
	count := strings.Count(shadow, "__inco_err :=")
	if count != 2 {
		t.Errorf("expected 2 ':=' declarations across functions, got %d\n%s", count, shadow)
	}
}

// ---------------------------------------------------------------------------
// Corner case tests
// ---------------------------------------------------------------------------

// Single-value `_ = foo() // @must` — promote = to :=
func TestEngine_Must_SingleValue(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Run() {
	_ = fmt.Errorf("boom") // @must
}
`,
	})
	e := NewEngine(dir)
	e.Run()
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "__inco_err :=") {
		t.Errorf("should promote = to :=, got:\n%s", shadow)
	}
	if !strings.Contains(shadow, "panic(__inco_err)") {
		t.Errorf("should panic with error var, got:\n%s", shadow)
	}
}

// Multiple @must in same function — first := then =
func TestEngine_Must_SameFuncMultiple(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Run() {
	_, _ = fmt.Println("a") // @must
	_, _ = fmt.Println("b") // @must
}
`,
	})
	e := NewEngine(dir)
	e.Run()
	shadow := readShadow(t, e)
	declCount := strings.Count(shadow, "__inco_err :=")
	plainCount := strings.Count(shadow, "__inco_err =") // does not match ":="
	if declCount != 1 {
		t.Errorf("expected 1 ':=' declaration, got %d\n%s", declCount, shadow)
	}
	if plainCount != 1 {
		t.Errorf("expected 1 plain '=' assignment, got %d\n%s", plainCount, shadow)
	}
}

// Mixed @must + @ensure in same function — separate variables, both get :=
func TestEngine_MixedMustEnsure(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Run() {
	_, _ = fmt.Println("hello") // @must
	m := map[string]int{"a": 1}
	v, _ := m["a"] // @ensure
	_ = v
}
`,
	})
	e := NewEngine(dir)
	e.Run()
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "__inco_err :=") {
		t.Errorf("should have __inco_err :=, got:\n%s", shadow)
	}
	if !strings.Contains(shadow, "__inco_ok :=") {
		t.Errorf("should have __inco_ok :=, got:\n%s", shadow)
	}
	if !strings.Contains(shadow, "panic(__inco_err)") {
		t.Error("should panic with __inco_err")
	}
	if !strings.Contains(shadow, "!__inco_ok") {
		t.Error("should check !__inco_ok")
	}
}

// Consecutive @must lines — no code gap between them
func TestEngine_Must_Consecutive(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Run() {
	_, _ = fmt.Println("a") // @must
	_, _ = fmt.Println("b") // @must
	fmt.Println("done")
}
`,
	})
	e := NewEngine(dir)
	e.Run()
	shadow := readShadow(t, e)
	// Both error checks should be present
	errChecks := strings.Count(shadow, "__inco_err != nil")
	if errChecks != 2 {
		t.Errorf("expected 2 error checks, got %d\n%s", errChecks, shadow)
	}
}

// @ensure for type assertion
func TestEngine_Ensure_TypeAssertion(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func MustString(x any) string {
	v, _ := x.(string) // @ensure panic("not a string")
	return v
}

func main() {
	fmt.Println(MustString("hello"))
}
`,
	})
	e := NewEngine(dir)
	e.Run()
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "__inco_ok") {
		t.Errorf("should replace _ with __inco_ok, got:\n%s", shadow)
	}
	if !strings.Contains(shadow, `panic("not a string")`) {
		t.Errorf("should have custom panic, got:\n%s", shadow)
	}
}

// Nested closure — @must inside inner closure gets its own scope
func TestEngine_Must_NestedClosure(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Outer() {
	outer := func() {
		inner := func() {
			_, _ = fmt.Println("nested") // @must
		}
		inner()
	}
	outer()
}
`,
	})
	e := NewEngine(dir)
	e.Run()
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "__inco_err :=") {
		t.Errorf("nested closure should get :=, got:\n%s", shadow)
	}
}

// Variable name containing underscore — replaceLastBlank must not touch it
func TestEngine_Must_UnderscoreInVarName(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Run() {
	my_result, _ := fmt.Println("hi") // @must
	_ = my_result
}
`,
	})
	e := NewEngine(dir)
	e.Run()
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "my_result") {
		t.Errorf("should preserve my_result, got:\n%s", shadow)
	}
	if strings.Contains(shadow, "my__inco_err") {
		t.Errorf("should not replace _ inside identifier, got:\n%s", shadow)
	}
}

// @must/@ensure comment on struct field — must NOT be processed
func TestEngine_DirectiveOnStructField_Ignored(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

type Config struct {
	Name string // @must not be empty
	Port int    // @ensure positive
}

func main() {}
`,
	})
	e := NewEngine(dir)
	e.Run()
	if len(e.Overlay.Replace) != 0 {
		t.Errorf("struct field comments should not trigger overlay, got %d entries", len(e.Overlay.Replace))
	}
}

// @must where original code already uses := — should not double-colon
func TestEngine_Must_AlreadyShortDecl(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Run() {
	result, _ := fmt.Println("hello") // @must
	fmt.Println(result)
}
`,
	})
	e := NewEngine(dir)
	e.Run()
	shadow := readShadow(t, e)
	// Should have exactly one := and it must not be :=:= or similar
	if strings.Contains(shadow, ":=:=") || strings.Contains(shadow, "::=") {
		t.Errorf("double declaration operator, got:\n%s", shadow)
	}
	if !strings.Contains(shadow, "__inco_err") {
		t.Errorf("should contain __inco_err, got:\n%s", shadow)
	}
}
