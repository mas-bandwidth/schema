package ir_test

// THE TABLE CLOSURE'S KIND CENSUS (docs/SPEC-TABLES.md §4) and the property a
// backend gates a DEFINITION on it for: it names the kind a declaration spells
// wherever the declaration sits — a field, an array's element, a map's half, a
// union arm, a pointee's field — so a widening helper's call site cannot be in
// a unit the census withheld the helper from.

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// every position a kind can be declared in, each spelling a kind nothing else
// in the fixture spells, so a census that missed one position is red.
const kindCensusSource = `package demo

table Leaf { deep float64 }

union Reach
{
    wide  int64
    plain int32
}

table Holder
{
    leaf    *Leaf
    tags    map[uint16]uint64
    scores  [3]float32
    reach   Reach
    ordinal int8
}
`

func TestTableDeclaredKindsNamesEveryPosition(t *testing.T) {
	kinds := ir.TableDeclaredKinds(unitFrom(t, kindCensusSource))
	for _, tc := range []struct {
		kind  int
		where string
	}{
		{ir.TableKindF64, "a pointee's field"},
		{ir.TableKindU16, "a map's key"},
		{ir.TableKindU64, "a map's value"},
		{ir.TableKindF32, "an array's element"},
		{ir.TableKindI64, "a union arm"},
		{ir.TableKindI8, "a plain field"},
	} {
		if !kinds[tc.kind] {
			t.Errorf("kind %d is declared at %s and the census does not name it", tc.kind, tc.where)
		}
	}
}

// the nearest neighbour: the same shapes with the float rung's top respelled at
// its bottom carry no kind 11 at all, which is the answer the C table emitter's
// table_wire_widen_f32 gate reads.
const kindCensusNarrowSource = `package demo

table Leaf { deep float32 }

table Holder
{
    leaf   *Leaf
    scores [3]float32
}
`

func TestTableDeclaredKindsIsEmptyOfAKindNobodyDeclares(t *testing.T) {
	kinds := ir.TableDeclaredKinds(unitFrom(t, kindCensusNarrowSource))
	if kinds[ir.TableKindF64] {
		t.Error("a unit that declares no float64 answered yes to kind 11")
	}
	if !kinds[ir.TableKindF32] {
		t.Error("the fixture declares float32 and the census does not name kind 10")
	}
}
