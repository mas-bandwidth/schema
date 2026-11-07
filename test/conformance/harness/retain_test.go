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
// rather than from a backend, and this row drives it through EVERY `retain`
// line of the manifest: the same wires, the same two capacities, and the same counters and saved bytes the C++ reference answers
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
			unit, err := u.get(rc.Unit)
			if err != nil {
				t.Fatal(err)
			}
			model := tabletext.NewModel(unit)
			def := model.Lookup(rc.Root)
			if def == nil {
				t.Fatalf("%s declares no root %s", rc.Unit, rc.Root)
			}
			wire := wireBytes(t, rc.Wire)

			load := func(capacity, ids int) (*tabletext.Instance, *tablewire.Retain, tabletext.Report) {
				t.Helper()
				var report tabletext.Report
				inst := model.New(def)
				store := &tablewire.Retain{Capacity: capacity, IdCapacity: ids}
				ok, err := tablewire.DecodeRetain(model, inst, wire, store, &report)
				if err != nil || !ok || report.Malformed {
					t.Fatalf("the load reported damage on a sound wire: ok=%v err=%v %+v", ok, err, report)
				}
				return inst, store, report
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
				capacity = full.Used() - 1
			}
			inst, store, report := load(capacity, ids)

			got := RetainCounts{Retained: report.Retained, RetainLost: report.RetainLost, Unknown: report.Unknown}
			if got != rc.Load {
				t.Fatalf("the load says %s and the manifest says %s", got, rc.Load)
			}
			if report.KindMismatch != 0 || report.Clamped != 0 || report.Widened != 0 || report.Duplicate != 0 {
				t.Fatalf("retention moved a read counter: %+v", report)
			}

			// AND THE SAVE
			var save tabletext.Report
			saved, err := tablewire.EncodeRetain(model, inst, store, &save)
			if err != nil {
				t.Fatal(err)
			}
			if save.RetainLost != rc.SaveLost {
				t.Fatalf("the save lost %d and the manifest says %d", save.RetainLost, rc.SaveLost)
			}
			if rc.Saves == "" {
				return
			}
			want, err := os.ReadFile(rc.Saves)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(saved, want) {
				t.Fatalf("the save is %d bytes and the pin is %d, and they differ at byte %d",
					len(saved), len(want), firstDifference(saved, want))
			}
		})
	}
}

// §6.6 NAMES THE RETENTION ARM'S ROSTER (schema#682). The arm runs the
// VARIABLE-CLASS FILE roots and nothing else: a fixed-class root's LoadRetain
// is refused by name, and the message form is fixed tables only (§3.3), so no
// message root has retention. Where the register's cell reader looks is
// §6.6's own fuzzer paragraph, so the page states it there. This bound holds
// the page to the harness: RED if the paragraph stops naming the roster, and
// RED if the arm admits a message root.
func TestSpec66NamesTheRetentionArmAsFileRootsOnly(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(conformanceRoot(t), "docs", "SPEC-TABLES.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	const anchor = "The wire fuzzer runs with retention OFF"
	at := strings.Index(text, anchor)
	if at < 0 {
		t.Fatalf("docs/SPEC-TABLES.md §6.6 no longer carries its fuzzer paragraph, anchored at %q", anchor)
	}
	para := text[at:]
	if cut := strings.Index(para, "\n\n"); cut >= 0 {
		para = para[:cut]
	}
	if !strings.Contains(strings.ToLower(para), "file roots") {
		t.Fatalf("docs/SPEC-TABLES.md §6.6's fuzzer paragraph does not name the retention arm's roster as file roots only (schema#682):\n%s", para)
	}

	_, _, u := corpus(t)
	if _, err := newWireRoot(u, "blobdemo", "Catalog", true, true); err == nil {
		t.Fatal("the retention arm admitted a message root (schema#682)")
	}
}
