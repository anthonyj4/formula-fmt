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

// Union is the reference union operator: a comma joining two or more
// references into one, e.g. (A1:A5,C1:C5). Excel only recognizes the comma
// this way inside an extra pair of parentheses - without them it's just the
// separator between function arguments - so a Union always corresponds to
// exactly one pair of parens in the source and prints its own.
type Union struct{ Refs []Node }

// Intersect is the reference intersection operator: whitespace between two
// references, selecting only the cells common to both, e.g. "A1:B5 B1:C10".
// Unlike Union it has no surrounding punctuation to key off - the parser
// notices it by checking whether the next reference-shaped token was
// separated from the previous one by whitespace in the source.
type Intersect struct{ X, Y Node }

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

// TableRef is a structured reference into an Excel table, such as
// Table1[Column1], Table1[[#Headers],[Column1]:[Column2]], or the
// current-row shorthand Table1[@Column1]. Specifiers holds special item
// specifiers ("#All", "#Data", "#Headers", "#Totals", "#This Row") in
// source order. Columns holds 0, 1, or 2 column names - 2 means a
// [C1]:[C2] column range. ThisRow marks the "@" shorthand, which always
// carries exactly one column and no specifiers.
type TableRef struct {
	Table      string
	ThisRow    bool
	Specifiers []string
	Columns    []string
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
func (*Union) node()        {}
func (*Intersect) node()    {}
func (*Call) node()         {}
func (*NamedRange) node()   {}
func (*ArrayLiteral) node() {}
func (*TableRef) node()     {}

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

	p := &parser{tokens: toks, lenient: lenient, src: []rune(work)}
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
	// src is the formula body (after the leading '=' has been stripped),
	// indexed by rune to match token.pos. Structured-reference column
	// names are read straight out of it instead of being reassembled from
	// tokens, so that names containing spaces or punctuation the general
	// lexer would otherwise split apart round-trip correctly.
	src []rune
}

func (p *parser) cur() token { return p.tokens[p.pos] }

func (p *parser) advance() {
	if p.pos < len(p.tokens)-1 {
		p.pos++
	}
}

// Operator precedence, loosest to tightest binding: comparison, &, + -,
// * /, unary +/- and ^ (Excel gives unary minus a tighter bind than ^, so
// -2^2 is (-2)^2 == 4, not -(2^2)), the intersection operator (whitespace
// between two references), then % as a postfix on whatever's left.

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
	return p.parseIntersect()
}

// parseIntersect parses the reference intersection operator: two or more
// reference-shaped operands separated by whitespace with no operator token
// between them, e.g. "A1:B5 B1:C10". Since whitespace is otherwise
// insignificant, this is also the only place two adjacent primaries would
// ever be legal - anywhere else, seeing one right after another (with
// nothing but a space between) is a syntax error - so there's no ambiguity
// to worry about with the rest of the grammar.
func (p *parser) parseIntersect() (Node, error) {
	xPos := p.cur().pos
	x, err := p.parsePercent()
	if err != nil {
		return nil, err
	}
	for p.cur().spaceBefore && startsPrimary(p.cur().kind) {
		if err := checkIntersectOperand(x, xPos); err != nil {
			return nil, err
		}
		yPos := p.cur().pos
		y, err := p.parsePercent()
		if err != nil {
			return nil, err
		}
		if err := checkIntersectOperand(y, yPos); err != nil {
			return nil, err
		}
		x = &Intersect{X: x, Y: y}
	}
	return x, nil
}

// checkIntersectOperand reports whether n is a shape the intersection
// operator accepts - the same set of reference shapes Union accepts, plus
// Intersect itself so a chain like "A1 B1 C1" round-trips as nested pairs.
func checkIntersectOperand(n Node, pos int) error {
	if paren, ok := n.(*Paren); ok {
		return checkIntersectOperand(paren.X, pos)
	}
	switch n.(type) {
	case *CellRef, *Range, *NamedRange, *TableRef, *Union, *Intersect:
		return nil
	default:
		return fmt.Errorf("invalid intersection operand at position %d: must be a cell reference, range, name, or table reference", pos)
	}
}

