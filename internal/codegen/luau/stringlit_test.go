package luau

import (
	"path/filepath"
	"strconv"
	"testing"
)

// TestLuauStringLit pins LUAU-1's spelling (review 2026-11-21): Go's %q
// everywhere it is also Luau, and \u{X} where %q writes the \uXXXX or
// \UXXXXXXXX Luau rejects. The generators' %q verbs go through it. Every
// input and spelling is the committed corpus testdata/review1121/stringlit.tsv.
func TestLuauStringLit(t *testing.T) {
	t.Parallel()
	shared, escaped := 0, 0
	for _, r := range review1121Rows(t, filepath.Join(review1121Dir, "stringlit.tsv"), 3) {
		in, err := strconv.Unquote(r[1])
		if err != nil {
			t.Fatalf("%s: input %s: %v", r[0], r[1], err)
		}
		if got := StringLit(in); got != r[2] {
			t.Errorf("%s: StringLit(%q) = %s, want %s", r[0], in, got, r[2])
		}
		if strconv.Quote(in) == r[2] {
			shared++
		} else {
			escaped++
		}
	}
	if shared == 0 || escaped == 0 {
		t.Fatalf("the corpus holds %d rows spelled as Go spells them and %d it must respell: each is proven beside the other", shared, escaped)
	}
	if got := Sprintf("name = %q, n = %d, %5.1f%%, %q", "x ", 3, 1.5, []byte("y")); got != `name = "x\u{2028}", n = 3,   1.5%, "y"` {
		t.Errorf("Sprintf = %s", got)
	}
}
