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

// TestJSONByteOrderMarkAtThePackerEntries pins the same rule at the packer's
// two whole-text entries (docs/SPEC-TABLES.md §17.1, "every rule is §16's"):
// ReadValue and ReadElement skip a leading mark exactly as Model.Read does,
// and a mark anywhere else is not JSON.
func TestJSONByteOrderMarkAtThePackerEntries(t *testing.T) {
	t.Parallel()
	m, inst, _ := read(t, "GlobalSettings", `{}`)
	fv := field(t, inst, "tick_rate")
	for _, tc := range []struct {
		text string
		ok   bool
	}{{"90", true}, {"\xEF\xBB\xBF90", true}, {" \xEF\xBB\xBF90", false}, {"\xEF\xBB90", false}, {"\xEF\xBB\xBF\xEF\xBB\xBF90", false}} {
		var r tabletext.Report
		if got := m.ReadValue(fv, []byte(tc.text), &r); got != tc.ok || r.Malformed == tc.ok {
			t.Errorf("ReadValue %q: ok %v report %+v, want ok %v", tc.text, got, r, tc.ok)
		}
		if tc.ok && fv.Cell.U != 90 {
			t.Errorf("ReadValue %q placed %d", tc.text, fv.Cell.U)
		}
	}
	m, inst, _ = read(t, "ProfileConfig", `{}`)
	ev := field(t, inst, "ratings")
	for _, tc := range []struct {
		text string
		ok   bool
	}{{"2.5", true}, {"\xEF\xBB\xBF2.5", true}, {" \xEF\xBB\xBF2.5", false}, {"\xEF\xBB2.5", false}} {
		var r tabletext.Report
		if got := m.ReadElement(ev, 0, []byte(tc.text), &r); got != tc.ok || r.Malformed == tc.ok {
			t.Errorf("ReadElement %q: ok %v report %+v, want ok %v", tc.text, got, r, tc.ok)
		}
		if tc.ok && float32(ev.Elems[0].F) != 2.5 {
			t.Errorf("ReadElement %q placed %v", tc.text, ev.Elems[0].F)
		}
	}
}
