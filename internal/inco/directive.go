// Package inco implements a compile-time code injection engine.
//
// Directives:
//
//	// @require <expression> [panic[("msg")]]
//	result, _ := foo() // @must [panic[("msg")]]
//	v, _ := m[k]       // @ensure [panic[("msg")]]
//
// The only action is panic (the default).
// Use panic("custom message") to customise the message.
package inco

import "strings"

// ---------------------------------------------------------------------------
// Action
// ---------------------------------------------------------------------------

// ActionKind identifies the response to a require violation.
type ActionKind int

const (
	ActionPanic ActionKind = iota // default — only action
)

func (k ActionKind) String() string {
	switch k {
	case ActionPanic:
		return "panic"
	default:
		return "unknown"
	}
}

// ---------------------------------------------------------------------------
// DirectiveKind
// ---------------------------------------------------------------------------

// DirectiveKind distinguishes the three directive types.
type DirectiveKind int

const (
	KindRequire DirectiveKind = iota // standalone: @require <expr>
	KindMust                         // inline: error check
	KindEnsure                       // inline: ok/bool check
)

func (k DirectiveKind) String() string {
	switch k {
	case KindRequire:
		return "require"
	case KindMust:
		return "must"
	case KindEnsure:
		return "ensure"
	default:
		return "unknown"
	}
}

// ---------------------------------------------------------------------------
// Directive
// ---------------------------------------------------------------------------

// Directive is the parsed form of a single @require / @must / @ensure comment.
type Directive struct {
	Kind       DirectiveKind // require, must, or ensure
	Action     ActionKind    // always ActionPanic
	ActionArgs []string      // e.g. panic("msg") → ['"msg"']
	Expr       string        // the Go boolean expression (@require only)
}

// ParseDirective extracts a Directive from a comment string.
// Returns nil when the comment is not a valid directive.
func ParseDirective(comment string) *Directive {
	s := stripComment(comment)
	if s == "" {
		return nil
	}

	var kind DirectiveKind
	var keyword string

	switch {
	case strings.HasPrefix(s, "@require"):
		kind = KindRequire
		keyword = "@require"
	case strings.HasPrefix(s, "@must"):
		kind = KindMust
		keyword = "@must"
	case strings.HasPrefix(s, "@ensure"):
		kind = KindEnsure
		keyword = "@ensure"
	default:
		return nil
	}

	rest := strings.TrimSpace(s[len(keyword):])
	d := &Directive{Kind: kind, Action: ActionPanic}

	switch kind {
	case KindRequire:
		// syntax: @require <expression> [panic[("msg")]]
		if rest == "" {
			return nil // expression is mandatory
		}
		d.Expr = parseTrailingPanic(d, rest)
		if d.Expr == "" {
			return nil
		}
	case KindMust, KindEnsure:
		// syntax: @must/@ensure [panic[("msg")]]
		if rest == "" {
			return d // bare → default panic
		}
		if !parsePanicAction(d, rest) {
			return nil // trailing text is not a valid action
		}
	}

	return d
}

// parseTrailingPanic extracts an optional panic keyword (and args) from
// the END of rest.  Returns the expression part (everything before panic).
//
// Syntax: <expression> [panic[("msg")]]
//
// Since panic is a Go builtin, it cannot appear as a standalone identifier
// in a valid Go expression, making the split unambiguous.
func parseTrailingPanic(d *Directive, rest string) string {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return ""
	}

	// Try "expr panic(args...)" — find rightmost " panic("
	needle := " panic("
	if idx := strings.LastIndex(rest, needle); idx >= 0 {
		argStart := idx + len(" panic") // position of '('
		args, remaining, ok := parseActionArgs(rest[argStart:])
		if ok && strings.TrimSpace(remaining) == "" {
			d.Action = ActionPanic
			d.ActionArgs = args
			return strings.TrimSpace(rest[:idx])
		}
	}

	// Try "expr panic" — bare panic at end
	suffix := " panic"
	if strings.HasSuffix(rest, suffix) {
		d.Action = ActionPanic
		return strings.TrimSpace(rest[:len(rest)-len(suffix)])
	}

	return rest // no action found — entire rest is the expression
}

// parsePanicAction attempts to parse "panic" (and optional parenthesised
// arguments) from the front of rest.  Used for @must / @ensure.
func parsePanicAction(d *Directive, rest string) bool {
	if !strings.HasPrefix(rest, "panic") {
		return false
	}
	after := rest[len("panic"):]
	if len(after) > 0 && after[0] != ' ' && after[0] != '\t' && after[0] != '(' {
		return false
	}
	d.Action = ActionPanic
	after = strings.TrimSpace(after)
	if strings.HasPrefix(after, "(") {
		args, _, ok := parseActionArgs(after)
		if ok {
			d.ActionArgs = args
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// stripComment removes Go comment delimiters and returns trimmed content.
func stripComment(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "//") {
		return strings.TrimSpace(s[2:])
	}
	if strings.HasPrefix(s, "/*") && strings.HasSuffix(s, "*/") {
		return strings.TrimSpace(s[2 : len(s)-2])
	}
	return ""
}

// parseActionArgs parses "(arg1, arg2, ...)" respecting nested parens/strings.
// Returns parsed args, the remaining string after ')', and whether parsing succeeded.
func parseActionArgs(s string) ([]string, string, bool) {
	if len(s) == 0 || s[0] != '(' {
		return nil, s, false
	}
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				inner := s[1:i]
				args := splitTopLevel(inner)
				return args, s[i+1:], true
			}
		case '"':
			// Skip string literal
			i++
			for i < len(s) && s[i] != '"' {
				if s[i] == '\\' {
					i++ // skip escaped char
				}
				i++
			}
		}
	}
	return nil, s, false // unmatched paren
}

// splitTopLevel splits s by top-level commas, respecting nested parens,
// brackets, braces and double-quoted strings.
func splitTopLevel(s string) []string {
	var result []string
	depth := 0
	inStr := false
	start := 0
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == '"' && !inStr:
			inStr = true
		case ch == '"' && inStr && (i == 0 || s[i-1] != '\\'):
			inStr = false
		case inStr:
			if ch == '\\' {
				i++ // skip next
			}
		case ch == '(' || ch == '[' || ch == '{':
			depth++
		case ch == ')' || ch == ']' || ch == '}':
			depth--
		case ch == ',' && depth == 0:
			result = append(result, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	if last := strings.TrimSpace(s[start:]); last != "" {
		result = append(result, last)
	}
	return result
}
