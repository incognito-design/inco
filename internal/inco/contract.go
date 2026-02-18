package inco

import (
	"strings"
)

// Kind represents the type of contract directive.
type Kind int

const (
	// Require defines a precondition (checked at function entry).
	Require Kind = iota
	// Ensure defines a postcondition (checked via defer at function exit).
	Ensure
	// Must asserts that an operation must not return an error (panic on error).
	Must
)

func (k Kind) String() string {
	switch k {
	case Require:
		return "require"
	case Ensure:
		return "ensure"
	case Must:
		return "must"
	default:
		return "unknown"
	}
}

// Directive represents a parsed contract annotation.
type Directive struct {
	Kind    Kind
	ND      bool     // -nd (non-defaulted) flag
	Vars    []string // variable names for -nd mode
	Expr    string   // custom boolean expression
	Message string   // custom error message (optional)
}

// ParseDirective parses a Go comment text into a Directive.
// Returns nil if the comment does not contain a contract directive.
//
// Supported forms:
//
//	// @require -nd var1, var2
//	// @require len(x) > 0, "x must not be empty"
//	// @ensure -nd result
//	// @must
func ParseDirective(text string) *Directive {
	// @require len(text) > 0, "text must not be empty"
	s := strings.TrimSpace(text)

	// Strip comment markers
	if strings.HasPrefix(s, "//") {
		s = strings.TrimSpace(s[2:])
	} else if strings.HasPrefix(s, "/*") {
		s = strings.TrimSuffix(strings.TrimPrefix(s, "/*"), "*/")
		s = strings.TrimSpace(s)
	}

	if !strings.HasPrefix(s, "@") {
		return nil
	}

	switch {
	case strings.HasPrefix(s, "@require"):
		return parseRequireOrEnsure(Require, strings.TrimPrefix(s, "@require"))
	case strings.HasPrefix(s, "@ensure"):
		return parseRequireOrEnsure(Ensure, strings.TrimPrefix(s, "@ensure"))
	case strings.HasPrefix(s, "@must"):
		return &Directive{Kind: Must}
	}

	return nil
}

func parseRequireOrEnsure(kind Kind, rest string) *Directive {
	rest = strings.TrimSpace(rest)
	d := &Directive{Kind: kind}

	if rest == "" {
		return d
	}

	// Check for -nd (non-defaulted) flag
	if strings.HasPrefix(rest, "-nd") {
		d.ND = true
		rest = strings.TrimSpace(rest[3:])
		if rest == "" {
			return d
		}
		for _, v := range strings.Split(rest, ",") {
			v = strings.TrimSpace(v)
			if v != "" {
				d.Vars = append(d.Vars, v)
			}
		}
		return d
	}

	// Expression form: expr [, "message"]
	if idx := strings.LastIndex(rest, ","); idx >= 0 {
		candidate := strings.TrimSpace(rest[idx+1:])
		if len(candidate) >= 2 && candidate[0] == '"' && candidate[len(candidate)-1] == '"' {
			d.Expr = strings.TrimSpace(rest[:idx])
			d.Message = candidate[1 : len(candidate)-1]
			return d
		}
	}

	d.Expr = rest
	return d
}
