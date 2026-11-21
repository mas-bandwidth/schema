// schemafmt regressions from the 2026-11-21 review (FRONTEND-1, -2, -6, -7,
// -9, -12): comments the formatter must keep, shapes the parser accepts that
// the formatter must canonicalize rather than refuse, spellings it must not
// rewrite, and differences its safety net must see.
package format

import (
	"runtime"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/ast"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

func parseOrFatal(t *testing.T, src string) *ast.File {
	t.Helper()
	f, errs := parser.Parse("Probe.schema", []byte(src))
	if len(errs) > 0 {
		t.Fatalf("parse: %v", errs[0])
	}
	return f
}

func mustFormat(t *testing.T, src string) string {
	t.Helper()
	out, err := Format("Probe.schema", []byte(src))
	if err != nil {
		t.Fatalf("Format refused a valid source: %v\n%s", err, src)
	}
	return string(out)
}

// FRONTEND-1: an unqualified variant in a qualified list keeps its trailing
// comment.
func TestFormatKeepsTheCommentOfAnUnqualifiedVariant(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"package p\nenum Weapon\n{\n    Laser | beam\n    Rocket // the slow one\n}\n",
		"package p\nenum Weapon\n{\n    Laser | beam\n    Rocket /* the slow one */\n}\n",
		"package p\nenum Weapon\n{\n    Laser | beam\n    Bomb, Rocket // the slow one\n}\n",
	} {
		if out := mustFormat(t, src); !strings.Contains(out, "the slow one") {
			t.Errorf("the trailing comment was dropped:\n%s\n->\n%s", src, out)
		}
	}
}

// FRONTEND-2: two comments on one physical line are both kept, in order.
func TestFormatKeepsEveryCommentOnALine(t *testing.T) {
	t.Parallel()
	out := mustFormat(t, "package p\ntype T\n{\n    hp int16 /* units: hit points */ // clamp later\n}\n")
	if !strings.Contains(out, "/* units: hit points */ // clamp later") {
		t.Errorf("a comment on the line was dropped:\n%s", out)
	}
	out = mustFormat(t, "package p\n/* x */ /// the doc\ntype T\n{\n    a int32\n}\n")
	if !strings.Contains(out, "/* x */ /// the doc") {
		t.Errorf("the block comment before a doc line was dropped:\n%s", out)
	}
	got, err := Migrate("Probe.schema", []byte("package p\ntype T\n{\n    hp int16 /* a */ // b\n}\n"))
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if !strings.Contains(string(got), "/* a */ // b") {
		t.Errorf("Migrate dropped a comment on the line:\n%s", got)
	}
}

// FRONTEND-6: shapes the parser accepts are canonicalized, not refused as a
// formatter defect.
func TestFormatCanonicalizesAcceptedClosingBraceShapes(t *testing.T) {
	t.Parallel()
	out := mustFormat(t, "package p\ntype T { a int32 | min = 0, max = 100, replicated }\n")
	if !strings.Contains(out, "| replicated, min = 0, max = 100 }") {
		t.Errorf("the one-line qualified body was not canonicalized:\n%s", out)
	}
	out = mustFormat(t, "package p\nenum E\n{\n    A,\n    B }\ntype T\n{\n    e E\n}\n")
	if strings.Contains(out, "type T,") || strings.Contains(out, "{,") {
		t.Errorf("the variant body ran on into the next declaration:\n%s", out)
	}
	if !strings.Contains(out, "    B\n}\n") {
		t.Errorf("the closing brace did not move to its own line:\n%s", out)
	}
}

// FRONTEND-7a: `::` in a binding name stays tight.
func TestFormatKeepsAQualifiedBindingNameTight(t *testing.T) {
	t.Parallel()
	out := mustFormat(t, "package p\ntype V { x int32 }\ntype W | cpp_native = math::Vec2\n{\n    v V\n}\n")
	if !strings.Contains(out, "cpp_native = math::Vec2") {
		t.Errorf("the qualified name was respaced:\n%s", out)
	}
}

// FRONTEND-7b: the safety net's fingerprint sees an unbounded array, an
// element `?` and a cpp_ binding.
func TestFingerprintSeesListsElementOptionalsAndBindings(t *testing.T) {
	t.Parallel()
	for _, pair := range [][2]string{
		{"package p\ntable Item { a int32 }\ntable T { items []Item }\n", "package p\ntable Item { a int32 }\ntable T { items Item }\n"},
		{"package p\ntable T { names [..4]?string(8) }\n", "package p\ntable T { names [..4]string(8) }\n"},
		{"package p\ntype V { x int32 }\ntype W | cpp_native = math::Vec2\n{\n    v V\n}\n", "package p\ntype V { x int32 }\ntype W\n{\n    v V\n}\n"},
	} {
		if mustFingerprint(t, pair[0]) == mustFingerprint(t, pair[1]) {
			t.Errorf("the fingerprint cannot tell these apart:\n%s\n%s", pair[0], pair[1])
		}
	}
}

// FRONTEND-9: the expression fingerprint costs linear work in a long chain:
// doubling the chain no more than about doubles the bytes it allocates (the
// quadratic form quadrupled them). Counted in bytes, never in time.
func TestFingerprintOfALongChainIsLinear(t *testing.T) {
	chain := func(n int) string {
		return "package p\nconst C = 1" + strings.Repeat(" + 1", n) + "\n"
	}
	measure := func(n int) uint64 {
		src := chain(n)
		f := parseOrFatal(t, src)
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		_ = fingerprint(f)
		runtime.ReadMemStats(&after)
		return after.TotalAlloc - before.TotalAlloc
	}
	small, large := measure(1000), measure(4000)
	if large > 6*small {
		t.Errorf("fingerprint allocations grew %d -> %d bytes for a 4x longer chain: superlinear", small, large)
	}
}

// FRONTEND-12: migration leaves a bound followed by `?` or `*` alone.
func TestMigrateLeavesModernBoundsAlone(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"names [..4]?string(8)", "nodes [..4]*Node", "items []*Node"} {
		src := "package p\ntable Node\n{\n    a int32\n}\n\ntable T\n{\n    " + field + "\n}\n"
		got, err := Migrate("Probe.schema", []byte(src))
		if err != nil {
			t.Errorf("%s: Migrate refused a modern file: %v", field, err)
			continue
		}
		if !strings.Contains(string(got), field) {
			t.Errorf("%s: Migrate rewrote the bound:\n%s", field, got)
		}
	}
}
