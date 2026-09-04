// Unbounded arrays in the checker (docs/SPEC-TABLES.md §2.9): the `[]T`
// spelling, the placements it takes and the refusals §11 states, each by name.
//
// An unbounded array is §2.8's map with the KEY and the SORT taken out, so
// almost nothing here is new machinery: the ELEMENT resolves through the
// ordinary array path, so whatever `[..N]T` admits `[]T` admits and whatever
// `[..N]T` refuses `[]T` refuses on the bounded array's own diagnostic. What
// this file adds is the placements the construct is refused in and the
// qualifications it does not take.
package check

import (
	"github.com/mas-bandwidth/schema/v2/internal/ast"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// checkListSpelling holds every rule a `[]T` field carries that is not the
// element's own (docs/SPEC-TABLES.md §2.9, §11). It runs BEFORE the element is
// resolved, so a refused placement draws one diagnostic rather than the
// element's too, and it returns false when the field is refused.
func (c *checker) checkListSpelling(f *ast.Field, inTable bool) bool {
	if !inTable {
		c.errf(f.Pos, "field %s: an UNBOUNDED ARRAY is a table-only construct — a `type`'s wire is positional and same-or-refuse, so nothing riding it can be unbounded; hold the []T in a `table` body, or bound it as [..N]T (docs/SPEC-TABLES.md §2.9, §11)",
			f.Name)
		return false
	}
	if c.arm != "" {
		// AN ARM MAY NOT (docs/SPEC-TABLES.md §2.9, §2.6, §11), on the ground
		// a `map` is refused there: both put their elements in the holder's
		// NODE EXTENT, and an arm's storage is overlaid, so the extent would
		// depend on the union's tag
		c.errf(f.Pos, "%s: a []T is not an arm — its elements live in the holder's NODE EXTENT and an arm's storage is OVERLAID, so the extent would depend on the tag; wrap the list in a table and make THAT the arm, which is the refusal a map takes here on the same ground (docs/SPEC-TABLES.md §2.9, §2.6, §11)",
			c.arm)
		return false
	}
	if f.Type.Optional {
		c.errf(f.Pos, "field %s: there is no ?[]T — a fresh list is empty, an empty list is elided, and its count's zero IS its absence, so a presence bit beside it would be two answers to one question; drop the ? (docs/SPEC-TABLES.md §2.9, §11)",
			f.Name)
		return false
	}
	if f.Default != nil {
		c.errf(f.Pos, "field %s: a []T takes no specified default — a fresh list is empty, and empty is the only value a default could name (docs/SPEC-TABLES.md §2.9, §11)",
			f.Name)
		return false
	}
	for i := range f.Attrs {
		a := &f.Attrs[i]
		switch a.Key {
		case "was", "json":
			// a list is renamed under `was` as any field is, and takes a
			// `json` key as any field does: both are about the field, not the
			// construct (docs/SPEC-TABLES.md §2.9, §5, §16.4)
		default:
			c.errf(a.Pos, "field %s: %s does not apply to an unbounded array — THE COUNT IS THE DATA'S, and a bound would buy only a CLAMP, which drops a tail; drop the qualification, or declare [..N]%s, which is the same bytes with a bound (docs/SPEC-TABLES.md §2.9, §11)",
				f.Name, a.Key, scalarSpelling(f.Type))
			return false
		}
	}
	return true
}

// listClaims registers the three names an unbounded array CLAIMS against its
// field (docs/SPEC-TABLES.md §2.9, §11): `<Table>` followed by the PascalCase
// of the field's name, then `Add`, `Each` or `Erase`. It claims THREE where a
// map claims eight, and the difference is the key on both sides.
func listClaims(table string, f *ir.Field) []string {
	base := table + ir.GoExportName(f.Name)
	out := make([]string, 0, len(ir.ListFieldVerbs))
	for _, verb := range ir.ListFieldVerbs {
		out = append(out, base+verb)
	}
	return out
}
