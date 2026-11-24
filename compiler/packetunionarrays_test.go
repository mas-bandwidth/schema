// The per-PR gate on an ARRAY OF PACKET UNIONS in a table closure
// (docs/SPEC-TABLES.md §2.6, §3): every registered target generates the
// conformance corpus unit that carries it, test/tables/packetunions, whose
// union's arms are all packet types, as a counted and a fixed array in a
// variable table and in a fixed table. Generation only, default build.
package compiler

import (
	"slices"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/mootable"
)

// packetUnionTargets is every language the release carries; a target missing
// from the registry, or one that refuses the unit, fails by name.
var packetUnionTargets = []string{
	"c", "cpp", "cs", "dart", "elixir", "go", "goose", "java", "js", "lobster",
	"lua", "luau", "moo", "python", "ruby", "rust", "squirrel", "unity",
}

func TestPacketUnionArraysEveryTarget(t *testing.T) {
	t.Parallel()
	u := unitFromPaths(t, []string{"../test/tables/packetunions"})
	shapes := 0
	for _, name := range []string{"ShapeDoc", "ShapeMsg"} {
		st := u.Tables[name]
		if st == nil {
			t.Fatalf("the corpus unit declares no table %s", name)
		}
		for _, f := range st.Fields {
			if _, carried := u.TableUnions[f.Type.Name]; carried {
				t.Fatalf("%s.%s: Shape has a table-side arm; the gate needs a union whose arms are all packet types", name, f.Name)
			}
			if f.Name == "shapes" || f.Name == "pair" {
				shapes++
			}
		}
	}
	if shapes != 4 {
		t.Fatalf("the corpus unit carries %d arrays of Shape, want 4", shapes)
	}
	c := New()
	registered := c.Targets()
	for _, target := range packetUnionTargets {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			if !slices.Contains(registered, target) {
				if target != "moo" {
					t.Fatalf("--lang %s is not registered", target)
				}
				// moo registers behind the schema_leg_moo tag
				// (target_moo.go), where this test drives it through
				// Generate; the default build drives its two emitters
				if _, err := moo.Generate(u); err != nil {
					t.Fatalf("the moo packet emitter refused an array of packet unions: %v", err)
				}
				tables, err := mootable.Generate(u)
				if err != nil {
					t.Fatalf("the moo table emitter refused an array of packet unions: %v", err)
				}
				if len(tables) == 0 {
					t.Fatal("the moo table emitter emitted nothing")
				}
				return
			}
			files, err := c.Generate(u, target, Options{})
			if err != nil {
				t.Fatalf("--lang %s refused an array of packet unions: %v", target, err)
			}
			if len(files) == 0 {
				t.Fatalf("--lang %s emitted nothing", target)
			}
		})
	}
}
