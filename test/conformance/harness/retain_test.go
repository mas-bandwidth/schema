package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE ORACLE CLAIMS THE RETAIN SURFACE (docs/SPEC-TABLES.md §6.6). The
// compiler's engine is a third reading of §3 and §6.6, written from the page
// rather than from a backend, and this row drives it through EVERY `retain` and
// `retain-message` line of the manifest: the same wires, the same two
// capacities, and the same counters and saved bytes the C++ reference answers
// on the same rows.
//
// It is the manifest's own expectation that is checked here and not a second
// one. A row whose counters drift shows up in two places at once, here and in
// the matrix, which is what makes the manifest data rather than a restatement
// of what one engine happens to do.
func TestTheOracleAnswersEveryRetainRow(t *testing.T) {
	m, _, u := corpus(t)
	if len(m.Retains) == 0 {
		t.Fatal("the manifest names no retain row")
	}
	for _, rc := range m.Retains {
		t.Run(rc.Name, func(t *testing.T) {
			// A MESSAGE ROW READS TWO FILES, because a batch's id table is
			// somewhere else (§3.3): the CONNECTION's announcement carries it.
			unitKey := rc.Unit
			var vocabulary *tablewire.Vocabulary
			if rc.Message {
				c, err := m.LookupConnection(rc.Connection)
				if err != nil {
					t.Fatal(err)
				}
				// the announcement is the SENDER's and the row's unit column
				// is the READER's, which is the build that cannot name what
				// the sender wrote
				_, vocabulary = announced(t, u, c)
			}
			unit, err := u.get(unitKey)
			if err != nil {
				t.Fatal(err)
			}
			model := tabletext.NewModel(unit)
			def := model.Lookup(rc.Root)
			if def == nil {
				t.Fatalf("%s declares no root %s", unitKey, rc.Root)
			}
			wire := wireBytes(t, rc.Wire)

			load := func(capacity, ids int) ([]*tabletext.Instance, []*tablewire.Retain, tabletext.Report) {
				t.Helper()
				var report tabletext.Report
				if !rc.Message {
					inst := model.New(def)
					store := &tablewire.Retain{Capacity: capacity, IdCapacity: ids}
					ok, err := tablewire.DecodeRetain(model, inst, wire, store, &report)
					if err != nil || !ok || report.Malformed {
						t.Fatalf("the load reported damage on a sound wire: ok=%v err=%v %+v", ok, err, report)
					}
					return []*tabletext.Instance{inst}, []*tablewire.Retain{store}, report
				}
				count, err := tablewire.MessageCount(wire, vocabulary)
				if err != nil {
					t.Fatal(err)
				}
				insts := make([]*tabletext.Instance, count)
				stores := make([]*tablewire.Retain, count)
				for i := range insts {
					insts[i] = model.New(def)
					stores[i] = &tablewire.Retain{Capacity: capacity, IdCapacity: ids}
				}
				read, ok, err := tablewire.DecodeRetainMessages(model, insts, wire, vocabulary, stores, &report)
				if err != nil || !ok || report.Malformed || read != count {
					t.Fatalf("the load reported damage on a sound batch: read=%d ok=%v err=%v %+v", read, ok, err, report)
				}
				return insts, stores, report
			}

			// THE CAPACITY RULES, applied rather than read as numbers: a
			// record's byte cost is the port's own, so `short` is one byte
			// under what THIS engine's own full load used (§6.6).
			const roomy = 1 << 20
			ids := rc.Ids
			if ids < 0 {
				ids = roomy / 8 // the page's own C/8 bound, which no file reaches
			}
			capacity := roomy
			if rc.Short {
				_, full, _ := load(roomy, roomy/8)
				used := 0
				for _, store := range full {
					used += store.Used()
				}
				capacity = used - 1
			}
			insts, stores, report := load(capacity, ids)

			got := RetainCounts{Retained: report.Retained, RetainLost: report.RetainLost, Unknown: report.Unknown}
			if got != rc.Load {
				t.Fatalf("the load says %s and the manifest says %s", got, rc.Load)
			}
			if report.KindMismatch != 0 || report.Clamped != 0 || report.Widened != 0 || report.Duplicate != 0 {
				t.Fatalf("retention moved a read counter: %+v", report)
			}

			// AND THE SAVE, which is the VARIABLE form on every row: retention
			// writing form 2 refuses by name (§3.3).
			var save tabletext.Report
			var saved []byte
			for i, inst := range insts {
				out, err := tablewire.EncodeRetain(model, inst, stores[i], &save)
				if err != nil {
					t.Fatal(err)
				}
				saved = append(saved, out...)
			}
			if save.RetainLost != rc.SaveLost {
				t.Fatalf("the save lost %d and the manifest says %d", save.RetainLost, rc.SaveLost)
			}
			if len(rc.Saves) == 0 {
				return
			}
			var want []byte
			for _, path := range rc.Saves {
				bs, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				want = append(want, bs...)
			}
			if !bytes.Equal(saved, want) {
				t.Fatalf("the save is %d bytes and the pin is %d, and they differ at byte %d",
					len(saved), len(want), firstDifference(saved, want))
			}
		})
	}
}

// TestAMessageRowRefusesShortByName is the loader's own control
// (docs/SPEC-TABLES.md §6.6, §3.3, schema#681). `short` is ONE BUFFER's rule,
// one byte short of the last record of one port's one store, and a batch takes
// one buffer a body with no rule on the page for which body's record that is.
// A message row carrying it would be answered by each leg's own reading, so the
// loader refuses the pair by name, and the same row at `full` still loads: what
// is refused is the pair and not the row.
func TestAMessageRowRefusesShortByName(t *testing.T) {
	dir := t.TempDir()
	row := func(capacity string) string {
		return "unit u test/tables/RT1.schema\n" +
			"connection c u 0x0 c.bin\n" +
			"retain-message m c u Node m.bin " + capacity + " full 0,0,0 0 -\n"
	}
	short := filepath.Join(dir, "short.txt")
	writeFile(t, short, row("short"))
	if _, err := ReadManifest(short, dir); err == nil {
		t.Fatal("a retain-message row carrying short loaded")
	} else if !strings.Contains(err.Error(), "short is one buffer's rule") {
		t.Fatalf("the refusal does not say why: %v", err)
	}
	full := filepath.Join(dir, "full.txt")
	writeFile(t, full, row("full"))
	m, err := ReadManifest(full, dir)
	if err != nil {
		t.Fatalf("the same row at full is refused: %v", err)
	}
	if len(m.Retains) != 1 || !m.Retains[0].Message || m.Retains[0].Short {
		t.Fatalf("the full row did not load as a message row at full capacity: %+v", m.Retains)
	}
}
