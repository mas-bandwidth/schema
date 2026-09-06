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

table ShipConfig | outer
{
    scale float32 = 1.0
}

table Ship
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
