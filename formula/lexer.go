package formula

import (
	"fmt"
	"strings"
)

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokNumber
	tokString
	tokIdent
	tokErrorLit
	tokPlus
	tokMinus
	tokStar
	tokSlash
	tokCaret
	tokAmp
	tokPercent
	tokEq
	tokNotEq
	tokLt
	tokGt
	tokLe
	tokGe
	tokLParen
	tokRParen
	tokComma
	tokColon
	tokBang
	tokLBrace
	tokRBrace
	tokSemi
	tokLBracket
	tokRBracket
	tokAt
)

// token positions are rune offsets into the formula body, i.e. after the
// leading '=' has already been stripped by Parse.
type token struct {
	kind   tokenKind
	text   string
	pos    int
	quoted bool
}

func lex(src string) ([]token, error) {
	runes := []rune(src)
	n := len(runes)
	var toks []token
	i := 0

	peek := func(off int) rune {
		if i+off >= n {
			return 0
		}
		return runes[i+off]
	}

	simple := func(k tokenKind, s string) {
		toks = append(toks, token{kind: k, text: s, pos: i})
		i += len(s)
	}

	for i < n {
		c := runes[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '+':
			simple(tokPlus, "+")
		case c == '-':
			simple(tokMinus, "-")
		case c == '*':
			simple(tokStar, "*")
		case c == '/':
			simple(tokSlash, "/")
		case c == '^':
			simple(tokCaret, "^")
		case c == '&':
			simple(tokAmp, "&")
		case c == '%':
			simple(tokPercent, "%")
		case c == '(':
			simple(tokLParen, "(")
		case c == ')':
			simple(tokRParen, ")")
		case c == ',':
			simple(tokComma, ",")
		case c == ':':
			simple(tokColon, ":")
		case c == '!':
			simple(tokBang, "!")
		case c == '{':
			simple(tokLBrace, "{")
		case c == '}':
			simple(tokRBrace, "}")
		case c == ';':
			simple(tokSemi, ";")
		case c == '[':
			simple(tokLBracket, "[")
		case c == ']':
			simple(tokRBracket, "]")
		case c == '@':
			simple(tokAt, "@")
		case c == '=':
			simple(tokEq, "=")
		case c == '<':
			if peek(1) == '>' {
				simple(tokNotEq, "<>")
			} else if peek(1) == '=' {
				simple(tokLe, "<=")
			} else {
				simple(tokLt, "<")
			}
		case c == '>':
			if peek(1) == '=' {
				simple(tokGe, ">=")
			} else {
				simple(tokGt, ">")
			}
		case c == '"':
			start := i
			var sb strings.Builder
			i++
			closed := false
			for i < n {
				if runes[i] == '"' {
					if peek(1) == '"' {
						sb.WriteRune('"')
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				sb.WriteRune(runes[i])
				i++
			}
			if !closed {
				return nil, fmt.Errorf("unterminated string literal at position %d", start)
			}
			toks = append(toks, token{kind: tokString, text: sb.String(), pos: start})
		case c == '\'':
			// quoted sheet name, e.g. 'Q1 Report'!A1
			start := i
			var sb strings.Builder
			i++
			closed := false
			for i < n {
				if runes[i] == '\'' {
					if peek(1) == '\'' {
						sb.WriteRune('\'')
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				sb.WriteRune(runes[i])
				i++
			}
			if !closed {
				return nil, fmt.Errorf("unterminated quoted name at position %d", start)
			}
			toks = append(toks, token{kind: tokIdent, text: sb.String(), pos: start, quoted: true})
		case c == '#':
			start := i
			j := i + 1
			for j < n && (isLetter(runes[j]) || isDigit(runes[j]) || runes[j] == '/') {
				j++
			}
			if j < n && (runes[j] == '!' || runes[j] == '?') {
				j++
			}
			toks = append(toks, token{kind: tokErrorLit, text: string(runes[start:j]), pos: start})
			i = j
		case isDigit(c) || (c == '.' && isDigit(peek(1))):
			start := i
			for i < n && isDigit(runes[i]) {
				i++
			}
			if i < n && runes[i] == '.' {
				i++
				for i < n && isDigit(runes[i]) {
					i++
				}
			}
			if i < n && (runes[i] == 'e' || runes[i] == 'E') {
				j := i + 1
				if j < n && (runes[j] == '+' || runes[j] == '-') {
					j++
				}
				if j < n && isDigit(runes[j]) {
					i = j
					for i < n && isDigit(runes[i]) {
						i++
					}
				}
			}
			toks = append(toks, token{kind: tokNumber, text: string(runes[start:i]), pos: start})
		case isLetter(c) || c == '_' || c == '$':
			// '$' only makes sense as an absolute-reference marker (e.g.
			// "$B$2"), but it's simplest to fold it into the identifier
			// here and let the parser reject anything that isn't a valid
			// cell reference shape.
			start := i
			for i < n && (isLetter(runes[i]) || isDigit(runes[i]) || runes[i] == '_' || runes[i] == '.' || runes[i] == '$') {
				i++
			}
			toks = append(toks, token{kind: tokIdent, text: string(runes[start:i]), pos: start})
		default:
			return nil, fmt.Errorf("unexpected character %q at position %d", string(c), i)
		}
	}
	toks = append(toks, token{kind: tokEOF, text: "", pos: n})
	return toks, nil
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}
