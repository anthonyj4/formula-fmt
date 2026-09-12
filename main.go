// Command formulafmt validates a spreadsheet formula and prints its
// canonical form.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/anthonyj4/formula-fmt/formula"
)

func main() {
	lenient := flag.Bool("lenient", false, "accept common real-world sloppiness (missing '=', lowercase refs, trailing commas) instead of rejecting it")
	check := flag.Bool("check", false, "don't print the canonical form; exit 1 if the formula is invalid or not already canonical")
	lint := flag.Bool("lint", false, "report every mismatched or unbalanced paren/brace/bracket instead of parsing")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s [--lenient] [--check] [--lint] [formula]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Reads a spreadsheet formula from the argument, or from stdin if no\n")
		fmt.Fprintf(os.Stderr, "argument is given, checks it, and prints the canonical form.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	var src string
	if flag.NArg() > 0 {
		src = strings.Join(flag.Args(), " ")
	} else {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading stdin: %v\n", err)
			os.Exit(1)
		}
		src = strings.TrimRight(string(data), "\n")
	}

	if *lint {
		issues, err := formula.Lint(src)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid formula: %v\n", err)
			os.Exit(1)
		}
		for _, issue := range issues {
			fmt.Println(issue)
		}
		if len(issues) > 0 {
			os.Exit(1)
		}
		return
	}

	f, err := formula.Parse(src, *lenient)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid formula: %v\n", err)
		os.Exit(1)
	}
	canonical := formula.Print(f)

	if *check {
		if src != canonical {
			fmt.Fprintf(os.Stderr, "not canonical: got %q, want %q\n", src, canonical)
			os.Exit(1)
		}
		return
	}
	fmt.Println(canonical)
}
