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

// CellRef is a reference to a single cell, or to one side of a range. Column
// or Row (but never both) may be empty: that's a full-column ("A" in "A:A")
// or full-row ("1" in "1:1") reference, which only appears as a Range
// endpoint - there's no such thing as a bare full-column reference on its
// own in a formula.
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

// NamedRange is a reference to a user-defined name rather than a cell or
// range, e.g. "TaxRate" or "Sheet1!TaxRate". Unlike cell references and
// function names, a name's case is whatever its author gave it - there's no
// canonical spelling to normalize toward, so it round-trips unchanged.
type NamedRange struct {
	Sheet       string
	SheetQuoted bool
	Name        string
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

// ArrayLiteral is a constant array such as {1,2,3;4,5,6}. Excel only allows
// number, string, boolean, and error constants inside one - no cell
// references, names, or nested expressions - and every row must be the same
// length. Rows[i][j] is the element at row i, column j.
type ArrayLiteral struct{ Rows [][]Node }

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
func (*NamedRange) node()   {}
func (*ArrayLiteral) node() {}

// Formula is a fully parsed cell formula.
type Formula struct {
	Body Node
}

var cellRefPattern = regexp.MustCompile(`^(\$?)([A-Za-z]{1,3})(\$?)([0-9]+)$`)
var columnRefPattern = regexp.MustCompile(`^(\$?)([A-Za-z]{1,3})$`)
var rowRefPattern = regexp.MustCompile(`^(\$?)([0-9]+)$`)
var namedRangePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)

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
		if isPlainInteger(tok.text) && p.cur().kind == tokColon {
			// a bare digit sequence right before ':' can only be the start
			// of a full-row range, e.g. the "1" in "1:1" - a plain number
			// is never followed by ':' otherwise.
			first, err := parseRowRefText(tok.text, "", false)
			if err != nil {
				return nil, err
			}
			return p.finishRefOrRange(first)
		}
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
	case tokLBrace:
		return p.parseArrayLiteral()
	default:
		return nil, fmt.Errorf("unexpected token %q at position %d", tok.text, tok.pos)
	}
}

// parseArrayLiteral parses a constant array like {1,2,3;4,5,6}. Rows are
// separated by ';', elements within a row by ','. Every row must have the
// same number of elements - Excel doesn't allow jagged array literals.
func (p *parser) parseArrayLiteral() (Node, error) {
	p.advance() // consume '{'

	row, err := p.parseArrayRow()
	if err != nil {
		return nil, err
	}
	rows := [][]Node{row}
	for p.cur().kind == tokSemi {
		p.advance()
		row, err := p.parseArrayRow()
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}

	if p.cur().kind != tokRBrace {
		return nil, fmt.Errorf("expected '}' or ';' at position %d", p.cur().pos)
	}
	p.advance()

	for _, r := range rows {
		if len(r) != len(rows[0]) {
			return nil, fmt.Errorf("array literal rows must all be the same length")
		}
	}
	return &ArrayLiteral{Rows: rows}, nil
}

func (p *parser) parseArrayRow() ([]Node, error) {
	elem, err := p.parseArrayElement()
	if err != nil {
		return nil, err
	}
	elems := []Node{elem}
	for p.cur().kind == tokComma {
		p.advance()
		elem, err := p.parseArrayElement()
		if err != nil {
			return nil, err
		}
		elems = append(elems, elem)
	}
	return elems, nil
}

