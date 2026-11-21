// The per-PR gate on an ARRAY OF PACKET UNIONS in a table closure
// (docs/SPEC-TABLES.md §2.6, §3): every registered target generates the
// conformance corpus unit that carries it, test/tables/packetunions, whose
// union's arms are all packet types, as a counted and a fixed array in a
// variable table and in a fixed table. Generation only, default build.
//
// The gate holds the fixture to its exact shape before it reads any output
// (packetUnionArraysUnit), and holds each target's output to carrying both
// table roots (tableRootsEmitted); TestPacketUnionArraysGateNegativeControls
// feeds each half the input it must refuse.
package compiler

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// packetUnionTargets is every language the release carries; a target missing
// from the registry, or one that refuses the unit, fails by name.
var packetUnionTargets = []string{
	"c", "cpp", "cs", "dart", "elixir", "go", "goose", "java", "js", "lobster",
	"lua", "luau", "moo", "python", "ruby", "rust", "squirrel", "unity",
}

// packetUnionRoots are the table roots the unit's output must carry.
var packetUnionRoots = []string{"ShapeDoc", "ShapeMsg"}

// packetUnionArraysUnit holds the corpus unit to what the gate is named for:
// a variable root and a fixed root, each with a counted array of three and a
// fixed array of two whose element is the union Shape, and Shape a union
// whose every arm is a packet type (never a table, never void).
func packetUnionArraysUnit(u *ir.Unit) error {
	if err := wantRoot(u, "ShapeDoc", false, "id int32", "shapes [..3]Shape", "pair [2]Shape"); err != nil {
		return err
	}
	if err := wantRoot(u, "ShapeMsg", true, "seq int32", "shapes [..3]Shape", "pair [2]Shape"); err != nil {
		return err
	}
	if _, tableSide := u.TableUnions["Shape"]; tableSide {
		return fmt.Errorf("Shape has a table-side arm; the gate needs a union whose arms are all packet types")
	}
	shape := u.Unions["Shape"]
	if shape == nil || len(shape.Variants) == 0 {
		return fmt.Errorf("the unit declares no packet union Shape with arms")
	}
	for _, root := range packetUnionRoots {
		for _, f := range u.Tables[root].Fields {
			if f.Array == ir.ArrayNone {
				continue
			}
			if ref, _ := f.Type.Ref.(*ir.Union); ref != shape {
				return fmt.Errorf("%s.%s: the array's element is not the union Shape", root, f.Name)
			}
		}
	}
	for _, v := range shape.Variants {
		if v.Ref == nil || u.Structs[v.Ref.Name] != v.Ref || u.Tables[v.Ref.Name] != nil {
			return fmt.Errorf("Shape arm %s is not a packet type", v.Name)
		}
	}
	return nil
}

func TestPacketUnionArraysEveryTarget(t *testing.T) {
	t.Parallel()
	u := unitFromPaths(t, []string{"../test/tables/packetunions"})
	if err := packetUnionArraysUnit(u); err != nil {
		t.Fatal(err)
	}
	c := New()
	registered := c.Targets()
	for _, target := range packetUnionTargets {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			if !slices.Contains(registered, target) {
				t.Fatalf("--lang %s is not registered", target)
			}
			files, err := c.Generate(u, target, Options{})
			if err != nil {
				t.Fatalf("--lang %s refused an array of packet unions: %v", target, err)
			}
			if err := tableRootsEmitted(files, packetUnionRoots...); err != nil {
				t.Fatalf("--lang %s: %v", target, err)
			}
		})
	}
}

// TestPacketUnionArraysGateNegativeControls feeds the gate what it must
// refuse. The fixture with its four union arrays turned into uint8 under the
// same field names, and the fixture with those fields renamed, fail the unit
// half. The fixture with both tables removed is what a target emits when it
// writes the packet files and drops every table product: its real output, for
// every target, fails the output half by root name.
func TestPacketUnionArraysGateNegativeControls(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../test/tables/packetunions/PacketUnions.schema")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	replace := func(t *testing.T, from, to string, n int) string {
		t.Helper()
		if got := strings.Count(src, from); got != n {
			t.Fatalf("the fixture spells %q %d times, want %d", from, got, n)
		}
		return strings.ReplaceAll(src, from, to)
	}
	t.Run("scalar_arrays", func(t *testing.T) {
		t.Parallel()
		mutated := replace(t, "[..3]Shape", "uint8", 2)
		mutated = strings.ReplaceAll(mutated, "[2]Shape", "uint8")
		if strings.Contains(mutated, "]Shape") {
			t.Fatal("the mutation left an array of Shape")
		}
		u, err := unitFromText(mutated)
		if err != nil {
			t.Fatal(err)
		}
		if err := packetUnionArraysUnit(u); err == nil {
			t.Fatal("the gate accepts the unit with no array of Shape at all")
		}
	})
	t.Run("renamed_fields", func(t *testing.T) {
		t.Parallel()
		u, err := unitFromText(replace(t, "    shapes [..3]Shape", "    forms  [..3]Shape", 2))
		if err != nil {
			t.Fatal(err)
		}
		if err := packetUnionArraysUnit(u); err == nil {
			t.Fatal("the gate accepts the unit with its counted arrays renamed")
		}
	})
	t.Run("tables_dropped", func(t *testing.T) {
		t.Parallel()
		packets, _, ok := strings.Cut(src, "// The variable class")
		if !ok {
			t.Fatal("the fixture no longer marks where its tables start")
		}
		u, err := unitFromText(packets)
		if err != nil {
			t.Fatal(err)
		}
		if len(u.Tables) != 0 || u.Unions["Shape"] == nil {
			t.Fatalf("the cut fixture declares %d tables, want the packet declarations alone", len(u.Tables))
		}
		c := New()
		for _, target := range packetUnionTargets {
			files, err := c.Generate(u, target, Options{})
			if err != nil || len(files) == 0 {
				t.Errorf("--lang %s emits no packet files for the control: %v", target, err)
				continue
			}
			if err := tableRootsEmitted(files, packetUnionRoots...); err == nil {
				t.Errorf("--lang %s: the gate accepts %d packet file(s) carrying neither table root", target, len(files))
			}
		}
	})
}
