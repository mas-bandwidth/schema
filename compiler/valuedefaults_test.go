// A string, bytes or flags default (SPEC §4.2) has separate packet and table
// carriers. Form 1's table-wire list is C, C++, C# and Go (elision); the
// fixed form elides nothing and does not take that refusal. Other targets
// carry packet-only defaults, and the fixed form's prefill on every leg.
package compiler

import (
	"os"
	"strings"
	"testing"
)

const valueDefaultsUnit = `package vdef

flags Caps { Jump, Crouch }

type Badge
{
    label string(8) = "new"
    caps  Caps = { Jump }
}

table Ship | was = "Vessel"
{
    name string(32) = "untitled"
    tag  bytes(4) = "ab"
    caps Caps = { Jump, Crouch }
}

table Fleet
{
    flagship *Ship
    badge    Badge
}
`

func TestTableValueDefaultsCarriers(t *testing.T) {
	u := unitFromSource(t, valueDefaultsUnit)
	c := New()
	for _, target := range c.Targets() {
		out, err := c.Generate(u, target, Options{})
		if target == "cs" {
			if err != nil {
				t.Fatalf("cs refused table defaults: %v", err)
			}
			continue
		}
		if target == "c" {
			if err != nil {
				t.Fatalf("C refused table defaults: %v", err)
			}
			header := string(out["ProbeTable.h"])
			for _, want := range []string{"value->name_length = 8;", "value->tag_length = 2;", "value->caps = 3ull;"} {
				if !strings.Contains(header, want) {
					t.Errorf("C output lacks %q", want)
				}
			}
			continue
		}
		if target == "cpp" {
			if err != nil {
				t.Fatalf("cpp carries the defaults and refused: %v", err)
			}
			var all strings.Builder
			for _, b := range out {
				all.Write(b)
			}
			for _, want := range []string{
				`char label[8 + 1] = "new";`, `Caps caps = ( Caps_Jump );`,
				`char name[32 + 1] = "untitled";`, `uint8_t tag[4] = { 0x61, 0x62 };`, `Caps caps = ( Caps_Jump | Caps_Crouch );`,
				`fnv1a64( "Vessel" )`,
			} {
				if !strings.Contains(all.String(), want) {
					t.Errorf("cpp output lacks %q", want)
				}
			}
			continue
		}
		if target == "go" {
			if err != nil {
				t.Fatalf("go refused supported table defaults: %v", err)
			}
			fixed := unitFromSource(t, strings.ReplaceAll(valueDefaultsUnit, "flagship *Ship", "flagship Ship"))
			goFiles, err := c.Generate(fixed, target, Options{})
			if err != nil {
				t.Fatal(err)
			}
			code := string(goFiles["ProbeTable.go"])
			for _, want := range []string{`copy(value.Label[:], "new")`, `copy(value.Name[:], "untitled")`, `copy(value.Tag[:], "ab")`, `value.Caps = 3`} {
				if !strings.Contains(code, want) {
					t.Errorf("go default output lacks %q", want)
				}
			}
			continue
		}
		if target == "rust" {
			// THE FIXED FORM NEEDS THE DEFAULTS: §3.4's answer to a field a
			// record does not carry is a PREFILL of the declared defaults, and
			// this port lays that prefill down as a record image. So Rust
			// carries table-wire defaults, and the bytes are the proof.
			if err != nil {
				t.Fatalf("rust refused the table defaults its fixed-form prefill is built out of: %v", err)
			}
			var all strings.Builder
			for _, b := range out {
				all.Write(b)
			}
			for _, want := range []string{
				"SHIP_FIXED_DEFAULTS",
				// "untitled" behind its u32 used length, then "ab" behind its own
				"0x08, 0x00, 0x00, 0x00, 0x75, 0x6e, 0x74, 0x69, 0x74, 0x6c, 0x65, 0x64,",
				"0x02, 0x00, 0x00, 0x00, 0x61, 0x62,",
			} {
				if !strings.Contains(all.String(), want) {
					t.Errorf("the rust prefill image lacks %q", want)
				}
			}
			continue
		}
		if target == "java" {
			// THE PREFILL IS WHERE A TABLE DEFAULT LIVES IN THIS PORT
			// (internal/codegen/javatable/fixedform.go, fixedDefaultImage): the
			// fixed form's one answer to "absent field" is a constant image of
			// the declared defaults laid out as RECORD BYTES, and it is the
			// write template too. So the check is that the bytes are in it —
			// the length in front of the text, the text, the bytes default
			// behind its own length, and the flags mask — and beside it that
			// the value surface constructs the same values.
			if err != nil {
				t.Fatalf("java refused supported table defaults: %v", err)
			}
			code := string(out["ShipFixed.java"])
			for _, want := range []string{
				// "untitled" behind its length 8, then "ab" behind its length 2, then Jump|Crouch
				"8, 0, 0, 0, 117, 110, 116, 105, 116, 108, 101, 100,",
				"2, 0, 0, 0, 97, 98, 0, 0, 3, 0, 0, 0,",
				"public int nameLength = 8;",
				"public int tagLength = 2;",
				"public long caps = 3L;",
			} {
				if !strings.Contains(code, want) {
					t.Errorf("java default output lacks %q", want)
				}
			}
			continue
		}
		if err == nil {
			t.Errorf("%s emitted a unit with string, bytes and flags defaults instead of refusing it", target)
			continue
		}
		if !strings.Contains(err.Error(), "Badge.caps, Badge.label, Ship.caps, Ship.name and Ship.tag") || !strings.Contains(err.Error(), "--lang cpp") {
			t.Errorf("%s refused without naming the fields and the carrier: %v", target, err)
		}
	}
}

const fixedValueDefaultsUnit = `package vdef

flags Caps { Jump, Crouch }

table Ship
{
    name string(32) = "untitled"
    tag  bytes(4) = "ab"
    caps Caps = { Jump, Crouch }
    label string(8) = "fx"
}
`

func TestFixedTableValueDefaultsEveryLeg(t *testing.T) {
	u := unitFromSource(t, fixedValueDefaultsUnit)
	c := New()
	for _, target := range c.Targets() {
		out, err := c.Generate(u, target, Options{})
		if err != nil {
			t.Errorf("%s refused a fixed table's string, bytes and flags defaults: %v", target, err)
			continue
		}
		var all strings.Builder
		for _, b := range out {
			all.Write(b)
		}
		got := all.String()
		switch target {
		case "cpp":
			for _, want := range []string{`char name[32 + 1] = "untitled";`, `char label[8 + 1] = "fx";`, `uint8_t tag[4] = { 0x61, 0x62 };`} {
				if !strings.Contains(got, want) {
					t.Errorf("cpp fixed-form prefill lacks %q", want)
				}
			}
		case "c":
			for _, want := range []string{"value->name_length = 8;", "value->label_length = 2;", "value->tag_length = 2;", "value->caps = 3ull;"} {
				if !strings.Contains(got, want) {
					t.Errorf("C fixed-form prefill lacks %q", want)
				}
			}
		}
	}
}

func TestFX1GeneratesOnEveryLeg(t *testing.T) {
	src, err := os.ReadFile("../test/tables/FX1.schema")
	if err != nil {
		t.Fatal(err)
	}
	u := unitFromSource(t, string(src))
	c := New()
	for _, target := range c.Targets() {
		if _, err := c.Generate(u, target, Options{}); err != nil {
			t.Errorf("%s refused FX1 (label string(8) = \"fx\"): %v", target, err)
		}
	}
}
