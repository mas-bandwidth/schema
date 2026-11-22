package luautable_test

import (
	"encoding/hex"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
)

// TestLuauCountBelowMinRefused pins SPEC §5.1 row 3 ("a counted array's
// count past its Max or below its Min") and §4.2 (a fresh value is born at
// its Min) on the Luau table runtime (review 2026-11-21, cross-leg
// counted-array-min): the variable, JSON, text, fixed and message reads each
// refuse a count below the reader's Min, as the Go engine refuses
// count_over_bound; the variable and fixed writes refuse one; a new value
// holds Min elements. The lawful twin of every case reads or writes. The
// reader and writer schemas, the probe, every case and the wire bytes are the
// committed corpus testdata/review1121/count-below-min.
func TestLuauCountBelowMinRefused(t *testing.T) {
	t.Parallel()
	corpus := filepath.Join(luauReview1121, "count-below-min")
	rows := review1121Rows(t, filepath.Join(corpus, "cases.tsv"), 5)
	reader := review1121Unit(t, filepath.Join(corpus, "reader", "Probe.schema"))
	writer := review1121Unit(t, filepath.Join(corpus, "writer", "Probe.schema"))
	dir := review1121Modules(t, reader, filepath.Join(corpus, "probe.luau"))

	// the committed wire bytes are what the writer's Go engine encodes for
	// the row's value: a fixture that drifts from its schema is refused
	model := tabletext.NewModel(writer)
	instance := func(root, text string) *tabletext.Instance {
		inst := model.New(model.Lookup(root))
		var report tabletext.Report
		if !model.Read(inst, []byte(text), &report) || !report.Silent() {
			t.Fatalf("writer value %s %s: %+v", root, text, report)
		}
		return inst
	}
	refused, held := 0, 0
	for _, r := range rows {
		id, form, input, wire, want := r[0], r[1], r[2], r[3], r[4]
		var b []byte
		var err error
		switch form {
		case "var-read":
			b, err = tablewire.Encode(model, instance("P", input))
		case "message-read":
			b, err = tablewire.EncodeMessages(model, []*tabletext.Instance{instance("M", input)})
		default:
			if wire != "-" {
				t.Errorf("%s: form %s reads no wire, but the row carries %s", id, form, wire)
			}
		}
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if b != nil && hex.EncodeToString(b) != wire {
			t.Errorf("%s: the writer encodes %s as %x, the corpus commits %s", id, input, b, wire)
		}
		if want == "refused" {
			refused++
		} else {
			held++
		}
	}
	if refused == 0 || held == 0 {
		t.Fatalf("the corpus holds %d refused and %d lawful rows: each is proven beside the other", refused, held)
	}
	raw := luaurun.Run(t, dir, "main.luau", map[string][]byte{
		"announce": tablewire.Announce(writer),
		"cases":    review1121CaseLines(rows),
	})
	got := review1121Verdicts(t, raw)
	for _, r := range rows {
		if got[r[0]] != r[4] {
			t.Errorf("%s (%s %s): got %q, want %q", r[0], r[1], r[2], got[r[0]], r[4])
		}
	}
}
