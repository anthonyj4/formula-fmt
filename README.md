# formula-fmt

A parser and pretty printer for spreadsheet formulas, the kind that live in
a single cell and start with `=`.

Every spreadsheet app accepts formulas that are technically fine but
inconsistent: `=sum(a1:a10,b2)`, `=SUM(A1:A10, B2)`, `=Sum( A1:A10,B2 )` all
mean the same thing. If you're generating formulas from code, reviewing a
diff of a spreadsheet export, or building a linter for a template library,
you want one canonical spelling and a clear error when something is
actually wrong rather than just differently styled.

`formula-fmt` parses a formula into an AST, validates it, and prints it back
out in a single canonical form: uppercase function names, uppercase column
letters, a space around binary operators, `", "` between arguments.

## Strict by default

By default the parser is strict about things that are legal in most
spreadsheet apps but are really just sloppiness:

- the formula must start with `=`
- function names must already be uppercase (`SUM`, not `sum`)
- cell reference columns must already be uppercase (`A1`, not `a1`)
- no empty arguments (`SUM(A1,,B2)`)
- no trailing commas (`SUM(A1,B2,)`)

Pass `--lenient` to accept all of that anyway. The parser normalizes it away
instead of rejecting it - lowercase refs get uppercased, empty arguments are
kept as gaps, trailing commas are dropped. Strict mode is what you want when
you're validating formulas someone else wrote by hand; lenient mode is what
you want when you're ingesting formulas from an old export or a tool that
was never careful about casing.

## Usage

```
$ go run . '=SUM(A1:A10,B2)*2'
=SUM(A1:A10, B2) * 2

$ go run . '=sum(a1:a10,b2)*2'
invalid formula: function name "sum" must be uppercase in strict mode

$ go run . --lenient 'sum(a1:a10,b2)*2'
=SUM(A1:A10, B2) * 2

$ go run . '=SUM(A1,B2,)'
invalid formula: trailing comma not allowed in strict mode at position 10

$ go run . --lenient '=SUM(A1,B2,)'
=SUM(A1, B2)

$ go run . "='Q1 Report'!A1+Sheet2!\$B\$2"
='Q1 Report'!A1 + Sheet2!$B$2

$ go run . '=IF(A1=0,#DIV/0!,A1/B1)'
=IF(A1 = 0, #DIV/0!, A1 / B1)

$ go run . '=SUM(A:A)+SUM(TaxRate,Sheet2!1:3)'
=SUM(A:A) + SUM(TaxRate, Sheet2!1:3)
```

With no argument it reads the formula from stdin, so it also works as a
filter:

```
$ echo '=sum(a1,a2)' | go run . --lenient
=SUM(A1, A2)
```

## What's parsed

Numbers, string literals (with `""` as an escaped quote, matching Excel),
`TRUE`/`FALSE`, error literals (`#REF!`, `#DIV/0!`, `#N/A`, `#NAME?`,
`#NULL!`, `#NUM!`, `#VALUE!`), cell references and ranges with `$` anchors,
full-column and full-row references (`A:A`, `$1:$1`), named ranges,
sheet-qualified references including quoted sheet names, function calls,
parentheses, and the arithmetic, comparison, concatenation (`&`), and
percent (`%`) operators, with Excel's actual operator precedence (unary
minus binds tighter than `^`, so `-2^2` is `4`).

A bare word that isn't a cell reference, a full-column/full-row reference,
or a function call is parsed as a named range and printed back exactly as
written - unlike function names and column letters, a name has no canonical
casing to normalize toward.

## What isn't, yet

Array literals (`{1,2,3}`), structured table references
(`Table1[Column]`), and the intersection/union operators. Formulas using
those fail to parse for now - see the roadmap in the issue tracker.

## Library use

The CLI is a thin wrapper around the `formula` package:

```go
f, err := formula.Parse(src, lenient)
if err != nil {
    // src is not a valid formula
}
canonical := formula.Print(f)
```

`formula.Parse` returns an AST (`*formula.Formula`) you can walk yourself if
printing isn't what you need.
