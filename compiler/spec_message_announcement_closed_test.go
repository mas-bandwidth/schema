// TestSpecMessageAnnouncementIsClosed pins docs/SPEC-TABLES.md §3.3's SECURITY
// rule that the announcement grammar is CLOSED: an announcement is refused
// unless a schema the compiler accepts could announce it (docs/rulings.json
// message-announcement-closed). The closed list states every condition, with
// the base condition among them, and the ruling exists with its date and words.
package compiler

import (
	"strings"
	"testing"
)

func TestSpecMessageAnnouncementIsClosed(t *testing.T) {
	t.Parallel()
	page := readSpecTables(t)
	security := specTablesSection(t, page, "#### SECURITY", "#### THE COST RULE, and it is this form's own")

	// the page wraps at a fixed width, so a sentence is matched with its line
	// breaks folded into single spaces
	flat := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	text := flat(security)

	wants := []string{
		// the base condition, among the list
		"an announcement is refused unless a schema the compiler accepts could announce it",
		// element kinds only from the closed set
		"KIND is one of §3's closed set",
		// shapes nested at most 128 levels, and no array or keyed shape whose
		// element is itself an array or keyed shape (the compiler refuses one)
		"nested at most 128 levels",
		"an array or keyed shape whose element is itself an array or keyed shape",
		"an array of arrays is not supported in v1",
		// an array's min at most its max
		"`min` is at most its `max`",
		// a packed bits and a max inside what the kind holds
		"`bits` and a `max` sit inside what the kind holds",
		// a ranged base that lies inside the announced kind
		"`base + 2^bits - 1` lies inside the announced kind",
		"today only the width is held",
	}
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Errorf("§3.3 SECURITY no longer states %q", want)
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
		if q == "an announcement is refused unless a schema the compiler accepts could announce it" {
			quoted = true
		}
	}
	if !quoted {
		t.Errorf("the ruling %q does not quote the base condition", "message-announcement-closed")
	}
}
