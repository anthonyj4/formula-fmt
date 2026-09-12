package formula

import (
	"fmt"
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
// reporting every problem it finds rather than stopping at the first one the
// way Parse does. Unlike Parse it doesn't care about '=', casing, or grammar
// beyond delimiter matching, so it still produces useful diagnostics for
// input that's too broken to parse at all.
func Lint(src string) ([]Issue, error) {
	body := strings.TrimPrefix(src, "=")
	toks, err := lex(body)
	if err != nil {
		return nil, err
	}
	return lintDelimiters(toks), nil
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