// parseArrayElement parses a single constant inside an array literal.
// Unlike a normal expression, only literals are allowed - no cell
// references, names, or nested arrays - and a unary sign may only prefix a
// number.
func (p *parser) parseArrayElement() (Node, error) {
	tok := p.cur()
	switch tok.kind {
	case tokMinus, tokPlus:
		op := tok.text
		opPos := tok.pos
		p.advance()
		numTok := p.cur()
		if numTok.kind != tokNumber {
			return nil, fmt.Errorf("expected number after %q in array literal at position %d", op, opPos)
		}
		p.advance()
		return &UnaryExpr{Op: op, X: &Number{Text: numTok.text}}, nil
	case tokNumber:
		p.advance()
		return &Number{Text: tok.text}, nil
	case tokString:
		p.advance()
		return &String{Value: tok.text}, nil
	case tokErrorLit:
		p.advance()
		return &ErrorLiteral{Text: tok.text}, nil
	case tokIdent:
		upper := strings.ToUpper(tok.text)
		if upper == "TRUE" || upper == "FALSE" {
			if !p.lenient && tok.text != upper {
				return nil, fmt.Errorf("boolean literal %q must be uppercase in strict mode", tok.text)
			}
			p.advance()
			return &Boolean{Value: upper == "TRUE"}, nil
		}
		return nil, fmt.Errorf("array literal elements must be constants, got %q at position %d", tok.text, tok.pos)
	default:
		return nil, fmt.Errorf("unexpected token %q in array literal at position %d", tok.text, tok.pos)
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
		switch refTok.kind {
		case tokIdent:
			p.advance()
			return p.parseRefOrName(refTok.text, name, wasQuoted)
		case tokNumber:
			if !isPlainInteger(refTok.text) {
				return nil, fmt.Errorf("expected cell reference after '!' at position %d", refTok.pos)
			}
			p.advance()
			first, err := parseRowRefText(refTok.text, name, wasQuoted)
			if err != nil {
				return nil, err
			}
			return p.finishRefOrRange(first)
		default:
			return nil, fmt.Errorf("expected cell reference after '!' at position %d", refTok.pos)
		}
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

	return p.parseRefOrName(name, "", false)
}

// parseRefOrName resolves a bare identifier (optionally sheet-qualified via
// the sheet/sheetQuoted params) into a cell reference, a column- or row-only
// reference, or a named range if its shape doesn't match any kind of
// reference at all. Which regex the text matches decides the outcome; a
// shape match with a strict-mode casing failure is reported as that error
// rather than falling through to being treated as a name.
func (p *parser) parseRefOrName(text, sheet string, sheetQuoted bool) (Node, error) {
	if cellRefPattern.MatchString(text) {
		ref, err := parseCellRefText(text, sheet, sheetQuoted, p.lenient)
		if err != nil {
			return nil, err
		}
		return p.finishRefOrRange(ref)
	}
	if p.cur().kind == tokColon {
		if columnRefPattern.MatchString(text) {
			ref, err := parseColumnRefText(text, sheet, sheetQuoted, p.lenient)
			if err != nil {
				return nil, err
			}
			return p.finishRefOrRange(ref)
		}
		if rowRefPattern.MatchString(text) {
			ref, err := parseRowRefText(text, sheet, sheetQuoted)
			if err != nil {
				return nil, err
			}
			return p.finishRefOrRange(ref)
		}
	}
	if !namedRangePattern.MatchString(text) {
		return nil, fmt.Errorf("invalid reference or name %q", text)
	}
	return &NamedRange{Sheet: sheet, SheetQuoted: sheetQuoted, Name: text}, nil
}

func (p *parser) finishRefOrRange(first CellRef) (Node, error) {
	if p.cur().kind != tokColon {
		ref := first
		return &ref, nil
	}
	p.advance()
	tok := p.cur()

	var second CellRef
	var err error
	switch {
	case tok.kind == tokNumber && isPlainInteger(tok.text):
		if first.Column != "" {
			return nil, fmt.Errorf("expected column reference after ':' at position %d", tok.pos)
		}
		second, err = parseRowRefText(tok.text, first.Sheet, false)
	case tok.kind == tokIdent:
		second, err = parseRangeEndpointText(tok.text, first, p.lenient)
	default:
		return nil, fmt.Errorf("expected cell reference after ':' at position %d", tok.pos)
	}
	if err != nil {
		return nil, err
	}
	p.advance()
	return &Range{From: first, To: second}, nil
}

// parseRangeEndpointText parses the right-hand side of a range so that it
// matches the shape of first: a full A1 ref pairs with another full ref, a
// column-only ref (the "A" in "A:A") pairs with another column, and a
// row-only ref pairs with another row.
func parseRangeEndpointText(text string, first CellRef, lenient bool) (CellRef, error) {
	switch {
	case first.Row == "":
		return parseColumnRefText(text, first.Sheet, false, lenient)
	case first.Column == "":
		return parseRowRefText(text, first.Sheet, false)
	default:
		return parseCellRefText(text, first.Sheet, false, lenient)
	}
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

func parseColumnRefText(text, sheet string, sheetQuoted, lenient bool) (CellRef, error) {
	m := columnRefPattern.FindStringSubmatch(text)
	if m == nil {
		return CellRef{}, fmt.Errorf("invalid column reference %q", text)
	}
	col := m[2]
	if !lenient && col != strings.ToUpper(col) {
		return CellRef{}, fmt.Errorf("column reference %q must use uppercase letters in strict mode", text)
	}
	return CellRef{
		Sheet:       sheet,
		SheetQuoted: sheetQuoted,
		ColAbsolute: m[1] == "$",
		Column:      strings.ToUpper(col),
	}, nil
}

func parseRowRefText(text, sheet string, sheetQuoted bool) (CellRef, error) {
	m := rowRefPattern.FindStringSubmatch(text)
	if m == nil {
		return CellRef{}, fmt.Errorf("invalid row reference %q", text)
	}
	return CellRef{
		Sheet:       sheet,
		SheetQuoted: sheetQuoted,
		RowAbsolute: m[1] == "$",
		Row:         m[2],
	}, nil
}

// isPlainInteger reports whether a lexed number token is a bare digit
// sequence with no decimal point or exponent - the only shape that can
// double as a full-row reference.
func isPlainInteger(text string) bool {
	return !strings.ContainsAny(text, ".eE")
}
