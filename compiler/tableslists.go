// The UNBOUNDED ARRAY cross-target refusal (docs/SPEC-TABLES.md §2.9, §11):
// its own file, per the registry split — a construct's refusal adds a file
// beside builtin.go rather than growing it. Every target's Generate calls
// [refuseLists], because no code generator carries the construct yet; the
// carrier registry the map's file has lands here with the first carrier.
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// refuseLists is the named refusal every target gives a unit whose table
// closure declares a `[]T` (docs/SPEC-TABLES.md §2.9, §11, §15).
//
// An unbounded array is a VARIABLE-CLASS construct, and the variable class is
// the C++ reference's alone — the arena, the region, the node extent and the
// walks a list's elements ride in are all the reference's. The LANGUAGE takes
// the spelling and the tool's WIRE and TEXT halves carry it, so `pack` and
// `unpack` read and write one, and a generator that emitted a codec for it
// would emit one that never met an element array. So every target refuses by
// name until the reference lands the codec.
func refuseLists(u *ir.Unit, target string) error {
	fields := ir.ListFields(u)
	if len(fields) == 0 {
		return nil
	}
	return fmt.Errorf("unit declares an unbounded array in a table closure (%s) — no code generator carries `[]T` yet, %s included: the language takes the spelling and the tool's `pack` and `unpack` read and write it, and the C++ reference lands the codec first (docs/SPEC-TABLES.md §2.9, §11, §15). Declare the array at a bound, [..N]T, which is the same bytes, and remove the bound when the reference carries it",
		englishList(fields), target)
}

// refuseToolLists is the TOOL's COOK refusal (docs/SPEC-TABLES.md §2.9, §15),
// the map's own, one construct over: a unit whose table closure declares a
// `[]T` is refused by name at the tool's COOK surfaces, because
// internal/tablecook does not lay out the element arrays yet.
//
// It is here, at the surface, rather than in the engine, and it is NAMED
// rather than left to the layout. Without it the engine lays out a region
// short of the element arrays and a reader meets a slot pointing past its
// holder's extent, which is a corrupt file with nothing saying who wrote it.
func refuseToolLists(u *ir.Unit) error {
	fields := ir.ListFields(u)
	if len(fields) == 0 {
		return nil
	}
	return fmt.Errorf("unit declares an unbounded array in a table closure (%s) — the tool's WIRE and TEXT halves carry the construct, and its COOK half does not, so this command would lay out a region short of the element arrays rather than refusing; the C++ reference lands the cook (--lang cpp) (docs/SPEC-TABLES.md §2.9, §15)",
		englishList(fields))
}
