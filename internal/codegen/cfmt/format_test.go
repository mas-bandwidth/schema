package cfmt

import (
	"os"
	"strings"
	"testing"
)

func read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// house.want.txt is what the pinned clang-format (make/c.mk CLANG_FORMAT_PIN)
// leaves unchanged under the repo's .clang-format: it was checked against the
// formatter when it was written, and the tables-c-clean gate holds the whole
// generated corpus to the same formatter. This test holds the constructs.
func TestFormatWritesTheHouseLayout(t *testing.T) {
	in, want := read(t, "house.in.txt"), read(t, "house.want.txt")
	if why := Why(in); why != "" {
		t.Fatalf("Format refused the fixture: %s", why)
	}
	got := Format(in)
	if got != want {
		gl, wl := strings.Split(got, "\n"), strings.Split(want, "\n")
		for i := 0; i < len(gl) && i < len(wl); i++ {
			if gl[i] != wl[i] {
				t.Fatalf("line %d:\n got  %q\n want %q", i+1, gl[i], wl[i])
			}
		}
		t.Fatalf("got %d lines, want %d", len(gl), len(wl))
	}
}

func TestFormatIsIdempotent(t *testing.T) {
	want := read(t, "house.want.txt")
	if got := Format(want); got != want {
		t.Fatal("Format changed text that is already laid out")
	}
}

// The pass moves whitespace and nothing else: the token stream of the output
// is the input's.
func TestFormatMovesWhitespaceOnly(t *testing.T) {
	in := read(t, "house.in.txt")
	a, ok := lex(in)
	if !ok {
		t.Fatal("the fixture does not lex")
	}
	if why := sameTokens(a, Format(in)); why != "" {
		t.Fatal(why)
	}
}

func TestFormatLeavesAloneWhatItCannotRead(t *testing.T) {
	for _, src := range []string{
		"int f( void ) { return 0;\n",           // an unclosed brace
		"int f( void ) { return ( 0; }\n",       // mismatched brackets
		"const char * s = \"unterminated;\n",    // an unterminated literal
		"int x; /* an unterminated comment\n",   // an unterminated comment
		"int f( void ) { if x ) return 0; }\n}", // a stray closer
	} {
		if got := Format(src); got != src {
			t.Errorf("Format changed %q to %q", src, got)
		}
		if Why(src) == "" {
			t.Errorf("Why gave no reason for %q", src)
		}
	}
}

// The guard under every layout rule: an output whose tokens are not the
// input's is refused, whether two tokens ran together, one was lost, or a
// directive's body changed.
func TestSameTokensRefusesAChangedStream(t *testing.T) {
	for _, c := range []struct{ in, out string }{
		{"x = a - -b;", "x = a --b;"},
		{"x = a + +b;", "x = a ++b;"},
		{"int a; // note\nint b;", "int a; // note int b;"},
		{"int a;\n#define N 1\nint b;", "int a; #define N 1\nint b;"},
		{"#define N( x ) ( - -x )\n", "#define N( x ) ( --x )\n"},
		{"int a, b;", "int a;"},
	} {
		toks, ok := lex(c.in)
		if !ok {
			t.Fatalf("%q does not lex", c.in)
		}
		if sameTokens(toks, c.out) == "" {
			t.Errorf("%q was accepted as a layout of %q", c.out, c.in)
		}
		if why := sameTokens(toks, c.in); why != "" {
			t.Errorf("%q was refused as a layout of itself: %s", c.in, why)
		}
	}
}

// Adjacent unary operators keep the space that keeps them two tokens.
func TestFormatKeepsUnaryOperatorsApart(t *testing.T) {
	for _, src := range []string{
		"int f( int a, int b ) { return a - -b; }\n",
		"int f( int a, int b ) { return a + +b; }\n",
		"int f( int a ) { return - -a; }\n",
		"int f( int * a ) { return a[0] & &a[1] != NULL; }\n",
	} {
		if why := Why(src); why != "" {
			t.Errorf("%q: refused: %s", src, why)
		}
		toks, _ := lex(src)
		if why := sameTokens(toks, Format(src)); why != "" {
			t.Errorf("%q: %s", src, why)
		}
	}
}
