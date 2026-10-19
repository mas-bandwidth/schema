// The TEXT LIST and ARRAY ROOT cross-target refusal (docs/SPEC-TABLES.md
// §2.13, §16.10): its own file, per the registry split. Go carries both
// constructs first; every other target refuses a unit that uses either, here,
// once, by name — the construct, the field or table, and the target — so no
// other emitter is edited and none can write a second wire or text for them.
package compiler

import (
	"fmt"
	"sort"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// refuseTextForms is the named refusal [Compiler.Generate] gives every target
// but go for a unit holding a text list or an array-root table.
func refuseTextForms(u *ir.Unit, target string) error {
	// UNBOUNDED TEXT first (docs/SPEC-TABLES.md §2.13.1): an unbounded element
	// is also a text list, and the refusal names the construct it is
	if fields := ir.TableUnboundedTexts(u); !carries(target, carryUnboundedText) && len(fields) > 0 {
		field := fields[0]
		what, spelling := "unbounded text-list element", "a []string, []?string, [..N]string or [..N]?string list"
		if ir.UnitMapKeyUnbounded(u, field) {
			what, spelling = "unbounded map key", "a map[string]V key"
		}
		return fmt.Errorf("target %q refuses the construct %s on field %s — %s is Go's first; no other target carries it until its turn (docs/SPEC-TABLES.md §2.13.1)", target, what, field, spelling)
	}
	if fields := ir.TableTextTotals(u); !carries(target, carryTextListTotal) && len(fields) > 0 {
		return fmt.Errorf("target %q refuses the construct text list total on field %s — | total = N on a text list is Go's first; no other target carries it until its turn (docs/SPEC-TABLES.md §2.13)", target, fields[0])
	}
	if fields := ir.TableTextLists(u); !carries(target, carryTextList) && len(fields) > 0 {
		if !carries(target, carryBoundedTextList) {
			return fmt.Errorf("target %q refuses the construct text list on field %s — a [..N]string(M) or []string(M) list is Go's first; no other target carries it until its turn (docs/SPEC-TABLES.md §2.13)", target, fields[0])
		}
		// A CARRIER OF THE COUNT-BOUNDED SPELLING ALONE (§2.13) still refuses
		// the unbounded count, `[]string(M)` / `[]?string(M)`: the bound is
		// what makes the list's count a wire number with a ceiling, and the
		// unbounded count is carryTextList's, which this target has not
		// registered.
		if unbounded := unboundedCountTextLists(u); len(unbounded) > 0 {
			return fmt.Errorf("target %q refuses the construct text list on field %s — the unbounded COUNT, []string(M) or []?string(M), is Go's first; no other target carries it until its turn (docs/SPEC-TABLES.md §2.13)", target, unbounded[0])
		}
	}
	if tables := ir.TableArrayRoots(u); !carries(target, carryArrayRoot) && len(tables) > 0 {
		return fmt.Errorf("target %q refuses the construct json = \"array\" on table %s — Go carries the array root first; no other target honours it until its turn (docs/SPEC-TABLES.md §16.10)", target, tables[0])
	}
	return nil
}

// unboundedCountTextLists names the text list fields whose COUNT is
// unbounded, `[]string(M)` / `[]?string(M)` (§2.13): the same fields
// ir.TableTextLists names, narrowed to Array == ir.ArrayList. A target that
// carries only the count-bounded spelling refuses these by name above.
func unboundedCountTextLists(u *ir.Unit) []string {
	var out []string
	for name, st := range u.Tables {
		if st == nil || st.MapEntryOf != "" || st.TextElem {
			continue
		}
		for _, f := range st.Fields {
			if f.IsTextList() && f.Array == ir.ArrayList {
				out = append(out, name+"."+f.Name)
			}
		}
	}
	sort.Strings(out)
	return out
}
