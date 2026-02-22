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
	actionRe = regexp.MustCompile(`^(.+),\s*-(panic|return|continue|break|log)(?:\((.+)\))?\s*$`)

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
	"log":      ActionLog,
}

// ParseDirective extracts a Directive from a comment string.
// Returns nil when the comment is not a valid @inco: directive.
//
// Syntax: @inco: <expr>[, -action[(args...)]]
func ParseDirective(comment string) *Directive {
	body := stripComment(comment)
	// @inco: body != "", -return(nil)

	m := directiveRe.FindStringSubmatch(body)
	// @inco: m != nil, -return(nil)
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

	// @inco: d.Expr != "", -return(nil)
	return d
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// stripComment removes Go comment delimiters and returns trimmed content.
func stripComment(s string) string {
	s = strings.TrimSpace(s)
	m := commentRe.FindStringSubmatch(s)
	// @inco: m != nil, -return("")
	// m[1] is // content, m[2] is /* */ content; one will be empty.
	if m[1] != "" {
		return m[1]
	}
	return m[2]
}

// splitTopLevel splits s by top-level commas, respecting nested parens,
// brackets, braces, double-quoted strings, and raw strings (backtick).
func splitTopLevel(s string) []string {
	var result []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == '"':
			// Double-quoted string: skip until unescaped closing quote.
			i++
			for i < len(s) {
				if s[i] == '\\' {
					i++ // skip escaped char
				} else if s[i] == '"' {
					break
				}
				i++
			}
		case ch == '`':
			// Raw string: no escapes, skip until closing backtick.
			i++
			for i < len(s) && s[i] != '`' {
				i++
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
