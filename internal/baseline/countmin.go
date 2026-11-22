package baseline

import (
	"fmt"
	"strconv"
)

// countMinFinding judges a counted array's MINIMUM COUNT moving, in either
// direction (RuleCountMin), between the baseline's line and the live one. A
// line that carries no `mincount=` holds a minimum of zero: a `[..N]T`, and
// every other array shape.
//
// The minimum is two reader facts, and TestCountMinimumPredictionsAgreeWithTheRead
// pins both against a real read of a saved file:
//
//   - a present array holding fewer elements than the reader's minimum
//     refuses the whole load `count_over_bound` (docs/SPEC-TABLES.md §4). Only
//     a RAISED minimum meets stored counts it does not admit.
//   - the minimum is the array's BORN count (SPEC §4.6), and a writer elides
//     an array still at its born value, so the reader's own minimum is what
//     that absence MEANS. A file that left the array alone reads back with
//     the reader's minimum of default elements, raised or lowered, and no
//     counter fires — the class a changed specified default is in.
//
// So any move refuses, and the sentence says which of the two applies. The
// born count on the side that carries no minimum is the shape's own: a fixed
// array is born full, at its bound, and every other shape is born empty.
func countMinFinding(where string, bf, lf Field) []Finding {
	before, ok1 := countFact(bf, "mincount")
	after, ok2 := countFact(lf, "mincount")
	if !ok1 || !ok2 {
		// a count this check cannot read as a number is a line it cannot
		// judge, and it says so rather than passing
		was, _ := bf.Get("mincount")
		now, _ := lf.Get("mincount")
		return []Finding{{Refuse, where, fmt.Sprintf("%s %q -> %q is not a count", tokenNoun("mincount"), was, now)}}
	}
	if before == after {
		return nil
	}
	head := fmt.Sprintf("%s %d -> %d", tokenNoun("mincount"), before, after)
	var parts []string
	// the smallest count a file can hold present is the old declaration's
	// own floor, which is its born count: a fixed array is always written
	// full, so it meets a new minimum only where its bound is below it
	wasBorn, nowBorn := bornCount(bf, before), bornCount(lf, after)
	if after > wasBorn {
		parts = append(parts, fmt.Sprintf(
			"a file holding the array with fewer than %d elements no longer reads: the load refuses count_over_bound", after))
	}
	if wasBorn != nowBorn {
		parts = append(parts, fmt.Sprintf(
			"a file whose writer elided the array at its born value, %s, reads back as %s, and nothing reports it",
			defaultElements(wasBorn), defaultElements(nowBorn)))
	}
	if len(parts) == 0 {
		return []Finding{{Refuse, where, head}}
	}
	what := parts[0]
	if len(parts) == 2 {
		what += "; and " + parts[1]
	}
	return []Finding{{Refuse, where, head + " — " + what + " (SPEC §4.6, docs/SPEC-TABLES.md §18.2)"}}
}

// countFact reads one count token off a field line: zero when the line
// carries none, and false when it carries one that is not a count.
func countFact(f Field, key string) (int64, bool) {
	v, has := f.Get(key)
	if !has {
		return 0, true
	}
	n, err := strconv.ParseInt(v, 10, 64)
	return n, err == nil && n >= 0
}

// bornCount is the count a fresh value of the field's array carries, read off
// its line: a counted array's minimum, a fixed array's bound, and zero for
// every shape whose count the data decides.
func bornCount(f Field, min int64) int64 {
	if shape, _ := f.Get("array"); shape == "fixed" {
		if n, ok := countFact(f, "bound"); ok {
			return n
		}
	}
	return min
}

// defaultElements spells a born count the way the sentence needs it.
func defaultElements(n int64) string {
	switch n {
	case 0:
		return "an empty array"
	case 1:
		return "1 default element"
	}
	return fmt.Sprintf("%d default elements", n)
}

// deprecatedVariantFinding reports an enum variant whose `| deprecated`
// marker turned ON since the baseline (docs/SPEC-TABLES.md §2.15). The id and
// the ordinal stay, so nothing else on the variant's line moves; what moves is
// the read: a stored value naming the variant yields the field's declared
// default, an array or map element the element's default, and the variant is
// not counted unknown. It warns, as a removed variant does, and
// TestDeprecatedVariantPredictionAgreesWithTheRead pins the sentence against
// a real read. The marker turning OFF passes: a deprecated variant is never
// written (§2.15), so the only stored values that name it predate the
// deprecation, and they read as the variant they were written as.
func deprecatedVariantFinding(enum string, was, now Variant) []Finding {
	if was.Deprecated || !now.Deprecated {
		return nil
	}
	return []Finding{{Warn, "enum " + enum, fmt.Sprintf(
		"variant %s deprecated — stored values naming it read as the field's declared default (an array or map element as the element's default) and nothing is counted (docs/SPEC-TABLES.md §2.15)",
		now.Name)}}
}
