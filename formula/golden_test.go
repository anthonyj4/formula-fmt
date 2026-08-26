package formula

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// golden cases live in testdata/golden.txt as blank-line-separated blocks of
// "key: value" lines, so new cases can be added without touching this file.
type goldenCase struct {
	mode    string
	input   string
	output  string
	wantErr string
}

func loadGoldenCases(t *testing.T, path string) []goldenCase {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden file: %v", err)
	}

	var cases []goldenCase
	for _, block := range strings.Split(string(data), "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		var c goldenCase
		for _, line := range strings.Split(block, "\n") {
			key, value, ok := strings.Cut(line, ": ")
			if !ok {
				t.Fatalf("malformed golden line %q", line)
			}
			switch key {
			case "mode":
				c.mode = value
			case "input":
				c.input = value
			case "output":
				c.output = value
			case "error":
				c.wantErr = value
			default:
				t.Fatalf("unknown golden key %q", key)
			}
		}
		if c.output == "" && c.wantErr == "" {
			t.Fatalf("golden case %q has neither output nor error", c.input)
		}
		cases = append(cases, c)
	}
	return cases
}

func TestGolden(t *testing.T) {
	cases := loadGoldenCases(t, filepath.Join("testdata", "golden.txt"))
	if len(cases) == 0 {
		t.Fatal("no golden cases loaded")
	}

	for _, c := range cases {
		c := c
		t.Run(c.input, func(t *testing.T) {
			var lenient bool
			switch c.mode {
			case "strict":
				lenient = false
			case "lenient":
				lenient = true
			default:
				t.Fatalf("unknown mode %q", c.mode)
			}

			f, err := Parse(c.input, lenient)
			if c.wantErr != "" {
				if err == nil {
					t.Fatalf("Parse(%q, lenient=%v) succeeded, want error %q", c.input, lenient, c.wantErr)
				}
				if err.Error() != c.wantErr {
					t.Fatalf("Parse(%q, lenient=%v) error = %q, want %q", c.input, lenient, err.Error(), c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q, lenient=%v) failed: %v", c.input, lenient, err)
			}
			if got := Print(f); got != c.output {
				t.Fatalf("Print(Parse(%q, lenient=%v)) = %q, want %q", c.input, lenient, got, c.output)
			}
		})
	}
}
