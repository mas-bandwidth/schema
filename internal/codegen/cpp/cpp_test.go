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

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
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
	files, err := Generate(u)
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

func TestUnionPlacementIncludeStaysInWireHeader(t *testing.T) {
	u := unitFromSources(t, map[string]string{
		"Payload.schema": `package t
type Payload {
    value int32 = -1
}
`,
		"Choice.schema": `package t
union Choice {
    ping
    payload Payload
    pong
}
`,
		"Signals.schema": `package t
union Signals {
    ping
    pong
}
`,
		"Empty.schema": `package t
union Empty {}
`,
	})
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, contents := range files {
		got := strings.Contains(string(contents), "\n#include <new>\n")
		want := name == "ChoiceWire.h"
		if got != want {
			t.Errorf("%s: direct <new> include = %v, want %v; only the wire header constructs a payload", name, got, want)
		}
	}
	if !strings.Contains(string(files["ChoiceWire.h"]), "::new ( (void*) &value.payload ) Payload{};") {
		t.Error("ChoiceWire.h lost selected-payload placement construction")
	}
	if !strings.Contains(string(files["Choice.h"]), "Application code must include <new>") {
		t.Error("Choice.h must tell applications to include <new> for its placement-construction example")
	}
	if !strings.Contains(string(files["Choice.h"]), "// ::new ( (void*) &value.payload ) Payload{}; value.type = ChoiceType::Payload;") {
		t.Error("Choice.h must use its first payload arm, not its first tag, for the construction example")
	}
	if !strings.Contains(string(files["Signals.h"]), "// For example: value.type = SignalsType::Ping;") {
		t.Error("Signals.h must demonstrate tag-only application selection")
	}
	for _, name := range []string{"Signals.h", "Empty.h"} {
		if strings.Contains(string(files[name]), "\n    union\n") {
			t.Errorf("%s emitted an anonymous union without payload members", name)
		}
	}
	for _, name := range []string{"Choice.h", "Signals.h", "ChoiceWire.h", "SignalsWire.h"} {
		for _, absent := range []string{" ping;", " pong;", "value.ping", "value.pong"} {
			if strings.Contains(string(files[name]), absent) {
				t.Errorf("%s gives a payload-free arm storage or access: %q", name, absent)
			}
		}
	}
	for _, want := range []string{"Ping = 1", "Payload = 2", "Pong = 3", "Count = 3", "Max = 3"} {
		if !strings.Contains(string(files["Choice.h"]), want) {
			t.Errorf("Choice.h lost a tag when omitting payload-free storage: %q", want)
		}
	}
	for _, name := range []string{"ChoiceWire.h", "SignalsWire.h"} {
		// Both void arms must remain explicit in both directions. Omitting
		// a case would reject a legal tag even though no member is needed.
		for _, arm := range []string{"Ping", "Pong"} {
			if got := strings.Count(string(files[name]), "::"+arm+":"); got != 2 {
				t.Errorf("%s has %d cases for %s, want write and read", name, got, arm)
			}
		}
	}
}
