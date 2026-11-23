package jstable

import (
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestJsMessageLoadRefusesKindMismatchBatch reads a batch written by
// test/tables/A1.schema (tbla1) under A1's own announcement through the
// generated JavaScript of A2.schema (tbla2), which declares the same arms
// under the same names at other types. The second body rides arm `c`, an
// int32 that tbla2 declares string(8): an announced kind this reader declares
// otherwise is §4's ordinary kind mismatch, and on the message form it
// refuses the batch by name, `kind_mismatch` (docs/SPEC-TABLES.md §3.3), as
// the Go reference does. Before the fix the loader counted Mismatch, skipped
// the arm and delivered both bodies, which the wire-fuzz oracle refuses
// (message-fuzz seed 1, cross-tbla1-to-tbla2.Root-b8).
func TestJsMessageLoadRefusesKindMismatchBatch(t *testing.T) {
	t.Parallel()
	node := jsNode(t)
	writer := loadUnitPaths(t, []string{filepath.Join("..", "..", "..", "test", "tables", "A1.schema")})
	reader := loadUnitPaths(t, []string{filepath.Join("..", "..", "..", "test", "tables", "A2.schema")})
	wm := tabletext.NewModel(writer)
	var bodies []*tabletext.Instance
	for _, text := range []string{`{"id":1,"value":{"a":5},"tail":2}`, `{"id":3,"value":{"c":7},"tail":4}`} {
		inst := wm.New(wm.Lookup("Root"))
		var rep tabletext.Report
		if ok := wm.Read(inst, []byte(text), &rep); !ok || rep.Refused {
			t.Fatalf("the engine did not place %s: %+v", text, rep)
		}
		bodies = append(bodies, inst)
	}
	batch, err := tablewire.EncodeMessages(wm, bodies)
	if err != nil {
		t.Fatal(err)
	}
	// the reference refuses the same batch the same way
	var voc tablewire.Vocabulary
	if err := voc.AnnounceRead(tablewire.Announce(writer), &tabletext.Report{}); err != nil {
		t.Fatal(err)
	}
	rm := tabletext.NewModel(reader)
	into := []*tabletext.Instance{rm.New(rm.Lookup("Root")), rm.New(rm.Lookup("Root"))}
	var rrep tabletext.Report
	if n, _, derr := tablewire.DecodeMessages(rm, into, batch, &voc, &rrep); n != 0 || derr == nil || rrep.Reason != "kind_mismatch" {
		t.Fatalf("the Go reference: n=%d err=%v report=%+v, want 0 and kind_mismatch", n, derr, rrep)
	}
	st := findTable(t, reader, "Root")
	probe := messageProbeHeader(reader, st, "") + `
// the writer announces more entries than this reader declares, so its
// vocabulary holds the wire bound, as the conformance driver sizes it
const wv = new TableVocabulary();
wv.Init(new Array(globalThis.Math.max(TableMessageEntriesHere, 4096)));
if (!AnnounceRead(wv, new Uint8Array(readFileSync("./writer.bin")), {})) { fail("AnnounceRead of the writer's announcement"); }
const wire = new Uint8Array(readFileSync("./batch.bin"));
const values = Array.from({ length: 8 }, () => new Root());
const report = {};
const count = RootLoadMessages(values, wv, wire, report);
if (count !== 0 || report.Verdict !== 1 || report.Reason !== "kind_mismatch") {
  fail("kind-mismatch batch: count=" + count + " " + JSON.stringify(report));
}
if (values[0].id !== 0 || values[0].tail !== 0) {
  fail("a refused batch delivered a body: " + JSON.stringify(values[0], (k, x) => typeof x === "bigint" ? x.toString() : x));
}
` + probeTail()
	out, err := jsRunMessageProbe(t, node, reader, map[string][]byte{
		"ann.bin":    tablewire.Announce(reader),
		"writer.bin": tablewire.Announce(writer),
		"batch.bin":  batch,
	}, probe)
	if err != nil {
		t.Fatalf("kind-mismatch batch: %v\n%s", err, out)
	}
}
