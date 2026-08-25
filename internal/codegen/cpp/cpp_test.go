// The declaration-order test: schema references are order-free (SPEC §4.2),
// so the C++ data header must be a function of the schema's MEANING, not of
// the order the author wrote the constants in. Before emissionOrder gained
// const nodes, a const referenced by an earlier-declared entity
// emitted as the folded literal — values identical, the symbolic link that
// makes the header maintainable silently lost, and only under some
// declaration orders.
package cpp

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/internal/check"
	"github.com/mas-bandwidth/schema/internal/parser"
	"github.com/mas-bandwidth/schema/ir"
)

func unitFromSources(t *testing.T, sources map[string]string) *ir.Unit {
	t.Helper()
	var files []check.SourceFile
	for name, src := range sources {
		f, perrs := parser.Parse(name, []byte(src))
		if len(perrs) > 0 {
			t.Fatalf("parse: %v", perrs[0])
		}
		files = append(files, check.SourceFile{
			Path: name, Name: name, Base: strings.TrimSuffix(name, ".schema"),
			Bytes: []byte(src), AST: f,
		})
	}
	u, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

// TestConstEmissionOrderFree pins that forward references TO a constant —
// from a const initializer and from a struct's symbolically-rendered field
// expressions — render symbolically in the data header regardless of
// declaration order. Every reference here names a const declared LATER in
// the same file; before the fix each one folded to its literal.
func TestConstEmissionOrderFree(t *testing.T) {
	u := unitFromSources(t, map[string]string{
		"Forward.schema": `package t

// the earlier-declared entities: every constant they name is declared below
const MaxItems = MaxSlots + 2

type Probe {
    items   [MaxItems]uint8
    text    string(MaxText)
    reserve uint8 = MinReserve
}

const MaxSlots   = 14
const MaxText    = 32
const MinReserve = 3
`,
	})
	files, err := Generate(u, Options{})
	if err != nil {
		t.Fatal(err)
	}
	h := string(files["Forward.h"])

	for _, want := range []string{
		// const -> const: symbolic, never the folded 16
		"inline constexpr int64_t MaxItems = MaxSlots + 2;",
		// struct field expressions -> const: array bound, string size, default
		"uint8_t items[MaxItems]",
		"char text[MaxText + 1]",
		"uint8_t reserve = MinReserve;",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("data header lost the symbolic reference: want %q in Forward.h\n%s", want, h)
		}
	}
	// the dependency must have been emitted ABOVE its user, or C++ cannot compile
	if slots, items := strings.Index(h, "MaxSlots ="), strings.Index(h, "MaxItems ="); slots == -1 || items == -1 || slots > items {
		t.Errorf("MaxSlots must be emitted before MaxItems (got MaxSlots at %d, MaxItems at %d)\n%s", slots, items, h)
	}
	if folded := "MaxItems = 16;"; strings.Contains(h, folded) {
		t.Errorf("data header folded a renderable const reference: %q present\n%s", folded, h)
	}
}
