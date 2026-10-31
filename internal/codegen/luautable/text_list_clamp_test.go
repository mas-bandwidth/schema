package luautable_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestLuauTextListClampAndMalformedLoadLikeGo pins the text-list tolerant load
// to the Go reference engine (docs/SPEC-TABLES.md §2.13): an element past M
// clamps at a code point boundary and counts clamped; an element that is not
// UTF-8 counts malformed. For each wire the Luau load must return a value (not
// nil) with the Go engine's six counters and verdict, and save of it must give
// the Go engine's bytes. Every expectation is computed here from
// internal/tabletext and internal/tablewire, never typed by hand.
func TestLuauTextListClampAndMalformedLoadLikeGo(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter")
	g := generateFiles(t, map[string]string{"Lists.schema": textListsSchema})
	m := textListModel(t)

	t.Run("element past M clamps", func(t *testing.T) {
		raw := textListRawWire(t, m, "123456789")
		back := m.New(m.Lookup("Lists"))
		var report tabletext.Report
		if ok, err := tablewire.Decode(m, back, raw, &report); !ok || err != nil {
			t.Fatalf("the Go load refuses the wire: ok=%v err=%v", ok, err)
		}
		if report.Clamped != 1 {
			t.Fatalf("the Go load does not clamp the element: %+v", report)
		}
		want, err := tablewire.Encode(m, back)
		if err != nil {
			t.Fatalf("the Go clamped value does not encode: %v", err)
		}
		got := runTextListProbe(t, g, raw)
		if got.report != goReportText(report) {
			t.Errorf("counters: luau %s, want the Go engine's %s", got.report, goReportText(report))
		}
		if string(got.saved) != string(want) {
			t.Errorf("the save is not the Go writer's bytes: %d saved, %d written; %s", len(got.saved), len(want), firstDiff(got.saved, want))
		}
	})

	t.Run("ill-formed UTF-8 is malformed", func(t *testing.T) {
		// damage discards the whole load (docs/SPEC-TABLES.md §4): the Go
		// load refuses it, and the leg owes the refusal and nothing else
		raw := textListRawWire(t, m, "\xff\xfe")
		var report tabletext.Report
		if ok, err := tablewire.Decode(m, m.New(m.Lookup("Lists")), raw, &report); ok || err != nil {
			t.Fatalf("the Go load does not refuse the damaged load: ok=%v err=%v", ok, err)
		}
		if !report.Malformed {
			t.Fatalf("the Go load does not call the element malformed: %+v", report)
		}
		got := runTextListProbe(t, g, raw)
		if !strings.HasSuffix(got.report, ",true") {
			t.Errorf("counters: luau %s, and a damaged load owes malformed", got.report)
		}
	})
}
