package tabletext_test

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// TestJSONByteOrderMarkAtTheEntry pins docs/SPEC-TABLES.md §16.2 and §16.11 at
// the engine's whole-text entry, Model.Read: a leading UTF-8 byte-order mark is
// skipped and lands the same instance and the same report as the text without
// it; the mark anywhere else, or cut short, is not JSON.
func TestJSONByteOrderMarkAtTheEntry(t *testing.T) {
	t.Parallel()
	const body = `{ "tick_rate": 90 }`
	_, plain, plainReport := read(t, "GlobalSettings", body)
	_, marked, markedReport := read(t, "GlobalSettings", "\xEF\xBB\xBF"+body)
	if plainReport != (tabletext.Report{}) || markedReport != plainReport {
		t.Fatalf("a leading mark: report %+v, want %+v", markedReport, plainReport)
	}
	if field(t, marked, "tick_rate").Cell.U != 90 || field(t, plain, "tick_rate").Cell.U != 90 {
		t.Fatal("a leading mark changed the value placed")
	}
	for _, text := range []string{
		" \xEF\xBB\xBF" + body,            // after whitespace: not at the start
		body + "\xEF\xBB\xBF",             // trailing
		"\xEF\xBB" + body,                 // cut short
		"\xEF\xBB\xBF\xEF\xBB\xBF" + body, // twice
		"\xFF\xFE" + body,                 // the UTF-16 mark is not UTF-8
	} {
		if _, _, r := read(t, "GlobalSettings", text); !r.Malformed {
			t.Errorf("%q: read clean, want Malformed", text)
		}
	}
}
