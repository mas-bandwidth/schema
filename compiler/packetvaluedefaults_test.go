package compiler

import (
	"strings"
	"testing"
)

const packetValueDefaultsUnit = `package vdef

flags Caps { Jump, Crouch }

type Badge
{
    label string(8) = "é"
    tag bytes(4) = "\n"
    caps Caps = { Jump, Crouch }
    empty_label string(4) = ""
    empty_tag bytes(4) = ""
    empty_caps Caps = {}
}

type Bundle
{
    badge Badge
    badges [2]Badge
    counted [1..3]Badge
}

union Choice
{
    badge Badge
}

type Holder
{
    choice Choice
}
`

func TestPacketValueDefaultsCarriers(t *testing.T) {
	u := unitFromSource(t, packetValueDefaultsUnit)
	c := New()
	for _, target := range c.Targets() {
		t.Run(target, func(t *testing.T) {
			_, err := c.Generate(u, target, Options{})
			if target == "cpp" || target == "cs" || target == "go" || target == "c" || target == "rust" || target == "java" || target == "js" || target == "dart" || target == "elixir" {
				if err != nil {
					t.Fatalf("packet defaults refused: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("unported packet defaults accepted")
			}
			for _, want := range []string{"packet-wire defaults", "Badge.label", "Badge.tag", "Badge.caps", "--lang cpp", "--lang go"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal does not name %q: %v", want, err)
				}
			}
		})
	}
}

func TestPacketValueDefaultsBesideUnrelatedTable(t *testing.T) {
	u := unitFromSource(t, packetValueDefaultsUnit+"\ntable Counter { number int32 }\n")
	for _, target := range []string{"rust", "java", "js", "dart", "elixir"} {
		if _, err := New().Generate(u, target, Options{}); err != nil {
			t.Fatalf("%s: an unrelated table must not turn packet defaults into table defaults: %v", target, err)
		}
	}
}

func TestPacketValueDefaultsRefuseTableClosure(t *testing.T) {
	const fields = `
    label string(8) = "new"
    tag bytes(4) = "ab"
    caps Caps = { Jump }
`
	const packet = `
type Loose
{
    label string(8) = "loose"
}
`
	for _, tc := range []struct {
		name string
		decl string
		edge string
	}{
		{"direct", "table", ""},
		{"nested_type", "type", "type Middle { badge Badge }\ntable Root { middle Middle }"},
		{"fixed_array", "type", "table Root { badges [2]Badge }"},
		{"counted_array", "type", "table Root { badges [..2]Badge }"},
		{"union_arm", "type", "union Choice { badge Badge }\ntable Root { choice Choice }"},
		{"union_array_arm", "type", "union Choice { badges [2]Badge }\ntable Root { choice Choice }"},
		{"pointer", "table", "table Root { badge *Badge }"},
		{"map_value", "type", "table Root { badges map[uint8]Badge }"},
		{"nested_map_value", "type", "table Root { badges map[uint8]map[uint8]Badge }"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "package vdef\nflags Caps { Jump, Crouch }\n" +
				tc.decl + " Badge {\n" + fields + "}\n" + tc.edge + packet
			u := unitFromSource(t, src)
			// js is NOT on this list any more: internal/codegen/jstable lays a
			// string, bytes or flags default into the table's own storage and
			// into the fixed form's prefill, so the port carries the form and
			// the refusal is not its. It moved to the carrier list in
			// compiler/target_javascript.go, and the sentence below moved with
			// it — the two are one list.
			for _, target := range []string{"rust", "java", "dart", "elixir"} {
				_, err := New().Generate(u, target, Options{})
				if err == nil {
					t.Fatal("table-closure defaults accepted without table reset and elision support")
				}
				// A later refusal of maps or table unions is not enough: the
				// defaults must be found through those edges before codec generation.
				for _, want := range []string{"table-wire defaults", "Badge.label", "Badge.tag", "Badge.caps", "--lang cpp"} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("refusal does not name %q: %v", want, err)
					}
				}
				if strings.Contains(err.Error(), "Loose.label") || !strings.Contains(err.Error(), "generate with --lang c, --lang cpp, --lang cs, --lang go and --lang js, or drop the default") {
					t.Errorf("table refusal includes a supported packet field or names %s as a table carrier: %v", target, err)
				}
			}
		})
	}
}
