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

// ---------------------------------------------------------------------------
// No directives — no overlay
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Default action (panic)
// ---------------------------------------------------------------------------

func TestEngine_DefaultPanic(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Greet(name string) {
	// @inco: len(name) > 0
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
	if !strings.Contains(shadow, "inco violation") {
		t.Error("shadow should contain default violation message")
	}
}

// ---------------------------------------------------------------------------
// Custom panic message
// ---------------------------------------------------------------------------

func TestEngine_PanicCustomMessage(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Process(x int) {
	// @inco: x > 0, -panic("x must be positive")
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

func TestEngine_PanicFmtSprintf(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Check(x int) {
	// @inco: x > 0, -panic(fmt.Sprintf("bad value: %d", x))
	fmt.Println(x)
}
`,
	})
	e := NewEngine(dir)
	e.Run()
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, `panic(fmt.Sprintf("bad value: %d", x))`) {
		t.Errorf("shadow should contain custom panic with Sprintf, got:\n%s", shadow)
	}
}

// ---------------------------------------------------------------------------
// Multiple directives in same function
// ---------------------------------------------------------------------------

func TestEngine_MultipleDirectives(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Process(name string, age int) {
	// @inco: len(name) > 0
	// @inco: age > 0
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

// ---------------------------------------------------------------------------
// //line directives
// ---------------------------------------------------------------------------

func TestEngine_LineDirectives(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Hello(name string) {
	// @inco: len(name) > 0
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

// ---------------------------------------------------------------------------
// Overlay JSON
// ---------------------------------------------------------------------------

func TestEngine_OverlayJSON(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Do(x int) {
	// @inco: x > 0
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

// ---------------------------------------------------------------------------
// Skips hidden directories
// ---------------------------------------------------------------------------

func TestEngine_SkipsHiddenDirs(t *testing.T) {
	dir := setupDir(t, map[string]string{
		".hidden/main.go": `package hidden

func X(x int) {
	// @inco: x > 0
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

// ---------------------------------------------------------------------------
// Content hash stability
// ---------------------------------------------------------------------------

func TestEngine_ContentHashStable(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Do(x int) {
	// @inco: x > 0
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

// ---------------------------------------------------------------------------
// Closure support
// ---------------------------------------------------------------------------

func TestEngine_Closure(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Outer() {
	f := func(x int) {
		// @inco: x > 0
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
// -return action
// ---------------------------------------------------------------------------

func TestEngine_Return(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Positive(x int) int {
	// @inco: x > 0, -return(-1)
	return x * 2
}
`,
	})
	e := NewEngine(dir)
	e.Run()
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "if !(x > 0)") {
		t.Errorf("should contain negated condition, got:\n%s", shadow)
	}
	if !strings.Contains(shadow, "return -1") {
		t.Errorf("should contain return -1, got:\n%s", shadow)
	}
}

func TestEngine_ReturnMultiValue(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Parse(s string) (int, error) {
	// @inco: len(s) > 0, -return(0, fmt.Errorf("empty"))
	return len(s), nil
}
`,
	})
	e := NewEngine(dir)
	e.Run()
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, `return 0, fmt.Errorf("empty")`) {
		t.Errorf("should contain multi-value return, got:\n%s", shadow)
	}
}

func TestEngine_ReturnBare(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Check(x int) {
	// @inco: x > 0, -return
	fmt.Println(x)
}
`,
	})
	e := NewEngine(dir)
	e.Run()
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "return\n") {
		t.Errorf("should contain bare return, got:\n%s", shadow)
	}
}

// ---------------------------------------------------------------------------
// -continue action
// ---------------------------------------------------------------------------

func TestEngine_Continue(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func PrintPositive(nums []int) {
	for _, n := range nums {
		// @inco: n > 0, -continue
		fmt.Println(n)
	}
}
`,
	})
	e := NewEngine(dir)
	e.Run()
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "if !(n > 0)") {
		t.Errorf("should contain negated condition, got:\n%s", shadow)
	}
	if !strings.Contains(shadow, "continue") {
		t.Errorf("should contain continue, got:\n%s", shadow)
	}
}

// ---------------------------------------------------------------------------
// -break action
// ---------------------------------------------------------------------------

func TestEngine_Break(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func FindFirst(nums []int) {
	for _, n := range nums {
		// @inco: n != 42, -break
		fmt.Println(n)
	}
}
`,
	})
	e := NewEngine(dir)
	e.Run()
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "if !(n != 42)") {
		t.Errorf("should contain negated condition, got:\n%s", shadow)
	}
	if !strings.Contains(shadow, "break") {
		t.Errorf("should contain break, got:\n%s", shadow)
	}
}

// ---------------------------------------------------------------------------
// Struct field comments — should NOT be processed
// ---------------------------------------------------------------------------

func TestEngine_StructFieldCommentIgnored(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

type Config struct {
	Name string // @inco: not empty
	Port int    // some comment
}

func main() {}
`,
	})
	e := NewEngine(dir)
	e.Run()
	// Struct field inline comment is not a standalone comment line,
	// so it should NOT produce an overlay.
	if len(e.Overlay.Replace) != 0 {
		t.Errorf("struct field comment should not trigger overlay, got %d entries", len(e.Overlay.Replace))
	}
}

// ---------------------------------------------------------------------------
// Multiple files — all processed
// ---------------------------------------------------------------------------

func TestEngine_MultipleFiles(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"a.go": `package main

func A(x int) {
	// @inco: x > 0
	_ = x
}
`,
		"b.go": `package main

func B(y int) {
	// @inco: y > 0
	_ = y
}
`,
	})
	e := NewEngine(dir)
	e.Run()
	if len(e.Overlay.Replace) != 2 {
		t.Errorf("expected 2 overlay entries, got %d", len(e.Overlay.Replace))
	}
}

