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
		name  string
		decl  string
		edge  string
		form1 bool // form-1 / variable tables still refuse; the fixed form does not
	}{
		{"direct", "table", "", false},
		{"nested_type", "type", "type Middle { badge Badge }\ntable Root { middle Middle }", false},
		{"fixed_array", "type", "table Root { badges [2]Badge }", false},
		{"counted_array", "type", "table Root { badges [..2]Badge }", false},
		{"union_arm", "type", "union Choice { badge Badge }\ntable Root { choice Choice }", false},
		{"union_array_arm", "type", "union Choice { badges [2]Badge }\ntable Root { choice Choice }", true},
		{"pointer", "table", "table Root { badge *Badge }", true},
		{"map_value", "type", "table Root { badges map[uint8]Badge }", true},
		{"nested_map_value", "type", "table Root { badges map[uint8]map[uint8]Badge }", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "package vdef\nflags Caps { Jump, Crouch }\n" +
				tc.decl + " Badge {\n" + fields + "}\n" + tc.edge + packet
			u := unitFromSource(t, src)
			// NEITHER JAVA NOR ELIXIR NOR JS IS ON THIS LIST ANY MORE: java
			// carries the table half (compiler/target_java.go) the way go and
			// rust do, elixir carries it too (compiler/target_elixir.go), and
			// js lays the default into the table's own storage and the fixed
			// form's prefill (compiler/target_javascript.go). Their carrier
			// test is TestTableValueDefaultsCarriers. What is left here is the
			// ports that carry neither table form.
			for _, target := range []string{"dart"} {
				_, err := New().Generate(u, target, Options{})
				// Rust's form-3 prefill carries string/bytes/flags defaults on a
				// unit of fixed roots. A variable table, a union arm of text, a
				// pointer or a map still has nowhere to put them.
				if target == "rust" && !tc.form1 {
					if err != nil {
						t.Fatalf("rust form 3 refused table defaults on a unit of fixed roots: %v", err)
					}
					continue
				}
				if !tc.form1 {
					if err != nil && strings.Contains(err.Error(), "table-wire defaults") {
						t.Errorf("fixed-form defaults refused as form-1 table-wire: %v", err)
					}
					continue
				}
				if err == nil {
					t.Fatal("form-1 table-closure defaults accepted without table reset and elision support")
				}
				// A later refusal of maps or table unions is not enough: the
				// defaults must be found through those edges before codec generation.
				for _, want := range []string{"table-wire defaults", "Badge.label", "Badge.tag", "Badge.caps", "--lang cpp"} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("refusal does not name %q: %v", want, err)
					}
				}
				if strings.Contains(err.Error(), "Loose.label") || !strings.Contains(err.Error(), "generate with --lang c, --lang cpp, --lang cs, --lang elixir, --lang go, --lang java, --lang js and --lang rust, or drop the default") {
					t.Errorf("table refusal includes a supported packet field or names %s as a table carrier: %v", target, err)
				}
			}
		})
	}
}
