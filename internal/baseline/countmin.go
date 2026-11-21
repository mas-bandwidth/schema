package baseline

import (
	"fmt"
	"strconv"
)

// countMinFinding judges a counted array's MINIMUM COUNT moving, in either
// direction (RuleCountMin). `was` and `now` are the two minimums, "0" for a
// line that carries no `mincount=`: a `[..N]T`, and every other array shape.
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
// So any move refuses, and the sentence says which of the two applies.
func countMinFinding(where, was, now string) []Finding {
	before, err1 := strconv.ParseInt(was, 10, 64)
	after, err2 := strconv.ParseInt(now, 10, 64)
	if err1 != nil || err2 != nil {
		// a count neither side reads as a number is a file this check cannot
		// judge, and it says so rather than passing
		return []Finding{{Refuse, where, fmt.Sprintf("%s %s -> %s", tokenNoun("mincount"), was, now)}}
	}
	if before == after {
		return nil
	}
	born := fmt.Sprintf(
		"a file whose writer elided the array at its born value, %s, reads back as %s, and nothing reports it",
		defaultElements(before), defaultElements(after))
	if after > before {
		return []Finding{{Refuse, where, fmt.Sprintf(
			"%s %d -> %d — a file holding the array with fewer than %d elements no longer reads: the load refuses count_over_bound; and %s (SPEC §4.6, docs/SPEC-TABLES.md §18.2)",
			tokenNoun("mincount"), before, after, after, born)}}
	}
	return []Finding{{Refuse, where, fmt.Sprintf(
		"%s %d -> %d — %s (SPEC §4.6, docs/SPEC-TABLES.md §18.2)",
		tokenNoun("mincount"), before, after, born)}}
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
