package java

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// THE CANONICAL PASS LAYS DOWN THE FORMATTER'S BREAKS FOR THE SHAPES THE TABLE
// TEMPLATES WROTE (#547): a throws clause too wide for its signature; a
// conditional and an argument list a template broke by hand; a chain whose
// receiver is `new T(...)`, a qualified type with its constant, or a short
// name with its first call; a call leading a broken comparison; a generic
// declaration too wide for `Type name =`; and a method body written whole
// levels too deep. testdata/canonical_shapes.in is what the templates wrote
// and testdata/canonical_shapes.want is google-java-format 1.37.0 --aosp's own
// output for it, captured on a bench; the test reads files and runs no Java.
func TestCanonicalLaysOutTheTemplatesShapes(t *testing.T) {
	t.Parallel()
	in, err := os.ReadFile(filepath.Join("testdata", "canonical_shapes.in"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "canonical_shapes.want"))
	if err != nil {
		t.Fatal(err)
	}
	got := Canonical(in)
	if string(got) != string(want) {
		gl, wl := strings.Split(string(got), "\n"), strings.Split(string(want), "\n")
		for i := 0; i < len(gl) || i < len(wl); i++ {
			var g, w string
			if i < len(gl) {
				g = gl[i]
			}
			if i < len(wl) {
				w = wl[i]
			}
			if g != w {
				t.Fatalf("line %d:\n got %q\nwant %q", i+1, g, w)
			}
		}
	}
	if again := Canonical(got); string(again) != string(got) {
		t.Fatalf("the canonical pass is not idempotent on the shapes")
	}
	if CodeTokens(string(got)) != CodeTokens(string(in)) {
		t.Fatalf("the canonical pass changed a token on the shapes")
	}
}

// A BODY IS MOVED ONLY WHOLE (#547): deepBodies moves a method body every line
// of which is the same whole number of indents too deep, and never a single
// mis-indented line among lines at the right depth -- that line is the drift
// the format gate (and its control) exists to see.
func TestDeepBodiesNeverAbsorbsOneMisIndentedLine(t *testing.T) {
	t.Parallel()
	whole := []string{
		"    static void f(In in) {",
		"            if (in.bad) {",
		"                return;",
		"            }",
		"    }",
	}
	moved := deepBodies(whole)
	if moved[1] != "        if (in.bad) {" || moved[2] != "            return;" || moved[3] != "        }" {
		t.Fatalf("a body two indents deep was not moved to its level: %q", moved)
	}
	for _, one := range [][]string{
		{"    static void f(In in) {", "        int a = 0;", "            int b = 0;", "    }"},
		{"    static void f(In in) {", "            int b = 0;", "        int a = 0;", "    }"},
		{"    static void f(In in) {", "         int b = 0;", "    }"},
		{"    static int f(In in) {", "             return 1;", "    }"},
	} {
		if got := deepBodies(one); strings.Join(got, "\n") != strings.Join(one, "\n") {
			t.Fatalf("deepBodies moved a mis-indented line it must leave for the gate:\n%s", strings.Join(got, "\n"))
		}
	}
}

// A NESTED GENERIC IS NEVER SPLIT INSIDE ITS BRACKETS (#547, cold-read
// finding): the declaration rule splits a too-wide generic type at its
// top-level arguments only, and leaves a type whose arguments are themselves
// generic as written rather than cutting one in half.
func TestCanonicalLeavesNestedGenericsWhole(t *testing.T) {
	t.Parallel()
	in := "class T {\n    void f() {\n        java.util.Map<java.lang.String, java.util.List<java.lang.String, java.lang.Integer>, VeryLongTypeName> someVariableName = make();\n    }\n}\n"
	got := string(Canonical([]byte(in)))
	for _, l := range strings.Split(got, "\n") {
		if strings.HasSuffix(strings.TrimSpace(l), "List<java.lang.String,") {
			t.Fatalf("a nested generic was split inside its brackets:\n%s", got)
		}
	}
	if CodeTokens(got) != CodeTokens(in) {
		t.Fatalf("the canonical pass changed a token")
	}
}
