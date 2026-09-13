package formula

import (
	"reflect"
	"testing"
)

func TestLint(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []Issue
	}{
		{
			name:  "balanced",
			input: "=SUM(A1:A10,IF(B1>0,{1,2;3,4},Table1[Column1]))",
			want:  nil,
		},
		{
			name:  "missing close paren",
			input: "=SUM(A1,B2",
			want: []Issue{
				{Pos: 3, Message: `unclosed "("`},
			},
		},
		{
			name:  "extra close paren",
			input: "=SUM(A1,B2))",
			want: []Issue{
				{Pos: 10, Message: `unexpected ")" with no matching opener`},
			},
		},
		{
			name:  "wrong closer",
			input: "=SUM(A1,B2]",
			want: []Issue{
				{Pos: 9, Message: `"]" does not match "(" opened at position 3`},
			},
		},
		{
			name:  "unclosed brace around otherwise-valid call",
			input: "={1,2,SUM(A1)",
			want: []Issue{
				{Pos: 0, Message: `unclosed "{"`},
			},
		},
		{
			name:  "table ref closed with paren",
			input: "=Table1[Column1)",
			want: []Issue{
				{Pos: 14, Message: `")" does not match "[" opened at position 6`},
			},
		},
		{
			name:  "known error literal",
			input: "=IF(A1=0,#DIV/0!,A1/B1)",
			want:  nil,
		},
		{
			name:  "malformed error literal missing bang",
			input: "=IF(A1=0,#REF,A1)",
			want: []Issue{
				{Pos: 8, Message: `"#REF" is not a recognized error literal or table item specifier`},
			},
		},
		{
			name:  "known item specifier including two-word form",
			input: "=Table1[#This Row]",
			want:  nil,
		},
		{
			name:  "unknown item specifier",
			input: "=Table1[#Foo]",
			want: []Issue{
				{Pos: 7, Message: `"#Foo" is not a recognized error literal or table item specifier`},
			},
		},
		{
			name:  "unclosed brace plus unknown specifier reported in position order",
			input: "={#Foo,SUM(A1)",
			want: []Issue{
				{Pos: 0, Message: `unclosed "{"`},
				{Pos: 1, Message: `"#Foo" is not a recognized error literal or table item specifier`},
			},
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got, err := Lint(c.input)
			if err != nil {
				t.Fatalf("Lint(%q) returned error: %v", c.input, err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("Lint(%q) = %#v, want %#v", c.input, got, c.want)
			}
		})
	}
}

func TestLintUnclosedOrderMatchesOpenOrder(t *testing.T) {
	// The braces close cleanly; the two parens don't. They should be
	// reported in the order they were opened, not the order the stack
	// unwinds in.
	got, err := Lint("=(A1+SUM({1,2}")
	if err != nil {
		t.Fatalf("Lint returned error: %v", err)
	}
	want := []Issue{
		{Pos: 0, Message: `unclosed "("`},
		{Pos: 7, Message: `unclosed "("`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Lint = %#v, want %#v", got, want)
	}
}
