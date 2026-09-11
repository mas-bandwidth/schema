// The cross-target gate on optional arrays: C++, C and C# carry the fixed
// shapes, and targets without them refuse the unit by name.
package compiler

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// optionalArraySrc holds the two optional-array spellings over the element
// kinds the construct admits — a table, a scalar, an enum — all with
// fixed-size closures, which is the whole legal set (§2.3).
const optionalArraySrc = `package probe

enum Grade
{
    Bronze,
    Silver
}

fixed table Entry
{
    value int32 = 0
}

fixed table Log
{
    entries ?[..4]Entry
    weights ?[2]float32
    grades  ?[..3]Grade
}
`

// TestOptionalArraysAreLegal: the spellings resolve to the plain array's
// framing — kind 14 with the element's own kind, nothing new on the wire —
// the holder stays FIXED-SIZE, and the storage is the array's pieces plus the
// one-byte presence companion last.
func TestOptionalArraysAreLegal(t *testing.T) {
	u := unitFromSource(t, optionalArraySrc)
	log := u.Tables["Log"]
	if log == nil {
		t.Fatalf("table Log missing")
	}
	if ir.VariableTables(u)["Log"] {
		t.Fatalf("Log derived VARIABLE — an optional array of a fixed closure must leave its holder fixed (§2.2)")
	}
	byName := map[string]*ir.Field{}
	for _, f := range log.Fields {
		byName[f.Name] = f
	}
	for name, elem := range map[string]int{
		"entries": ir.TableKindTable,
		"weights": ir.TableKindF32,
		"grades":  ir.TableKindU16,
	} {
		f := byName[name]
		if f == nil {
			t.Fatalf("field %s missing", name)
		}
		if !f.Type.Optional {
			t.Errorf("%s: not Optional in the IR", name)
		}
		if got := ir.TableFieldKind(f); got != ir.TableKindArray {
			t.Errorf("%s: field kind %d, want %d (kind 14 — the plain array's framing)", name, got, ir.TableKindArray)
		}
		if got := ir.TableElemKind(f); got != elem {
			t.Errorf("%s: element kind %d, want %d", name, got, elem)
		}
		pieces := ir.FieldPieces(u, f, 0)
		last := pieces[len(pieces)-1]
		if last.Size != 1 {
			t.Errorf("%s: last storage piece is %d bytes, want the 1-byte presence companion (§2.3)", name, last.Size)
		}
	}
	if got := ir.TableOptionalArrays(u); len(got) != 3 || got[0] != "Log.entries" || got[1] != "Log.grades" || got[2] != "Log.weights" {
		t.Errorf("TableOptionalArrays = %v, want the three fields sorted", got)
	}
}

// TestOptionalArraysCarriers: --lang cpp and --lang cs emit the table sources for a unit
// whose closure holds an optional array; targets without the form
// refuse the UNIT, naming the fields, the carrier and the flag that selects
// it — a fixed-class codec that never met the presence companion beside an
// array must not be emitted.
func TestOptionalArraysCarriers(t *testing.T) {
	u := unitFromSource(t, optionalArraySrc)
	c := New()
	for _, target := range c.Targets() {
		if target == "cpp" || target == "c" || target == "cs" || target == "go" {
			files, err := c.Generate(u, target, Options{})
			if err != nil {
				t.Fatalf("--lang %s refused the supported shape: %v", target, err)
			}
			suffix := "h"
			if target != "cpp" {
				suffix = target
			}
			if _, ok := files["ProbeTable."+suffix]; !ok {
				t.Fatalf("--lang %s emitted no table source", target)
			}
			continue
		}
		t.Run(target, func(t *testing.T) {
			_, err := c.Generate(u, target, Options{})
			if err == nil {
				t.Fatalf("--lang %s accepted a unit with an optional array in a table closure — it must refuse by name", target)
			}
			for _, want := range []string{"optional array", "Log.entries", "Log.grades", "Log.weights", "--lang cpp"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("--lang %s: the refusal does not name %q: %v", target, want, err)
				}
			}
		})
	}
}
