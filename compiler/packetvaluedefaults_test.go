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
			if target == "cpp" || target == "cs" || target == "go" || target == "c" || target == "rust" || target == "java" || target == "js" || target == "dart" || target == "elixir" || target == "goose" || target == "lobster" || target == "lua" || target == "luau" {
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
	for _, target := range []string{"rust", "java", "js", "dart", "elixir", "lobster"} {
		if _, err := New().Generate(u, target, Options{}); err != nil {
			t.Fatalf("%s: an unrelated table must not turn packet defaults into table defaults: %v", target, err)
		}
	}
}

func TestPacketValueDefaultsDartTableClosure(t *testing.T) {
	t.Parallel()
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
		{"direct", "fixed table", "", false},
		{"nested_type", "type", "type Middle { badge Badge }\nfixed table Root { middle Middle }", false},
		{"fixed_array", "type", "fixed table Root { badges [2]Badge }", false},
		{"counted_array", "type", "fixed table Root { badges [..2]Badge }", false},
		{"union_arm", "type", "union Choice { badge Badge }\nfixed table Root { choice Choice }", false},
		{"union_array_arm", "type", "union Choice { badges [2]Badge }\ntable Root { choice Choice }", true},
		{"pointer", "table", "table Root { badge *Badge }", true},
		{"map_value", "type", "table Root { badges map[uint8]Badge }", true},
		{"nested_map_value", "type", "table Root { badges map[uint8]map[uint8]Badge }", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "package vdef\nflags Caps { Jump, Crouch }\n" +
				tc.decl + " Badge {\n" + fields + "}\n" + tc.edge + packet
			u := unitFromSource(t, src)
			// Dart carries the fixed and variable classes on form 1, including
			// every edge here. The W1/W2 Go oracle rows pin default elision.
			if _, err := New().Generate(u, "dart", Options{}); err != nil {
				t.Fatalf("dart refused table-closure defaults (form1=%v): %v", tc.form1, err)
			}
		})
	}
}
