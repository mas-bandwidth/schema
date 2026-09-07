// Package listwalk is the LIST-WALK GATE's instrument (docs/SPEC-TABLES.md
// §2.9, §13.5, §16): it answers, per generated translation unit, whether the
// JSON walker's LIST half belongs in it, and it answers from the SCHEMAS
// rather than from a list of directory names kept beside them.
//
// WHY THE FACT IS DERIVED. The gate's premise is §13.5's: the list half is a
// body of non-template code that every consumer of a header would otherwise
// re-parse, so it rides only where a list rides, and a list-free unit pays
// nothing for the text form's list surface. The premise did not move. What
// moved was the corpus: the gate named `tables/maps` list-free by directory,
// #662 gave `Spans` a `map[uint8][]Item`, the unit became list-bearing, the
// copy of the fact did not, and the gate reported correct emission as a leak.
// A copied fact rots on the day the thing it copies changes; a derived one
// cannot.
//
// WHAT DERIVES IT. `ir.ListFields` walks the unit's TABLE CLOSURE and names
// every `[]T` an author wrote, including a list-typed MAP VALUE, because a
// map's generated entry is a real table of that closure (§2.8), which is what
// makes `map[K][]T` reach this answer with no clause of its own.
//
// AND WHAT DOES NOT. The answer here is never asked of the C++ emitter. The
// emitter has its own `unitHasList`, and a gate that shared it would move with
// every sabotage of it and stay green: the negative controls patch the
// emitter's gating and this package must still say what the schemas say.
package listwalk

import (
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Bearing names the unbounded arrays in a unit's table closure, as
// `Table.field`, sorted. A unit is LIST-BEARING when it names one and
// LIST-FREE when it names none.
func Bearing(u *ir.Unit) []string { return ir.ListFields(u) }

// The MARKERS the emitter writes around the list half
// (internal/codegen/cpptable/json.go). The gate reads the emitted text for
// them rather than for a function name, so the region compared is exactly the
// region the emitter claims is one half.
const (
	Begin = "// ---- json list walk: begin ----"
	End   = "// ---- json list walk: end ----"
)

// Half returns the list half of one generated .cpp, marker lines included, and
// reports whether the file carries one at all. A file whose begin marker has
// no end marker carries no half and says so, so a truncated region is a red
// rather than a silent pass.
func Half(source string) (string, bool) {
	begin := strings.Index(source, Begin)
	if begin < 0 {
		return "", false
	}
	end := strings.Index(source[begin:], End)
	if end < 0 {
		return "", false
	}
	return source[begin : begin+end+len(End)], true
}

// GeneratedUnit is one unit of the C++ table corpus: the directory
// `tables_generate` emits it into, and the schema path it is generated from.
type GeneratedUnit struct {
	Dir    string
	Source string
}

// Corpus reads the units out of the Makefile's `tables_generate` define, which
// is the list that GENERATES the tree the gate scans.
//
// IT IS THE GENERATOR'S OWN LIST, not a second one beside it. A gate whose
// corpus is hand-kept holds the units someone remembered to add; this one
// holds every unit the build emits, so a unit added to `tables_generate` is
// under the gate the same day, list-bearing or list-free.
func Corpus(makefile string) []GeneratedUnit {
	var out []GeneratedUnit
	inside := false
	for line := range strings.SplitSeq(makefile, "\n") {
		trimmed := strings.TrimSpace(line)
		if !inside {
			inside = trimmed == "define tables_generate"
			continue
		}
		if trimmed == "endef" {
			break
		}
		fields := strings.Fields(trimmed)
		// `$(1) generate --lang cpp --out $(2)/<dir> <source>` and nothing
		// else; a comment line or a recipe of another shape is not a unit.
		if len(fields) != 7 || fields[1] != "generate" || fields[4] != "--out" {
			continue
		}
		dir, ok := strings.CutPrefix(fields[5], "$(2)/")
		if !ok {
			continue
		}
		out = append(out, GeneratedUnit{Dir: dir, Source: fields[6]})
	}
	return out
}
