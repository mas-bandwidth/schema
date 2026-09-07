// A string, bytes or flags default (SPEC §4.2) has separate packet and table
// carriers. C++ and Go carry both; the remaining packet carriers refuse table defaults.
package compiler

import (
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

func TestTableValueDefaultCarriers(t *testing.T) {
	u := unitFromSource(t, valueDefaultsUnit)
	c := New()
	for _, target := range c.Targets() {
		out, err := c.Generate(u, target, Options{})
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
		if err == nil {
			t.Errorf("%s emitted a unit with string, bytes and flags defaults instead of refusing it", target)
			continue
		}
		if !strings.Contains(err.Error(), "Badge.caps, Badge.label, Ship.caps, Ship.name and Ship.tag") || !strings.Contains(err.Error(), "--lang cpp") {
			t.Errorf("%s refused without naming the fields and the carrier: %v", target, err)
		}
	}
}
