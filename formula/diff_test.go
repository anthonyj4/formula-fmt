package formula

import "testing"

func TestDiff(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
		out  string
	}{
		{
			name: "single line change",
			got:  "=SUM(A1,B2)",
			want: "=SUM(A1, B2)",
			out: "--- got\n" +
				"+++ want\n" +
				"@@ -1,1 +1,1 @@\n" +
				"-=SUM(A1,B2)\n" +
				"+=SUM(A1, B2)\n",
		},
		{
			name: "identical",
			got:  "=SUM(A1, B2)",
			want: "=SUM(A1, B2)",
			out: "--- got\n" +
				"+++ want\n" +
				"@@ -1,1 +1,1 @@\n" +
				" =SUM(A1, B2)\n",
		},
		{
			name: "multi-line string literal, only the middle line changes",
			got:  "=\"a\nb\nc\"&sum(a1,a2)",
			want: "=\"a\nb\nc\"&SUM(A1, A2)",
			out: "--- got\n" +
				"+++ want\n" +
				"@@ -1,3 +1,3 @@\n" +
				" =\"a\n" +
				" b\n" +
				"-c\"&sum(a1,a2)\n" +
				"+c\"&SUM(A1, A2)\n",
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got := Diff(c.got, c.want)
			if got != c.out {
				t.Errorf("Diff(%q, %q) = %q, want %q", c.got, c.want, got, c.out)
			}
		})
	}
}
