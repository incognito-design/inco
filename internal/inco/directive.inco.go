package inco

import "strings"

// actionKeywords maps -prefixed action names to their ActionKind.
var actionKeywords = map[string]ActionKind{
	"-panic":    ActionPanic,
	"-return":   ActionReturn,
	"-continue": ActionContinue,
	"-break":    ActionBreak,
}

// ParseDirective extracts a Directive from a comment string.
// Returns nil when the comment is not a valid @inco: directive.
//
// Syntax: @inco: <expr>[, -action[(args...)]]
func ParseDirective(comment string) *Directive {
	s := stripComment(comment)
	if s == "" {
		return nil
	}

	if !strings.HasPrefix(s, "@inco:") {
		return nil
	}

	rest := strings.TrimSpace(s[len("@inco:"):])
	if rest == "" {
		return nil // expression is mandatory
	}

	d := &Directive{Action: ActionPanic}
	return parseRequireRest(d, rest)
}

// parseRequireRest parses the rest after the "@inco:" keyword.
//
// Two forms:
//   - expression only
//   - expression, -action[(args...)]
func parseRequireRest(d *Directive, rest string) *Directive {
	// Split on the last top-level comma to find the action part.
	commaIdx := findLastTopLevelComma(rest)
	if commaIdx >= 0 {
		afterComma := strings.TrimSpace(rest[commaIdx+1:])
		if strings.HasPrefix(afterComma, "-") {
			// Try to match an action keyword.
			for keyword, action := range actionKeywords {
				if !strings.HasPrefix(afterComma, keyword) {
					continue
				}
				after := afterComma[len(keyword):]
				if len(after) == 0 {
					// bare action: -continue, -break, -return, -panic
					d.Action = action
					d.Expr = strings.TrimSpace(rest[:commaIdx])
					if d.Expr == "" {
						return nil
					}
					return d
				}
				if after[0] == '(' {
					// action with args: -panic("msg"), -return(0, err)
					args, remaining, ok := parseActionArgs(after)
					if ok && strings.TrimSpace(remaining) == "" {
						d.Action = action
						d.ActionArgs = args
						d.Expr = strings.TrimSpace(rest[:commaIdx])
						if d.Expr == "" {
							return nil
						}
						return d
					}
				}
				// Not a valid action with that keyword; keep looking.
			}
		}
	}

	// No comma+action found — entire rest is the expression.
	d.Expr = rest
	return d
}

// findLastTopLevelComma returns the index of the last comma at depth 0,
// respecting parentheses, brackets, braces and string literals.
// Returns -1 if no top-level comma is found.
func findLastTopLevelComma(s string) int {
	depth := 0
	inStr := false
	lastComma := -1
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
			lastComma = i
		}
	}
	return lastComma
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
