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
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s [--lenient] [formula]\n\n", os.Args[0])
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

	f, err := formula.Parse(src, *lenient)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid formula: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(formula.Print(f))
}
