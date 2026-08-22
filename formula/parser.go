// Package formula parses spreadsheet formulas (the kind that live in a
// single cell, starting with "=") into an AST and prints that AST back out
// in a canonical form.
package formula

import (
	"fmt"
	"regexp"
	"strings"
)

// Node is any element of a parsed formula's AST.
type Node interface {
	node()
}

type Number struct{ Text string }
type String struct{ Value string }
type Boolean struct{ Value bool }
type ErrorLiteral struct{ Text string }

type CellRef struct {
	Sheet       string
	SheetQuoted bool
	ColAbsolute bool
	RowAbsolute bool
	Column      string
	Row         string
}

type Range struct {
	From CellRef
	To   CellRef
}

type UnaryExpr struct {
	Op string
	X  Node
}

type PercentExpr struct{ X Node }

type BinaryExpr struct {
	Op   string
	X, Y Node
}

type Paren struct{ X Node }

// Call is a function call. An entry in Args is nil for an omitted argument
// (e.g. the middle slot in SUM(A1,,B2)), which is only produced in lenient
// mode.
type Call struct {
	Name string
	Args []Node
}

func (*Number) node()       {}
func (*String) node()       {}
func (*Boolean) node()      {}
func (*ErrorLiteral) node() {}
func (*CellRef) node()      {}
func (*Range) node()        {}
func (*UnaryExpr) node()    {}
func (*PercentExpr) node()  {}
func (*BinaryExpr) node()   {}
func (*Paren) node()        {}
func (*Call) node()         {}

// Formula is a fully parsed cell formula.
type Formula struct {
	Body Node
}

var cellRefPattern = regexp.MustCompile(`^(\$?)([A-Za-z]{1,3})(\$?)([0-9]+)$`)

// Parse validates src and builds its AST.
//
// In strict mode (lenient == false) the formula must start with '=', cell
// references and function names must already be uppercase, and function
// calls may not contain empty or trailing arguments. Lenient mode relaxes
// all of that and normalizes it away instead of rejecting it.
func Parse(src string, lenient bool) (*Formula, error) {
	work := src
	if lenient {
		work = strings.TrimSpace(work)
	}

	hasEquals := strings.HasPrefix(work, "=")
	switch {
	case hasEquals:
		work = work[1:]
	case !lenient:
		return nil, fmt.Errorf("formula must start with '=' (use --lenient to allow bare expressions)")
	}

	if lenient {
		work = strings.TrimSpace(work)
	}
	if work == "" {
		return nil, fmt.Errorf("empty formula body")
	}

	toks, err := lex(work)
	if err != nil {
		return nil, err
	}

	p := &parser{tokens: toks, lenient: lenient}
	body, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if p.cur().kind != tokEOF {
		return nil, fmt.Errorf("unexpected token %q at position %d", p.cur().text, p.cur().pos)
	}
	return &Formula{Body: body}, nil
}

type parser struct {
	tokens  []token
	pos     int
	lenient bool
}

func (p *parser) cur() token { return p.tokens[p.pos] }

func (p *parser) advance() {
	if p.pos < len(p.tokens)-1 {
		p.pos++
	}
}

// Operator precedence, loosest to tightest binding: comparison, &, + -,
// * /, unary +/- and ^ (Excel gives unary minus a tighter bind than ^, so
// -2^2 is (-2)^2 == 4, not -(2^2)), then % as a postfix on whatever's left.

func (p *parser) parseExpr() (Node, error) { return p.parseCompare() }

func (p *parser) parseCompare() (Node, error) {
	x, err := p.parseConcat()
	if err != nil {
		return nil, err
	}
	for {
		var op string
		switch p.cur().kind {
		case tokEq:
			op = "="
		case tokNotEq:
			op = "<>"
		case tokLt:
			op = "<"
		case tokGt:
			op = ">"
		case tokLe:
			op = "<="
		case tokGe:
			op = ">="
		default:
			return x, nil
		}
		p.advance()
		y, err := p.parseConcat()
		if err != nil {
			return nil, err
		}
		x = &BinaryExpr{Op: op, X: x, Y: y}
	}
}

func (p *parser) parseConcat() (Node, error) {
	x, err := p.parseAdd()
	if err != nil {
		return nil, err
	}
	for p.cur().kind == tokAmp {
		p.advance()
		y, err := p.parseAdd()
		if err != nil {
			return nil, err
		}
		x = &BinaryExpr{Op: "&", X: x, Y: y}
	}
	return x, nil
}

func (p *parser) parseAdd() (Node, error) {
	x, err := p.parseMul()
	if err != nil {
		return nil, err
	}
	for {
		var op string
		switch p.cur().kind {
		case tokPlus:
			op = "+"
		case tokMinus:
			op = "-"
		default:
			return x, nil
		}
		p.advance()
		y, err := p.parseMul()
		if err != nil {
			return nil, err
		}
		x = &BinaryExpr{Op: op, X: x, Y: y}
	}
}

func (p *parser) parseMul() (Node, error) {
	x, err := p.parsePower()
	if err != nil {
		return nil, err
	}
	for {
		var op string
		switch p.cur().kind {
		case tokStar:
			op = "*"
		case tokSlash:
			op = "/"
		default:
			return x, nil
		}
		p.advance()
		y, err := p.parsePower()
		if err != nil {
			return nil, err
		}
		x = &BinaryExpr{Op: op, X: x, Y: y}
	}
}

