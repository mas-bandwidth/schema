package compiler

// What TestValueDefaultsEveryShape holds before it reads any output, and the
// inputs each half of the gate must refuse (review finding F-139 on #4450: the
// gate passed a replacement fixture of one scalar packet type).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// valueDefaultsRoots are the table roots the unit's output must carry.
var valueDefaultsRoots = []string{"VarDefaults", "FixedDefaults"}

// valueDefaultsFixture holds the corpus unit to what the gate is named for: the
// variable root and the fixed root with a string, a bytes and a flags default
// on the root itself and every promised field shape of Item beside them, and
// the same three kinds of default on Item (the element of every container
// shape and the first arm of Pick) and on Leaf (the pointer target).
func valueDefaultsFixture(u *ir.Unit) error {
	defaults := func(rest ...string) []string {
		return append([]string{`s string(8) = "dflt"`, `b bytes(4) = "ab"`, "f Caps = 5"}, rest...)
	}
	if err := wantRoot(u, "VarDefaults", false, defaults(
		"item Item", "counted [..3]Item", "pair [2]Item", "list []Item", "keyed [Slot]Item",
		"by_id map{key uint8; value Item}", "opt ?Item", "leaf *Leaf", "pick Pick")...); err != nil {
		return err
	}
	if err := wantRoot(u, "FixedDefaults", true, defaults(
		"item Item", "counted [..3]Item", "pair [2]Item", "keyed [Slot]Item", "pick Pick")...); err != nil {
		return err
	}
	if err := wantRoot(u, "Leaf", false, `s string(8) = "leaf"`, `b bytes(4) = "lf"`, "f Caps = 2", "next *Leaf"); err != nil {
		return err
	}
	item := u.Structs["Item"]
	if item == nil {
		return fmt.Errorf("the unit declares no type Item")
	}
	if got, want := rootShape(item), strings.Join(defaults("n int32"), "\n"); got != want {
		return fmt.Errorf("type Item declares\n%s\nwant\n%s", got, want)
	}
	pick := u.Unions["Pick"]
	if pick == nil {
		pick = u.TableUnions["Pick"]
	}
	if pick == nil || len(pick.Variants) != 2 || pick.Variants[0].Name != "item" || pick.Variants[0].Ref != item || pick.Variants[1].Name != "count" {
		return fmt.Errorf("union Pick is not { item Item; count int32 }")
	}
	return nil
}

// TestValueDefaultsGateNegativeControls feeds the gate what it must refuse. A
// replacement fixture that is one scalar packet type with no table and no
// default, the fixture with a default removed from a root or from the element
// type, with one container shape removed, and with the fixed root made
// variable all fail the unit half. The replacement's real output, for every
// target, fails the output half by root name.
func TestValueDefaultsGateNegativeControls(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "test", "tables", "ValueDefaults.schema"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	const plain = "package tblvaluedefaults\n\ntype Plain\n{\n    v uint8\n}\n"
	mutate := func(from, to string) string { return strings.Replace(src, from, to, 1) }
	units := []struct{ name, text string }{
		{"one_scalar_packet", plain},
		{"root_default_removed", mutate("table VarDefaults\n{\n    s       string(8) = \"dflt\"", "table VarDefaults\n{\n    s       string(8)")},
		{"element_default_removed", mutate("    f Caps = { Jump, Fly }\n    n int32", "    f Caps\n    n int32")},
		{"shape_removed", mutate("    opt     ?Item\n", "")},
		{"fixed_root_made_variable", mutate("fixed table FixedDefaults", "table FixedDefaults")},
	}
	for _, tc := range units {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.text == src {
				t.Fatal("the mutation changed nothing: the fixture no longer spells what it replaces")
			}
			u, err := unitFromText(tc.text)
			if err != nil {
				t.Fatal(err)
			}
			if err := valueDefaultsFixture(u); err == nil {
				t.Fatal("the gate accepts the mutated unit")
			}
		})
	}
	t.Run("tables_dropped", func(t *testing.T) {
		t.Parallel()
		u, err := unitFromText(plain)
		if err != nil {
			t.Fatal(err)
		}
		c := New()
		for _, target := range c.Targets() {
			files, err := c.Generate(u, target, Options{})
			if err != nil || len(files) == 0 {
				t.Errorf("%s emits no packet files for the control: %v", target, err)
				continue
			}
			if err := tableRootsEmitted(files, valueDefaultsRoots...); err == nil {
				t.Errorf("%s: the gate accepts %d packet file(s) carrying neither table root", target, len(files))
			}
		}
	})
}
