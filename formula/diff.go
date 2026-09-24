package formula

import (
	"fmt"
	"strings"
)

// Diff returns a unified diff turning got into want, for reporting --check
// failures in a form that's easy to apply by eye or with a patch tool. Most
// formulas are a single line, but a string literal can contain a literal
// newline, so this diffs line by line rather than assuming one line each.
func Diff(got, want string) string {
	a := strings.Split(got, "\n")
	b := strings.Split(want, "\n")
	ops := diffLines(a, b)

	var out strings.Builder
	out.WriteString("--- got\n")
	out.WriteString("+++ want\n")
	fmt.Fprintf(&out, "@@ -1,%d +1,%d @@\n", len(a), len(b))
	for _, op := range ops {
		switch op.kind {
		case diffEqual:
			out.WriteString(" ")
		case diffDelete:
			out.WriteString("-")
		case diffInsert:
			out.WriteString("+")
		}
		out.WriteString(op.text)
		out.WriteByte('\n')
	}
	return out.String()
}

type diffOpKind int

const (
	diffEqual diffOpKind = iota
	diffDelete
	diffInsert
)

type diffOp struct {
	kind diffOpKind
	text string
}

// diffLines produces a minimal edit script turning a into b from the table
// of longest-common-subsequence lengths. Formulas are short enough that the
// O(len(a)*len(b)) table is never worth worrying about.
func diffLines(a, b []string) []diffOp {
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			switch {
			case a[i] == b[j]:
				lcs[i][j] = lcs[i+1][j+1] + 1
			case lcs[i+1][j] >= lcs[i][j+1]:
				lcs[i][j] = lcs[i+1][j]
			default:
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	var ops []diffOp
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffOp{diffEqual, a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, diffOp{diffDelete, a[i]})
			i++
		default:
			ops = append(ops, diffOp{diffInsert, b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, diffOp{diffDelete, a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, diffOp{diffInsert, b[j]})
	}
	return ops
}
