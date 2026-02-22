package inco

import (
	"reflect"
	"testing"
)

// ---------------------------------------------------------------------------
// ParseDirective — basic recognition
// ---------------------------------------------------------------------------

func TestParseDirective_Nil(t *testing.T) {
	for _, input := range []string{
		"",
		"// just a comment",
		"// @inco",     // missing colon
		"// @inco:",    // no expression
		"// @inco:   ", // whitespace only
		"/* block comment */",
		"// @INCO: x > 0", // wrong case
	} {
		if d := ParseDirective(input); d != nil {
			t.Errorf("ParseDirective(%q) = %+v, want nil", input, d)
		}
	}
}

func TestParseDirective_ExprOnly(t *testing.T) {
	d := ParseDirective("// @inco: x > 0")
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Expr != "x > 0" {
		t.Errorf("Expr = %q, want %q", d.Expr, "x > 0")
	}
	if d.Action != ActionPanic {
		t.Errorf("Action = %v, want ActionPanic", d.Action)
	}
	if len(d.ActionArgs) != 0 {
		t.Errorf("ActionArgs = %v, want empty", d.ActionArgs)
	}
}

func TestParseDirective_FuncCallExpr(t *testing.T) {
	d := ParseDirective("// @inco: len(name) > 0")
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Expr != "len(name) > 0" {
		t.Errorf("Expr = %q", d.Expr)
	}
}

// ---------------------------------------------------------------------------
// Actions — comma+dash syntax
// ---------------------------------------------------------------------------

func TestParseDirective_PanicBare(t *testing.T) {
	d := ParseDirective("// @inco: x > 0, -panic")
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Action != ActionPanic {
		t.Errorf("Action = %v, want ActionPanic", d.Action)
	}
	if d.Expr != "x > 0" {
		t.Errorf("Expr = %q", d.Expr)
	}
}

func TestParseDirective_PanicWithMessage(t *testing.T) {
	d := ParseDirective(`// @inco: x > 0, -panic("x must be positive")`)
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Action != ActionPanic {
		t.Errorf("Action = %v", d.Action)
	}
	want := []string{`"x must be positive"`}
	if !reflect.DeepEqual(d.ActionArgs, want) {
		t.Errorf("ActionArgs = %v, want %v", d.ActionArgs, want)
	}
}

func TestParseDirective_PanicFmtSprintf(t *testing.T) {
	d := ParseDirective(`// @inco: x > 0, -panic(fmt.Sprintf("bad: %d", x))`)
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Action != ActionPanic {
		t.Errorf("Action = %v", d.Action)
	}
	want := []string{`fmt.Sprintf("bad: %d", x)`}
	if !reflect.DeepEqual(d.ActionArgs, want) {
		t.Errorf("ActionArgs = %v, want %v", d.ActionArgs, want)
	}
}

func TestParseDirective_ReturnBare(t *testing.T) {
	d := ParseDirective("// @inco: x > 0, -return")
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Action != ActionReturn {
		t.Errorf("Action = %v, want ActionReturn", d.Action)
	}
	if len(d.ActionArgs) != 0 {
		t.Errorf("ActionArgs = %v, want empty", d.ActionArgs)
	}
}

func TestParseDirective_ReturnSingleValue(t *testing.T) {
	d := ParseDirective("// @inco: x > 0, -return(-1)")
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Action != ActionReturn {
		t.Errorf("Action = %v", d.Action)
	}
	want := []string{"-1"}
	if !reflect.DeepEqual(d.ActionArgs, want) {
		t.Errorf("ActionArgs = %v, want %v", d.ActionArgs, want)
	}
}

func TestParseDirective_ReturnMultiValue(t *testing.T) {
	d := ParseDirective(`// @inco: len(s) > 0, -return(0, fmt.Errorf("empty"))`)
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Action != ActionReturn {
		t.Errorf("Action = %v", d.Action)
	}
	want := []string{"0", `fmt.Errorf("empty")`}
	if !reflect.DeepEqual(d.ActionArgs, want) {
		t.Errorf("ActionArgs = %v, want %v", d.ActionArgs, want)
	}
	if d.Expr != "len(s) > 0" {
		t.Errorf("Expr = %q", d.Expr)
	}
}

func TestParseDirective_Continue(t *testing.T) {
	d := ParseDirective("// @inco: n > 0, -continue")
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Action != ActionContinue {
		t.Errorf("Action = %v, want ActionContinue", d.Action)
	}
	if d.Expr != "n > 0" {
		t.Errorf("Expr = %q", d.Expr)
	}
}

func TestParseDirective_Break(t *testing.T) {
	d := ParseDirective("// @inco: n != 42, -break")
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Action != ActionBreak {
		t.Errorf("Action = %v, want ActionBreak", d.Action)
	}
	if d.Expr != "n != 42" {
		t.Errorf("Expr = %q", d.Expr)
	}
}

func TestBuildPanicBody_Do(t *testing.T) {
	e := NewEngine(t.TempDir())
	d := &Directive{Action: ActionDo, Expr: "x != nil", ActionArgs: []string{`log.Println("x is nil")`}}
	body := e.buildPanicBody(d, "test.go", 1)
	want := `log.Println("x is nil")`
	if body != want {
		t.Errorf("got %q, want %q", body, want)
	}
}

