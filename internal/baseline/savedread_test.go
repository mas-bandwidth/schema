package baseline_test

// The saved-file read every prediction in this package is pinned against: the
// OLD schema takes a JSON text and writes the table wire, and the NEW schema
// reads those bytes. A diagnostic that says what a reader does with a file
// written before an edit is held to what this read does.

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// savedRead is one old save met by the new schema.
type savedRead struct {
	OK     bool             // the new schema read the old save
	Reason string           // the refusal's reason name, "" when it read
	Report tabletext.Report // the read's counters
	Text   string           // the value the new schema holds after the read, as JSON; "" when refused
}

// readOldSave writes `json` as a save of `table` under the `before` schema and
// reads it under the `after` schema.
func readOldSave(t *testing.T, before, after, table, json string) savedRead {
	t.Helper()
	old := unit(t, before)
	om := tabletext.NewModel(old)
	oi := om.New(om.Lookup(table))
	var rr tabletext.Report
	if !om.Read(oi, []byte(json), &rr) {
		t.Fatalf("the old schema does not take the fixture %s: %+v", json, rr)
	}
	saved, err := tablewire.Encode(om, oi)
	if err != nil {
		t.Fatalf("the old schema does not write the fixture %s: %v", json, err)
	}

	live := unit(t, after)
	lm := tabletext.NewModel(live)
	li := lm.New(lm.Lookup(table))
	var out savedRead
	ok, err := tablewire.Decode(lm, li, saved, &out.Report)
	out.OK = ok && err == nil
	if !out.OK {
		out.Reason = tablewire.RefusalReason(err)
		if out.Reason == "" {
			t.Fatalf("the read of %s failed without a refusal reason: %v", json, err)
		}
		return out
	}
	text, err := lm.Write(li)
	if err != nil {
		t.Fatalf("the new schema does not write back what it read from %s: %v", json, err)
	}
	out.Text = string(text)
	return out
}