// ---------------------------------------------------------------------------
// Test files (_test.go) should be skipped
// ---------------------------------------------------------------------------

func TestEngine_SkipsTestFiles(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go":      "package main\n\nfunc main() {}\n",
		"main_test.go": "package main\n\nfunc TestFoo() {\n\t// @inco: true\n}\n",
	})
	e := NewEngine(dir)
	e.Run()
	if len(e.Overlay.Replace) != 0 {
		t.Errorf("should skip _test.go, got %d entries", len(e.Overlay.Replace))
	}
}

// ---------------------------------------------------------------------------
// Import injection — fmt.Errorf in action args
// ---------------------------------------------------------------------------

func TestEngine_ImportInjection(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Do(s string) (int, error) {
	// @inco: len(s) > 0, -return(0, fmt.Errorf("empty"))
	return len(s), nil
}
`,
	})
	e := NewEngine(dir)
	e.Run()
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, `"fmt"`) {
		t.Errorf("should inject fmt import, got:\n%s", shadow)
	}
}

// ---------------------------------------------------------------------------
// Deeply nested closure
// ---------------------------------------------------------------------------

func TestEngine_NestedClosure(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Outer() {
	a := func() {
		b := func(x int) {
			// @inco: x > 0
			fmt.Println(x)
		}
		b(1)
	}
	a()
}
`,
	})
	e := NewEngine(dir)
	e.Run()
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "!(x > 0)") {
		t.Error("should process directive in nested closure")
	}
}

// ---------------------------------------------------------------------------
// Vendor / testdata directories skipped
// ---------------------------------------------------------------------------

func TestEngine_SkipsVendor(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go":        "package main\n\nfunc main() {}\n",
		"vendor/v/v.go":  "package v\n\nfunc V(x int) {\n\t// @inco: x > 0\n}\n",
		"testdata/td.go": "package td\n\nfunc TD(x int) {\n\t// @inco: x > 0\n}\n",
	})
	e := NewEngine(dir)
	e.Run()
	if len(e.Overlay.Replace) != 0 {
		t.Errorf("should skip vendor/testdata, got %d entries", len(e.Overlay.Replace))
	}
}
