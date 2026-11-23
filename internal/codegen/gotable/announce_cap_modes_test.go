package gotable

import (
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// compact drops every space, tab and newline, so a one-line function the
// emitter writes and gofmt then spreads over lines is matched either way.
func compact(b []byte) string {
	return strings.Join(strings.Fields(string(b)), "")
}

// THE ANNOUNCEMENT WRITER'S REFUSAL HOLDS IN BOTH RUNTIME FORMS
// (docs/SPEC-TABLES.md §3.3, R8 follow-up). The shared runtime is built by
// hoisting every declaration identical across its probe units, and an earlier
// form of the refusal was one constant, identical in every probe (false), so
// the shared form hoisted it into the runtime package and a 4097-entry unit
// served its announcement there. The unit's own text now carries the refusal
// (two writers that answer -1 for a unit past 4096 entries) and the shared
// runtime carries no announcement writer at all. This test generates the unit
// in the standalone form and in the shared form, at 4096 and 4097 entries, and
// checks the unit file's writers and that nothing under the shared runtime
// defines or hoists them. It is red when the refusal is a hoistable constant
// (the old form leaves `if tableAnnounceTooLarge` in the unit and a
// TableAnnounceTooLarge in the runtime) and red when the refusal is dropped.
func TestGoAnnouncementRefusalHoldsInBothRuntimeForms(t *testing.T) {
	t.Parallel()
	const refusedMeasure = "funcAnnounceMeasure()int64{return-1}"
	const refusedWrite = "funcAnnounce(buffer[]byte)int64{return-1}"
	const servedMeasure = "funcAnnounceMeasure()int64{returnint64(len(tableAnnouncement))}"
	for _, rt := range []Runtime{RuntimeStandalone, RuntimeShared} {
		for _, row := range []struct {
			entries int
			refused bool
		}{{4096, false}, {4097, true}} {
			u := unitFrom(t, tablewire.AnnounceSchema(row.entries))
			files, err := golang.Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			tables, err := GenerateRuntime(u, nil, rt)
			if err != nil {
				t.Fatal(err)
			}
			maps.Copy(files, tables)
			var unit string
			for name, data := range files {
				text := compact(data)
				if strings.Contains(text, "AnnounceTooLarge") {
					t.Errorf("%v %d entries: %s carries AnnounceTooLarge, a refusal flag a shared runtime can hoist as false", rt, row.entries, name)
				}
				if strings.Contains(text, "funcAnnounceMeasure()") {
					unit = text
				}
			}
			if unit == "" {
				t.Fatalf("%v %d entries: no file defines AnnounceMeasure", rt, row.entries)
			}
			refused := strings.Contains(unit, refusedMeasure) && strings.Contains(unit, refusedWrite)
			served := strings.Contains(unit, servedMeasure)
			if refused != row.refused || served == row.refused {
				t.Errorf("%v %d entries: refused=%t served=%t, want refused=%t", rt, row.entries, refused, served, row.refused)
			}
		}
	}
	// the shared runtime package itself defines no announcement writer and no
	// refusal constant, for any unit
	shared, err := SharedRuntimeFiles()
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range shared {
		text := compact(data)
		for _, bad := range []string{"funcAnnounceMeasure(", "funcAnnounce(buffer", "AnnounceTooLarge"} {
			if strings.Contains(text, bad) {
				t.Errorf("shared runtime %s contains %q: a per-unit announcement writer was hoisted", name, bad)
			}
		}
	}
}

// The same refusal, run: the generated unit is compiled and called in the
// standalone form and in the shared form, at 4096 (written, and AnnounceRead
// accepts it) and 4097 (Announce and AnnounceMeasure answer -1).
func TestGoAnnouncementRefusalRunsInBothRuntimeForms(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it builds and runs the generated unit in both runtime forms)")
	for _, rt := range []Runtime{RuntimeStandalone, RuntimeShared} {
		for _, row := range []struct {
			entries int
			refused bool
		}{{4096, false}, {4097, true}} {
			schema := tablewire.AnnounceSchema(row.entries)
			probe := `package idcap

import "testing"

func TestAnnounceCap(t *testing.T) {
	refused := ` + map[bool]string{true: "true", false: "false"}[row.refused] + `
	n := AnnounceMeasure()
	buf := make([]byte, 1<<20)
	w := Announce(buf)
	if refused {
		if n >= 0 || w >= 0 {
			t.Fatalf("measure %d, announce %d: want both refused", n, w)
		}
		return
	}
	if n <= 0 || w != n {
		t.Fatalf("measure %d, announce %d: want both to write", n, w)
	}
}
`
			out, err := sharedRun(t, unitFrom(t, schema), probe, rt, nil)
			if err != nil {
				t.Errorf("%v %d entries: %v\n%s", rt, row.entries, err, out)
			}
		}
	}
}
