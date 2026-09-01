package csharp

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

func generateCs(t *testing.T, name, src string) map[string][]byte {
	t.Helper()
	f, perrs := parser.Parse(name+".schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: name + ".schema", Name: name + ".schema", Base: name,
		Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func countAcross(files map[string][]byte, needle string) int {
	n := 0
	for _, src := range files {
		n += strings.Count(string(src), needle)
	}
	return n
}

func methodBody(t *testing.T, files map[string][]byte, decl string) string {
	t.Helper()
	for _, src := range files {
		text := string(src)
		start := strings.Index(text, decl)
		if start < 0 {
			continue
		}
		end := strings.Index(text[start:], "\n        }\n")
		if end < 0 {
			t.Fatalf("%s is not terminated", decl)
		}
		return text[start : start+end]
	}
	t.Fatalf("%s was not emitted", decl)
	return ""
}

// The #198 class, held for C# (#212, from the #208 review): the flat word
// codec carries ir.Item on each piece and derives safety from every
// classifier being 1:1 — one piece per item, or false. A run that falls back
// to the per-field form must therefore re-emit its ITEMS, never its pieces:
// emitting pieces doubled a fixed array's wire in Go and named a nested
// struct's fields on the wrong base. Today C#'s classifiers refuse arrays
// and nested structs outright, so these properties hold trivially — this
// test exists so lever F (grouped runs, the change that made Go's
// classification 1:N) cannot land without keeping them true.
func TestFlatFallbackReEmitsItemsNotPieces(t *testing.T) {
	// A fixed scalar array: exactly one element loop per direction. A
	// fallback over pieces would emit the loop once per element and double
	// the wire.
	files := generateCs(t, "Span", "package t\n\ntype Pair { values [2]float64 }\n")
	if got := countAcross(files, "for (int i = 0; i < 2; i++)"); got != 2 {
		t.Errorf("[2]float64 emitted %d element loops, want 2 (one write, one read) — "+
			"a fallback over pieces emits the loop once per element and doubles the wire", got)
	}

	// A single small scalar: classified into a run of one, refused by
	// flatWorthwhile, re-emitted through the per-field path — the one
	// fallback that RUNS today, so this case is the live tripwire (the
	// sabotage control doubles it).
	files = generateCs(t, "Solo", "package t\n\ntype Solo { x bits(3) }\n")
	if got := countAcross(files, "SerializeBits(ref value.X, 3)"); got != 2 {
		t.Errorf("bits(3) fallback emitted %d per-field serializes, want 2 (one write, one read) — "+
			"a fallback over pieces re-emits per piece and doubles the wire", got)
	}

	// A nested struct positioned so a grouped run would split inside it: the
	// nested type's fields are reachable only through value.Inner, so any
	// spelling that names them on the OUTER value is piece re-emission.
	var src strings.Builder
	src.WriteString("package t\n\ntype Inner\n{\n    a bits(20)\n    b bits(20)\n    c bits(24)\n}\n\ntype Outer {\n")
	const innerBits = 64
	prefix := flatMaxRunBits - innerBits + 20 // a split would land inside Inner
	for w := prefix; w > 0; {
		n := w
		if n > 64 {
			n = 64
		}
		fmt.Fprintf(&src, "    pad%d bits(%d)\n", w, n)
		w -= n
	}
	src.WriteString("    inner Inner\n}\n")
	files = generateCs(t, "Straddle", src.String())
	for _, fn := range []string{"public static bool WriteOuter(", "public static bool ReadOuter("} {
		body := methodBody(t, files, fn)
		for _, forbidden := range []string{"value.A", "value.B", "value.C"} {
			if strings.Contains(body, forbidden) {
				t.Errorf("a nested struct at the %d-bit run cap named %q on the OUTER type in %s — "+
					"Inner's fields are reachable only through value.Inner, so a fallback must "+
					"re-emit the ITEM against its own base", flatMaxRunBits, forbidden, fn)
			}
		}
	}
}
