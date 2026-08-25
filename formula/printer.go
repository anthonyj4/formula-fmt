package formula

import (
	"strings"
	"unicode"
)

// Print renders f in canonical form: leading '=', uppercase function names
// and column letters, a single space around binary operators, and ", "
// between call arguments. It never returns an error - a *Formula produced
// by Parse is always printable.
func Print(f *Formula) string {
	var b strings.Builder
	b.WriteByte('=')
	writeNode(&b, f.Body)
	return b.String()
}

func writeNode(b *strings.Builder, n Node) {
	switch v := n.(type) {
	case *Number:
		b.WriteString(v.Text)
	case *String:
		b.WriteByte('"')
		b.WriteString(strings.ReplaceAll(v.Value, `"`, `""`))
		b.WriteByte('"')
	case *Boolean:
		if v.Value {
			b.WriteString("TRUE")
		} else {
			b.WriteString("FALSE")
		}
	case *ErrorLiteral:
		b.WriteString(v.Text)
	case *CellRef:
		writeCellRef(b, v)
	case *Range:
		writeCellRef(b, &v.From)
		b.WriteByte(':')
		to := v.To
		to.Sheet = "" // a range's sheet is only printed once, on the left side
		writeCellRef(b, &to)
	case *UnaryExpr:
		b.WriteString(v.Op)
		writeNode(b, v.X)
	case *PercentExpr:
		writeNode(b, v.X)
		b.WriteByte('%')
	case *BinaryExpr:
		writeNode(b, v.X)
		b.WriteByte(' ')
		b.WriteString(v.Op)
		b.WriteByte(' ')
		writeNode(b, v.Y)
	case *Paren:
		b.WriteByte('(')
		writeNode(b, v.X)
		b.WriteByte(')')
	case *NamedRange:
		writeSheetPrefix(b, v.Sheet, v.SheetQuoted)
		b.WriteString(v.Name)
	case *Call:
		b.WriteString(strings.ToUpper(v.Name))
		b.WriteByte('(')
		for i, arg := range v.Args {
			if i > 0 {
				b.WriteString(", ")
			}
			if arg == nil {
				continue // omitted argument, only reachable via --lenient
			}
			writeNode(b, arg)
		}
		b.WriteByte(')')
	}
}

func writeCellRef(b *strings.Builder, r *CellRef) {
	writeSheetPrefix(b, r.Sheet, r.SheetQuoted)
	if r.ColAbsolute {
		b.WriteByte('$')
	}
	b.WriteString(r.Column)
	if r.RowAbsolute {
		b.WriteByte('$')
	}
	b.WriteString(r.Row)
}

// writeSheetPrefix writes the "Sheet1!" or "'Q1 Report'!" prefix shared by
// cell references, ranges, and named ranges. It writes nothing for sheet == "".
func writeSheetPrefix(b *strings.Builder, sheet string, quoted bool) {
	if sheet == "" {
		return
	}
	if quoted || needsSheetQuoting(sheet) {
		b.WriteByte('\'')
		b.WriteString(strings.ReplaceAll(sheet, "'", "''"))
		b.WriteByte('\'')
	} else {
		b.WriteString(sheet)
	}
	b.WriteByte('!')
}

// needsSheetQuoting reports whether a sheet name must be wrapped in single
// quotes to be re-parsed unambiguously (anything but letters, digits, and
// underscores, or a name starting with a digit).
func needsSheetQuoting(name string) bool {
	if name == "" {
		return true
	}
	for i, r := range name {
		if r == '_' || unicode.IsLetter(r) {
			continue
		}
		if unicode.IsDigit(r) && i > 0 {
			continue
		}
		return true
	}
	return false
}
