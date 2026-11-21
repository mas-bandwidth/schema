package baseline_test

// The saved-file read every prediction in this package is pinned against: the
// OLD schema takes a JSON text and writes the table wire, and the NEW schema
// reads those bytes. A diagnostic that says what a reader does with a file
// written before an edit is held to what this read does.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// savedRead is one old save met by the new schema.
type savedRead struct {
	OK     bool             // the new schema read the old save
	Reason string           // the refusal's reason name, "" when it read
	Report tabletext.Report // the read's counters
	Text   string           // what the new schema holds after the read, as compact JSON; "" when refused
	Own    string           // what the OLD schema reads back from its own save, as compact JSON
}

// tryUnit is unit without the fatal: the compiler's answer for a source it
// may refuse.
func tryUnit(src string) (*ir.Unit, error) {
	f, perrs := parser.Parse("Fixture.schema", []byte(src))
	if len(perrs) > 0 {
		return nil, perrs[0]
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Fixture.schema", Name: "Fixture.schema", Base: "Fixture", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		return nil, cerrs[0]
	}
	return u, nil
}

// compact drops a JSON text's layout, so two texts compare by content.
func compact(text string) string { return strings.Join(strings.Fields(text), "") }

// trySavedRead writes `json` as a save of `table` under the old unit and reads
// it under the new one. The error is the fixture's: a text the old schema
// does not take, or a value either schema cannot write back.
func trySavedRead(old, live *ir.Unit, table, json string) (savedRead, error) {
	var out savedRead
	om := tabletext.NewModel(old)
	oi := om.New(om.Lookup(table))
	var rr tabletext.Report
	if !om.Read(oi, []byte(json), &rr) {
		return out, fmt.Errorf("the old schema does not take the fixture %s: %s", json, rr.Reason)
	}
	saved, err := tablewire.Encode(om, oi)
	if err != nil {
		return out, fmt.Errorf("the old schema does not write the fixture %s: %w", json, err)
	}
	// the control: the old schema reads its own save
	back := om.New(om.Lookup(table))
	var br tabletext.Report
	if ok, err := tablewire.Decode(om, back, saved, &br); !ok || err != nil {
		return out, fmt.Errorf("the old schema does not read its own save of %s: %v", json, err)
	}
	own, err := om.Write(back)
	if err != nil {
		return out, fmt.Errorf("the old schema does not write back its own save of %s: %w", json, err)
	}
	out.Own = compact(string(own))

	lm := tabletext.NewModel(live)
	li := lm.New(lm.Lookup(table))
	ok, err := tablewire.Decode(lm, li, saved, &out.Report)
	out.OK = ok && err == nil
	if !out.OK {
		out.Reason = tablewire.RefusalReason(err)
		if out.Reason == "" {
			return out, fmt.Errorf("the read of %s failed without a refusal reason: %v", json, err)
		}
		return out, nil
	}
	text, err := lm.Write(li)
	if err != nil {
		// a value the reader holds and its own writer refuses (a flags mask
		// naming no variant, say): the read happened, and the text says so
		out.Text = "unwritable: " + err.Error()
		return out, nil
	}
	out.Text = compact(string(text))
	return out, nil
}

// readOldSave is trySavedRead over two sources, fatal on a broken fixture.
func readOldSave(t *testing.T, before, after, table, json string) savedRead {
	t.Helper()
	r, err := trySavedRead(unit(t, before), unit(t, after), table, json)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