func TestBuildPanicBody_DoMultiExpr(t *testing.T) {
	e := NewEngine(t.TempDir())
	d := &Directive{Action: ActionDo, Expr: "ok", ActionArgs: []string{"count++", `log.Println("fail")`}}
	body := e.buildPanicBody(d, "test.go", 1)
	want := `count++; log.Println("fail")`
	if body != want {
		t.Errorf("got %q, want %q", body, want)
	}
}

func TestParseDirective_DoNotParsed(t *testing.T) {
	// -do is internal only — ParseDirective should not recognize it.
	d := ParseDirective(`// @inco: x != nil, -do(log.Println("x is nil"))`)
	if d == nil {
		t.Fatal("got nil — should parse as expr-only with default panic")
	}
	if d.Action != ActionPanic {
		t.Errorf("Action = %v, want ActionPanic (do should not be parsed)", d.Action)
	}
}

func TestParseDirective_Log(t *testing.T) {
	d := ParseDirective(`// @inco: x > 0, -log("x must be positive", x)`)
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Action != ActionLog {
		t.Errorf("Action = %v, want ActionLog", d.Action)
	}
	if d.Expr != "x > 0" {
		t.Errorf("Expr = %q", d.Expr)
	}
	if len(d.ActionArgs) != 2 {
		t.Errorf("ActionArgs = %v, want 2 args", d.ActionArgs)
	}
}

// ---------------------------------------------------------------------------
// Edge cases — comma inside expression
// ---------------------------------------------------------------------------

func TestParseDirective_CommaInFuncCallIsNotAction(t *testing.T) {
	// The comma inside foo(a, b) should NOT be treated as an action separator.
	d := ParseDirective("// @inco: foo(a, b) > 0")
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Expr != "foo(a, b) > 0" {
		t.Errorf("Expr = %q, want %q", d.Expr, "foo(a, b) > 0")
	}
	if d.Action != ActionPanic {
		t.Errorf("Action = %v, want ActionPanic", d.Action)
	}
}

func TestParseDirective_CommaInFuncCallWithAction(t *testing.T) {
	d := ParseDirective(`// @inco: foo(a, b) > 0, -panic("bad")`)
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Expr != "foo(a, b) > 0" {
		t.Errorf("Expr = %q", d.Expr)
	}
	if d.Action != ActionPanic {
		t.Errorf("Action = %v", d.Action)
	}
	want := []string{`"bad"`}
	if !reflect.DeepEqual(d.ActionArgs, want) {
		t.Errorf("ActionArgs = %v, want %v", d.ActionArgs, want)
	}
}

func TestParseDirective_MapLiteralComma(t *testing.T) {
	// m[k] is not depth-tracked by parens, but this should still be expr-only.
	d := ParseDirective("// @inco: m[k] > 0")
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Expr != "m[k] > 0" {
		t.Errorf("Expr = %q", d.Expr)
	}
}

func TestParseDirective_NestedParenComma(t *testing.T) {
	d := ParseDirective("// @inco: f(g(a, b), c) != nil, -return(-1)")
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Expr != "f(g(a, b), c) != nil" {
		t.Errorf("Expr = %q", d.Expr)
	}
	if d.Action != ActionReturn {
		t.Errorf("Action = %v", d.Action)
	}
}

// ---------------------------------------------------------------------------
// Block comment form
// ---------------------------------------------------------------------------

func TestParseDirective_BlockComment(t *testing.T) {
	d := ParseDirective("/* @inco: x > 0 */")
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Expr != "x > 0" {
		t.Errorf("Expr = %q", d.Expr)
	}
}

// ---------------------------------------------------------------------------
// stripComment helper
// ---------------------------------------------------------------------------

func TestStripComment(t *testing.T) {
	cases := []struct {
		input, want string
	}{
		{"// hello", "hello"},
		{"//hello", "hello"},
		{"/* block */", "block"},
		{"  // spaced  ", "spaced"},
		{"not a comment", ""},
	}
	for _, c := range cases {
		got := stripComment(c.input)
		if got != c.want {
			t.Errorf("stripComment(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// splitTopLevel helper
// ---------------------------------------------------------------------------

func TestSplitTopLevel(t *testing.T) {
	cases := []struct {
		input string
		want  []string
	}{
		{"a, b, c", []string{"a", "b", "c"}},
		{`f(x, y), z`, []string{"f(x, y)", "z"}},
		{`"a,b", c`, []string{`"a,b"`, "c"}},
		{"single", []string{"single"}},
		{"", nil},
		// Raw string with comma inside.
		{"`a,b`, c", []string{"`a,b`", "c"}},
		// Raw string with backslash (no escaping in raw strings).
		{"`a\\b`, c", []string{"`a\\b`", "c"}},
		// Double-quoted string with escaped quote.
		{`"a\"b", c`, []string{`"a\"b"`, "c"}},
		// Double-quoted string with escaped backslash before closing quote.
		{`"a\\", c`, []string{`"a\\"`, "c"}},
	}
	for _, c := range cases {
		got := splitTopLevel(c.input)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("splitTopLevel(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}