// startsPrimary reports whether a token kind is one parsePrimary can begin
// with - used to recognize when a space-separated token is meant as an
// intersection operand rather than something parsePrimary itself will
// reject with a clearer error (e.g. an array literal).
func startsPrimary(k tokenKind) bool {
	switch k {
	case tokNumber, tokString, tokErrorLit, tokLParen, tokIdent, tokLBrace:
		return true
	default:
		return false
	}
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
		startPos := p.cur().pos
		x, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if p.cur().kind == tokComma {
			return p.parseUnion(x, startPos)
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

// parseUnion parses the rest of a union reference expression (ref,ref,...),
// having already parsed first (starting at firstPos) and found a ',' after
// it. The closing ')' is consumed here rather than by the tokLParen case in
// parsePrimary, since a plain parenthesized expression never sees a comma.
func (p *parser) parseUnion(first Node, firstPos int) (Node, error) {
	if err := checkUnionOperand(first, firstPos); err != nil {
		return nil, err
	}
	refs := []Node{first}
	for p.cur().kind == tokComma {
		p.advance()
		pos := p.cur().pos
		x, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if err := checkUnionOperand(x, pos); err != nil {
			return nil, err
		}
		refs = append(refs, x)
	}
	if p.cur().kind != tokRParen {
		return nil, fmt.Errorf("expected ')' or ',' at position %d", p.cur().pos)
	}
	p.advance()
	return &Union{Refs: refs}, nil
}

// checkUnionOperand reports whether n is a shape the union operator accepts.
// Excel's union only makes sense over references - cells, ranges, names,
// table refs, nested unions, or a parenthesized one of those - not over
// arithmetic or other value expressions.
func checkUnionOperand(n Node, pos int) error {
	if paren, ok := n.(*Paren); ok {
		return checkUnionOperand(paren.X, pos)
	}
	switch n.(type) {
	case *CellRef, *Range, *NamedRange, *TableRef, *Union, *Intersect:
		return nil
	default:
		return fmt.Errorf("invalid union operand at position %d: must be a cell reference, range, name, or table reference", pos)
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

	if p.cur().kind == tokLBracket && !wasQuoted {
		return p.parseTableRef(name)
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

// parseTableRef parses the "[...]" portion of a structured table reference,
// having already consumed the table name. The body is one of:
//
//	@ColumnItem            current-row shorthand, e.g. Table1[@Column1]
//	#Specifier             a single item specifier on its own, e.g. Table1[#All]
//	ColumnRange            a bare column, or column range, e.g. Table1[Column1]
//	Item (, Item)*         a bracketed list mixing specifiers and columns,
//	                       e.g. Table1[[#Headers],[Column1]:[Column2]] -
//	                       once there's more than one item every item needs
//	                       its own brackets to separate it from the rest.
func (p *parser) parseTableRef(table string) (Node, error) {
	p.advance() // consume '['
	ref := &TableRef{Table: table}

	switch {
	case p.cur().kind == tokAt:
		p.advance()
		col, err := p.parseColumnItem()
		if err != nil {
			return nil, err
		}
		ref.ThisRow = true
		ref.Columns = []string{col}

	case p.cur().kind == tokLBracket:
		if err := p.parseTableRefItemList(ref); err != nil {
			return nil, err
		}

	case p.cur().kind == tokErrorLit:
		spec, err := p.parseSpecifier()
		if err != nil {
			return nil, err
		}
		ref.Specifiers = []string{spec}

	default:
		cols, err := p.parseColumnRange()
		if err != nil {
			return nil, err
		}
		ref.Columns = cols
	}

	if p.cur().kind != tokRBracket {
		return nil, fmt.Errorf("expected ']' at position %d", p.cur().pos)
	}
	p.advance()
	return ref, nil
}

// parseTableRefItemList parses a comma-separated list of bracketed items,
// e.g. "[#Headers],[Column1]:[Column2]" (the part between the outer
// "Table1[" and "]"). Each item is either a bracketed specifier
// ("[#Headers]") or a bracketed column, optionally extended into a range
// with ":[Column2]"; at most one column (or column range) is allowed, and
// it must come last, matching how Excel itself only ever places it there.
func (p *parser) parseTableRefItemList(ref *TableRef) error {
	for {
		if p.cur().kind != tokLBracket {
			return fmt.Errorf("expected '[' at position %d", p.cur().pos)
		}
		p.advance()

		if p.cur().kind == tokErrorLit {
			spec, err := p.parseSpecifier()
			if err != nil {
				return err
			}
			if p.cur().kind != tokRBracket {
				return fmt.Errorf("expected ']' at position %d", p.cur().pos)
			}
			p.advance()
			ref.Specifiers = append(ref.Specifiers, spec)
		} else {
			name, err := p.parseColumnName()
			if err != nil {
				return err
			}
			if p.cur().kind != tokRBracket {
				return fmt.Errorf("expected ']' at position %d", p.cur().pos)
			}
			p.advance()
			cols := []string{name}
			if p.cur().kind == tokColon {
				p.advance()
				if p.cur().kind != tokLBracket {
					return fmt.Errorf("expected '[' at position %d", p.cur().pos)
				}
				p.advance()
				name2, err := p.parseColumnName()
				if err != nil {
					return err
				}
				if p.cur().kind != tokRBracket {
					return fmt.Errorf("expected ']' at position %d", p.cur().pos)
				}
				p.advance()
				cols = append(cols, name2)
			}
			ref.Columns = cols
		}

		if p.cur().kind != tokComma {
			return nil
		}
		p.advance()
	}
}

// tableSpecifiers maps the lowercased form of every valid item specifier to
// its canonical spelling.
var tableSpecifiers = map[string]string{
	"#all":      "#All",
	"#data":     "#Data",
	"#headers":  "#Headers",
	"#totals":   "#Totals",
	"#this row": "#This Row",
}

// parseSpecifier parses one item specifier such as "#All" or "#This Row".
// The lexer treats "#This" and "Row" as separate tokens (an error literal
// followed by an identifier, split on the space between them), so this
// glues the two back together when they appear next to each other.
func (p *parser) parseSpecifier() (string, error) {
	tok := p.cur()
	if tok.kind != tokErrorLit {
		return "", fmt.Errorf("expected item specifier at position %d", tok.pos)
	}
	text := tok.text
	p.advance()
	if strings.EqualFold(text, "#This") && p.cur().kind == tokIdent && strings.EqualFold(p.cur().text, "Row") {
		text += " " + p.cur().text
		p.advance()
	}

	canon, ok := tableSpecifiers[strings.ToLower(text)]
	if !ok {
		return "", fmt.Errorf("unknown item specifier %q at position %d", text, tok.pos)
	}
	if !p.lenient && text != canon {
		return "", fmt.Errorf("item specifier %q must be written as %q in strict mode", text, canon)
	}
	return canon, nil
}

// parseColumnRange parses a single column or a "[C1]:[C2]" range.
func (p *parser) parseColumnRange() ([]string, error) {
	first, err := p.parseColumnItem()
	if err != nil {
		return nil, err
	}
	if p.cur().kind != tokColon {
		return []string{first}, nil
	}
	p.advance()
	second, err := p.parseColumnItem()
	if err != nil {
		return nil, err
	}
	return []string{first, second}, nil
}

// parseColumnItem parses one column name, either bracketed ("[Column 1]")
// or bare ("Column1").
func (p *parser) parseColumnItem() (string, error) {
	if p.cur().kind == tokLBracket {
		p.advance()
		name, err := p.parseColumnName()
		if err != nil {
			return "", err
		}
		if p.cur().kind != tokRBracket {
			return "", fmt.Errorf("expected ']' at position %d", p.cur().pos)
		}
		p.advance()
		return name, nil
	}
	return p.parseColumnName()
}

// parseColumnName reads raw source text up to (but not including) the next
// ']', ':', or ',', and returns it trimmed of surrounding whitespace. See
// the parser.src field comment for why this reads source text rather than
// token text.
func (p *parser) parseColumnName() (string, error) {
	start := p.cur()
	if isColumnNameTerminator(start.kind) {
		return "", fmt.Errorf("expected column name at position %d", start.pos)
	}
	startPos := start.pos
	endPos := startPos
	for !isColumnNameTerminator(p.cur().kind) {
		tok := p.cur()
		endPos = tok.pos + len([]rune(tok.text))
		p.advance()
	}
	name := strings.TrimSpace(string(p.src[startPos:endPos]))
	if name == "" {
		return "", fmt.Errorf("empty column name at position %d", startPos)
	}
	return name, nil
}

func isColumnNameTerminator(k tokenKind) bool {
	return k == tokRBracket || k == tokColon || k == tokComma || k == tokEOF
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
