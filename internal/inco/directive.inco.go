package inco

import (
	"regexp"
	"strings"
)

var (
	// directiveRe matches the body after stripping comment delimiters.
	// Group 1: everything after "@inco: "
	directiveRe = regexp.MustCompile(`^@inco:\s+(.+)$`)

	// actionRe splits "expr, -action(args)" into components.
	// Greedy (.+) backtracks to find the last top-level ", -action..." —
	// this naturally handles commas inside parenthesized sub-expressions.
	//
	// Group 1: expression
	// Group 2: action name (panic|return|continue|break)
	// Group 3: action arguments (optional)
	actionRe = regexp.MustCompile(`^(.+),\s*-(panic|return|continue|break)(?:\((.+)\))?\s*$`)

	// commentRe strips Go comment delimiters.
	// Group 1: content of // comment
	// Group 2: content of /* */ comment
	commentRe = regexp.MustCompile(`^//\s*(.*?)\s*$|^/\*\s*(.*?)\s*\*/$`)
)

// actionFromName maps action name strings to ActionKind.
var actionFromName = map[string]ActionKind{
	"panic":    ActionPanic,
	"return":   ActionReturn,
	"continue": ActionContinue,
	"break":    ActionBreak,
}

// ParseDirective extracts a Directive from a comment string.
// Returns nil when the comment is not a valid @inco: directive.
//
// Syntax: @inco: <expr>[, -action[(args...)]]
func ParseDirective(comment string) *Directive {
	body := stripComment(comment)
	if body == "" {
		return nil
	}

	m := directiveRe.FindStringSubmatch(body)
	if m == nil {
		return nil
	}
	rest := m[1]

	d := &Directive{Action: ActionPanic}
	if am := actionRe.FindStringSubmatch(rest); am != nil {
		d.Expr = strings.TrimSpace(am[1])
		d.Action = actionFromName[am[2]]
		if am[3] != "" {
			d.ActionArgs = splitTopLevel(am[3])
		}
	} else {
		d.Expr = rest
	}

	if d.Expr == "" {
		return nil
	}
	return d
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// stripComment removes Go comment delimiters and returns trimmed content.
func stripComment(s string) string {
	s = strings.TrimSpace(s)
	m := commentRe.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	// m[1] is // content, m[2] is /* */ content; one will be empty.
	if m[1] != "" {
		return m[1]
	}
	return m[2]
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
