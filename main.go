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
	diff := flag.Bool("diff", false, "with --check, report a non-canonical formula as a unified diff instead of a got/want message")
	lint := flag.Bool("lint", false, "report every mismatched delimiter and unrecognized error literal/item specifier instead of parsing")
	files := flag.Bool("files", false, "treat each argument as the path to a file holding one formula, rather than the formula text itself, and process all of them as a batch")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s [--lenient] [--check] [--diff] [--lint] [--files] [formula | file...]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Reads a spreadsheet formula from the argument, or from stdin if no\n")
		fmt.Fprintf(os.Stderr, "argument is given, checks it, and prints the canonical form.\n\n")
		fmt.Fprintf(os.Stderr, "With --files, every argument names a file holding one formula. Each\n")
		fmt.Fprintf(os.Stderr, "file is processed even if an earlier one is invalid or fails --check,\n")
		fmt.Fprintf(os.Stderr, "and messages are prefixed with the file's name.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *files {
		if flag.NArg() == 0 {
			fmt.Fprintln(os.Stderr, "--files requires at least one file argument")
			os.Exit(1)
		}
		ok := true
		for _, path := range flag.Args() {
			data, err := os.ReadFile(path)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
				ok = false
				continue
			}
			src := strings.TrimRight(string(data), "\n")
			if !process(path, src, *lenient, *check, *diff, *lint) {
				ok = false
			}
		}
		if !ok {
			os.Exit(1)
		}
		return
	}

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

	if !process("", src, *lenient, *check, *diff, *lint) {
		os.Exit(1)
	}
}

// process runs every mode (lint, check, check --diff, plain print) against
// one formula. name identifies the input for diagnostic messages - the path
// given to --files, or "" for the single-formula case, which keeps the
// unprefixed messages that case had before --files existed. It reports
// whether the input was clean: valid and, under --check or --lint, already
// canonical or issue-free. Batch mode uses that to decide the process's
// final exit status without letting one bad input stop the rest.
func process(name, src string, lenient, check, diff, lintMode bool) bool {
	prefix := ""
	if name != "" {
		prefix = name + ": "
	}

	if lintMode {
		issues, err := formula.Lint(src)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%sinvalid formula: %v\n", prefix, err)
			return false
		}
		for _, issue := range issues {
			fmt.Printf("%s%s\n", prefix, issue)
		}
		return len(issues) == 0
	}

	f, err := formula.Parse(src, lenient)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sinvalid formula: %v\n", prefix, err)
		return false
	}
	canonical := formula.Print(f)

	if check {
		if src != canonical {
			if diff {
				if name != "" {
					fmt.Fprintf(os.Stderr, "%s:\n", name)
				}
				fmt.Fprint(os.Stderr, formula.Diff(src, canonical))
			} else {
				fmt.Fprintf(os.Stderr, "%snot canonical: got %q, want %q\n", prefix, src, canonical)
			}
			return false
		}
		return true
	}

	if name != "" {
		fmt.Printf("%s:\n", name)
	}
	fmt.Println(canonical)
	return true
}
