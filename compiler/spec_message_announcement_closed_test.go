// TestSpecMessageAnnouncementIsClosed pins docs/SPEC-TABLES.md §3.3's SECURITY
// rule that the announcement grammar is CLOSED: an announcement is refused
// unless a schema the compiler accepts could announce it (docs/rulings.json
// message-announcement-closed). The closed list states every condition, with
// the base condition among them, the two body rules beside it, and the ruling
// exists with its date and words.
package compiler

import (
	"strings"
	"testing"
)

func TestSpecMessageAnnouncementIsClosed(t *testing.T) {
	t.Parallel()
	page := readSpecTables(t)
	security := specTablesSection(t, page, "#### SECURITY", "#### THE COST RULE, and it is this form's own")

	// the page wraps at a fixed width and the list is indented, so a sentence
	// is matched with its line breaks and indentation folded into single spaces
	flat := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	text := flat(security)

	const rule = "an announcement is refused unless a schema the compiler accepts could announce it"
	wants := []string{
		"THE ANNOUNCEMENT GRAMMAR IS CLOSED",
		rule,
		"The conditions are ONE closed list",
		// element kinds only from the closed set
		"an entry's KIND, and an array's or keyed array's element kind, is one of §3's closed set",
		// shapes nested at most 128 levels, and no array or keyed shape whose
		// element is itself an array or keyed shape, naming which fields the
		// compiler refuses
		"a SHAPE is nested at most 128 levels",
		"an array or keyed shape whose element is itself an array or keyed shape is refused",
		"an array of arrays is not supported in v1 (the parser refuses the second bracket)",
		"the checker refuses an array of `bytes(N)`, which is itself a kind `14` array, and an enum-keyed array whose value is an array or a map",
		// an array's min at most its max
		"an array's `min` is at most its `max`",
		// a packed bits and a max inside what the kind holds
		"a packed `bits` and a `max` sit inside what the kind holds",
		// a ranged base such that the top of its range lies inside the kind
		"a RANGED integer's `base` is such that `base + 2^bits - 1` lies inside the announced kind",
		"a width the kind holds is not enough",
		// the zero-bit element, cited where it is written
		"An array element that consumes no bits is refused where it is walked (A WALKED ARRAY ELEMENT THAT CONSUMES NO BITS REFUSES THE READ, above)",
		// a landed integer outside the reader's kind
		"an integer that lands outside the READER's own kind refuses `out_of_range` and is never masked or wrapped",
	}
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Errorf("§3.3 SECURITY no longer states %q", want)
		}
	}
	// the cited paragraph is on the page
	if !strings.Contains(flat(page), "A WALKED ARRAY ELEMENT THAT CONSUMES NO BITS REFUSES THE READ.") {
		t.Errorf("§3.3 no longer carries the paragraph the closed list cites for a zero-bit element")
	}
	// the rule is written, not its history
	stales := []string{
		"today only the width is held",
	}
	for _, stale := range stales {
		if strings.Contains(text, stale) {
			t.Errorf("§3.3 SECURITY still says %q", stale)
		}
	}

	r, ok := readRulings(t)["message-announcement-closed"]
	if !ok {
		t.Fatalf("docs/rulings.json carries no ruling %q", "message-announcement-closed")
	}
	if r.Date != "2026-11-10" {
		t.Errorf("the ruling %q is dated %q, want %q", "message-announcement-closed", r.Date, "2026-11-10")
	}
	if r.Source == "" {
		t.Errorf("the ruling %q carries no source", "message-announcement-closed")
	}
	quoted := false
	for _, q := range r.Quotes {
		if q == "ruled 2026-11-10: "+rule {
			quoted = true
		}
	}
	if !quoted {
		t.Errorf("the ruling %q does not quote %q", "message-announcement-closed", "ruled 2026-11-10: "+rule)
	}
}
