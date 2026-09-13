package formula

import (
	"fmt"
	"sort"
	"strings"
)

// Issue is a single problem found by Lint.
type Issue struct {
	Pos     int
	Message string
}

func (i Issue) String() string { return fmt.Sprintf("position %d: %s", i.Pos, i.Message) }

// delimCloser maps each opening delimiter token to the closing token it
// expects.
var delimCloser = map[tokenKind]tokenKind{
	tokLParen:   tokRParen,
	tokLBrace:   tokRBrace,
	tokLBracket: tokRBracket,
}

var delimText = map[tokenKind]string{
	tokLParen: "(", tokRParen: ")",
	tokLBrace: "{", tokRBrace: "}",
	tokLBracket: "[", tokRBracket: "]",
}

// Lint checks src for mismatched or unbalanced parens, braces, and brackets,
// and for '#...' tokens that aren't a recognized error literal or table item
// specifier, reporting every problem it finds rather than stopping at the
// first one the way Parse does. Unlike Parse it doesn't care about '=',
// casing, or grammar beyond that, so it still produces useful diagnostics
// for input that's too broken to parse at all.
func Lint(src string) ([]Issue, error) {
	body := strings.TrimPrefix(src, "=")
	toks, err := lex(body)
	if err != nil {
		return nil, err
	}
	issues := append(lintDelimiters(toks), lintErrorTokens(toks)...)
	sort.SliceStable(issues, func(i, j int) bool { return issues[i].Pos < issues[j].Pos })
	return issues, nil
}

func lintDelimiters(toks []token) []Issue {
	var issues []Issue

	type frame struct {
		kind tokenKind
		pos  int
	}
	var stack []frame

	for _, tok := range toks {
		switch tok.kind {
		case tokLParen, tokLBrace, tokLBracket:
			stack = append(stack, frame{kind: tok.kind, pos: tok.pos})
		case tokRParen, tokRBrace, tokRBracket:
			if len(stack) == 0 {
				issues = append(issues, Issue{
					Pos:     tok.pos,
					Message: fmt.Sprintf("unexpected %q with no matching opener", delimText[tok.kind]),
				})
				continue
			}
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if delimCloser[top.kind] != tok.kind {
				issues = append(issues, Issue{
					Pos:     tok.pos,
					Message: fmt.Sprintf("%q does not match %q opened at position %d", delimText[tok.kind], delimText[top.kind], top.pos),
				})
			}
		}
	}

	// Anything still on the stack was opened but never closed, in the order
	// it was opened.
	for _, f := range stack {
		issues = append(issues, Issue{
			Pos:     f.pos,
			Message: fmt.Sprintf("unclosed %q", delimText[f.kind]),
		})
	}

	return issues
}

// knownErrorLiterals holds the lowercased form of every error literal Excel
// recognizes. Parse treats an ErrorLiteral's text as opaque - Lint is the
// place that actually checks it's one of the real ones.
var knownErrorLiterals = map[string]bool{
	"#ref!":   true,
	"#div/0!": true,
	"#n/a":    true,
	"#name?":  true,
	"#null!":  true,
	"#num!":   true,
	"#value!": true,
}

// lintErrorTokens flags any '#...' token that is neither a known error
// literal nor a known table item specifier (tableSpecifiers, in parser.go).
// The lexer can't tell those two apart - both start with '#' and are
// lexed as tokErrorLit - so a formula like "=Table1[#Headers]" and one like
// "=IF(A1=0,#DIV/0!,A1)" both need to check the same token kind against
// both lists. Because Lint doesn't parse, it can't tell whether a given
// token was meant as one or the other either, so a typo in either position
// is reported the same way: as not matching anything real.
func lintErrorTokens(toks []token) []Issue {
	var issues []Issue
	for i := 0; i < len(toks); i++ {
		tok := toks[i]
		if tok.kind != tokErrorLit {
			continue
		}
		text := tok.text
		if strings.EqualFold(text, "#This") && i+1 < len(toks) &&
			toks[i+1].kind == tokIdent && strings.EqualFold(toks[i+1].text, "Row") {
			text += " " + toks[i+1].text
			i++
		}
		lower := strings.ToLower(text)
		if knownErrorLiterals[lower] {
			continue
		}
		if _, ok := tableSpecifiers[lower]; ok {
			continue
		}
		issues = append(issues, Issue{
			Pos:     tok.pos,
			Message: fmt.Sprintf("%q is not a recognized error literal or table item specifier", text),
		})
	}
	return issues
}