func (p *parser) parsePower() (Node, error) {
	x, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	if p.cur().kind == tokCaret {
		p.advance()
		y, err := p.parsePower()
		if err != nil {
			return nil, err
		}
		return &BinaryExpr{Op: "^", X: x, Y: y}, nil
	}
	return x, nil
}

func (p *parser) parseUnary() (Node, error) {
	if p.cur().kind == tokMinus || p.cur().kind == tokPlus {
		op := p.cur().text
		p.advance()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{Op: op, X: x}, nil
	}
	return p.parsePercent()
}

func (p *parser) parsePercent() (Node, error) {
	x, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for p.cur().kind == tokPercent {
		p.advance()
		x = &PercentExpr{X: x}
	}
	return x, nil
}

func (p *parser) parsePrimary() (Node, error) {
	tok := p.cur()
	switch tok.kind {
	case tokNumber:
		p.advance()
		return &Number{Text: tok.text}, nil
	case tokString:
		p.advance()
		return &String{Value: tok.text}, nil
	case tokErrorLit:
		p.advance()
		return &ErrorLiteral{Text: tok.text}, nil
	case tokLParen:
		p.advance()
		x, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if p.cur().kind != tokRParen {
			return nil, fmt.Errorf("expected ')' at position %d", p.cur().pos)
		}
		p.advance()
		return &Paren{X: x}, nil
	case tokIdent:
		return p.parseIdentLike()
	default:
		return nil, fmt.Errorf("unexpected token %q at position %d", tok.text, tok.pos)
	}
}

// parseIdentLike handles everything that starts with a bare word: booleans,
// function calls, cell references, and sheet-qualified references.
func (p *parser) parseIdentLike() (Node, error) {
	tok := p.cur()
	name := tok.text
	wasQuoted := tok.quoted
	p.advance()

	if p.cur().kind == tokBang {
		p.advance()
		refTok := p.cur()
		if refTok.kind != tokIdent {
			return nil, fmt.Errorf("expected cell reference after '!' at position %d", refTok.pos)
		}
		p.advance()
		first, err := parseCellRefText(refTok.text, name, wasQuoted, p.lenient)
		if err != nil {
			return nil, err
		}
		return p.finishRefOrRange(first)
	}

	if p.cur().kind == tokLParen {
		return p.parseCall(name)
	}

	upper := strings.ToUpper(name)
	if upper == "TRUE" || upper == "FALSE" {
		if !p.lenient && name != upper {
			return nil, fmt.Errorf("boolean literal %q must be uppercase in strict mode", name)
		}
		return &Boolean{Value: upper == "TRUE"}, nil
	}

	first, err := parseCellRefText(name, "", false, p.lenient)
	if err != nil {
		return nil, err
	}
	return p.finishRefOrRange(first)
}

func (p *parser) finishRefOrRange(first CellRef) (Node, error) {
	if p.cur().kind != tokColon {
		ref := first
		return &ref, nil
	}
	p.advance()
	tok := p.cur()
	if tok.kind != tokIdent {
		return nil, fmt.Errorf("expected cell reference after ':' at position %d", tok.pos)
	}
	p.advance()
	second, err := parseCellRefText(tok.text, first.Sheet, false, p.lenient)
	if err != nil {
		return nil, err
	}
	return &Range{From: first, To: second}, nil
}

func (p *parser) parseCall(name string) (Node, error) {
	if !p.lenient && name != strings.ToUpper(name) {
		return nil, fmt.Errorf("function name %q must be uppercase in strict mode", name)
	}
	p.advance() // consume '('

	call := &Call{Name: name}
	if p.cur().kind == tokRParen {
		p.advance()
		return call, nil
	}

	for {
		if p.cur().kind == tokComma {
			if !p.lenient {
				return nil, fmt.Errorf("empty argument not allowed in strict mode at position %d", p.cur().pos)
			}
			call.Args = append(call.Args, nil)
			p.advance()
			continue
		}

		arg, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		call.Args = append(call.Args, arg)

		if p.cur().kind != tokComma {
			break
		}
		p.advance()
		if p.cur().kind == tokRParen {
			if !p.lenient {
				return nil, fmt.Errorf("trailing comma not allowed in strict mode at position %d", p.cur().pos)
			}
			break
		}
	}

	if p.cur().kind != tokRParen {
		return nil, fmt.Errorf("expected ')' or ',' at position %d", p.cur().pos)
	}
	p.advance()
	return call, nil
}

func parseCellRefText(text, sheet string, sheetQuoted, lenient bool) (CellRef, error) {
	m := cellRefPattern.FindStringSubmatch(text)
	if m == nil {
		return CellRef{}, fmt.Errorf("invalid cell reference %q", text)
	}
	col := m[2]
	if !lenient && col != strings.ToUpper(col) {
		return CellRef{}, fmt.Errorf("cell reference %q must use uppercase column letters in strict mode", text)
	}
	return CellRef{
		Sheet:       sheet,
		SheetQuoted: sheetQuoted,
		ColAbsolute: m[1] == "$",
		RowAbsolute: m[3] == "$",
		Column:      strings.ToUpper(col),
		Row:         m[4],
	}, nil
}
