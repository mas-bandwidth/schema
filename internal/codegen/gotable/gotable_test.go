package gotable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func unitFrom(t *testing.T, src string) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

func generate(t *testing.T, src string) map[string][]byte {
	t.Helper()
	out, err := Generate(unitFrom(t, src))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return out
}

// TestTagListNamesDoNotCollide holds the tag-list naming to the one thing a
// package-level name has to be: unique. A table Ship with a tagged field
// config and a tagged table ShipConfig are two rows whose owner and member
// spellings concatenate to the same text, so the separator between the two
// halves is what keeps the emitted package compiling.
func TestTagListNamesDoNotCollide(t *testing.T) {
	out := generate(t, `package probe

fixed table ShipConfig | outer
{
    scale float32 = 1.0
}

fixed table Ship
{
    config ShipConfig | inner
}
`)
	var body string
	for name, data := range out {
		if strings.HasSuffix(name, "Table.go") {
			body += string(data)
		}
	}
	if body == "" {
		t.Fatal("the unit emitted no table module")
	}
	seen := map[string]int{}
	for line := range strings.SplitSeq(body, "\n") {
		rest, ok := strings.CutPrefix(line, "var ")
		if !ok {
			continue
		}
		name, _, ok := strings.Cut(rest, " ")
		if !ok || !strings.Contains(name, "Tags") {
			continue
		}
		seen[name]++
	}
	if len(seen) != 2 {
		t.Fatalf("expected two tag lists, got %v", seen)
	}
	for name, n := range seen {
		if n != 1 {
			t.Fatalf("tag list %s is declared %d times: two rows share one package-level name", name, n)
		}
	}
}

// TestOneSidedRangeGeneratedByteIdentical pins that a one-sided range (`| min =
// A` alone) lowers to the two-sided declaration and generates byte-identical
// Go (docs/SPEC-TABLES.md §4). It lives here, not in internal/check, because
// gotable imports internal/check, and internal/check's own tests importing
// gotable would form an import cycle.
func TestOneSidedRangeGeneratedByteIdentical(t *testing.T) {
	t.Parallel()
	one := generate(t, `package t
table OneSided
{
    a int64 | min = 0
    b uint16 | max = 9
    c int8 | min = -3
}
`)
	two := generate(t, `package t
table OneSided
{
    a int64 | min = 0, max = 9223372036854775807
    b uint16 | min = 0, max = 9
    c int8 | min = -3, max = 127
}
`)
	if len(one) != len(two) {
		t.Fatalf("file counts differ: one-sided %d, two-sided %d", len(one), len(two))
	}
	for name, one := range one {
		two, ok := two[name]
		if !ok {
			t.Errorf("file %s present one-sided, absent two-sided", name)
			continue
		}
		if string(one) != string(two) {
			t.Errorf("file %s is not byte-identical between the spellings", name)
		}
	}
}
