package gotable

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestGoUncoveredEmitters covers the emitter surface no other test reaches.
// GenerateOmitting is the omit path; the two region emitters it used to name
// were empty comment-only stubs, so they were removed rather than tested (a
// no-op function has no emitted behaviour to pin). The message and retain
// forms below keep the behaviour those stubs stood beside under test.
func TestGoUncoveredEmitters(t *testing.T) {
	t.Parallel()
	t.Run("GenerateOmitting_omits_only_the_named_form", func(t *testing.T) {
		t.Parallel()
		u := unitFrom(t, `package probe
fixed table Root { x int32 }
`)
		full, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		hadCook := false
		for name := range full {
			if strings.HasSuffix(name, "Cook.go") {
				hadCook = true
			}
		}
		if !hadCook {
			t.Fatal("baseline unit emits no Cook surface; cannot prove omission")
		}
		out, err := GenerateOmitting(u, nil, FormCook)
		if err != nil {
			t.Fatal(err)
		}
		for name := range out {
			if strings.HasSuffix(name, "Cook.go") {
				t.Fatalf("FormCook omitted but %s still generated", name)
			}
		}
		if _, ok := out["ProbeTable.go"]; !ok {
			t.Fatal("the core ProbeTable.go is missing when the Cook form is omitted")
		}
	})
	t.Run("GenerateOmitting_nothing_is_Generate", func(t *testing.T) {
		t.Parallel()
		u := unitFrom(t, `package probe
fixed table Root { x int32 }
`)
		full, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		out, err := GenerateOmitting(u, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(full, out) {
			t.Fatalf("omitting no form changed the output: %d files vs %d", len(full), len(out))
		}
	})
	t.Run("message_form_round_trips", func(t *testing.T) {
		t.Parallel()
		// A variable companion makes the unit regional; fixed Root carries the
		// message form. Pin a public message-form read, not only its writer.
		runGenerated(t, `package probe
table V { a int32 }
fixed table Root { b int32 }
`, `package probe
import "testing"
func TestRegionMessageRead(t *testing.T) {
	t.Parallel()
	want := Root{B: 7}
	values := []*Root{&want}
	var report TableReport
	n := RootMeasureMessages(values, nil)
	out := make([]byte, n)
	if got := RootSaveMessages(values, out, &report); got != n || report != (TableReport{}) { t.Fatalf("save %d/%d %+v", got, n, report) }
	var entries [TableMessageEntriesHere]TableMessageEntry
	var vocab TableVocabulary
	vocab.Init(entries[:])
	announcement := make([]byte, AnnounceMeasure())
	Announce(announcement)
	if !AnnounceRead(&vocab, announcement, &report) || !vocab.Announced { t.Fatalf("announce %+v", report) }
	var decoded [1]Root
	if count, ok := RootLoadMessages(decoded[:], &vocab, out, &report); !ok || count != 1 || decoded[0] != want || report != (TableReport{}) {
		t.Fatalf("load count=%d ok=%v value=%+v report=%+v", count, ok, decoded[0], report)
	}
}
`)
	})
	t.Run("variable_root_retention_round_trips_unknown_wire_field", func(t *testing.T) {
		t.Parallel()
		// The public retained round trip is the VARIABLE root's region/wire
		// path (§6.6). A message-form retained root is intentionally unavailable:
		// §3.3 gives the message form only to fixed tables, which have no region.
		const oldSchema = `package probe
table Root { b int32 }
`
		const newSchema = `package probe
table Root { b int32
	future int32 }
`
		newModel := tabletext.NewModel(unitFrom(t, newSchema))
		newRoot := newModel.New(newModel.Lookup("Root"))
		var oracleReport tabletext.Report
		if !newModel.Read(newRoot, []byte(`{"b":7,"future":11}`), &oracleReport) {
			t.Fatalf("new-schema text: %+v", oracleReport)
		}
		wire, err := tablewire.Encode(newModel, newRoot)
		if err != nil {
			t.Fatal(err)
		}
		oldModel := tabletext.NewModel(unitFrom(t, oldSchema))
		oldRoot := oldModel.New(oldModel.Lookup("Root"))
		oracleRetain := &tablewire.Retain{Capacity: 1024, IdCapacity: 8}
		if ok, err := tablewire.DecodeRetain(oldModel, oldRoot, wire, oracleRetain, &oracleReport); !ok || err != nil {
			t.Fatalf("oracle retaining load %v %v %+v", ok, err, oracleReport)
		}
		want, err := tablewire.EncodeRetain(oldModel, oldRoot, oracleRetain, &oracleReport)
		if err != nil {
			t.Fatal(err)
		}
		source := fmt.Sprintf(`package probe
import("bytes"; "testing"; "unsafe")
func TestVariableRootRetentionRoundTrip(t *testing.T) {
	t.Parallel()
	wire := []byte{%s}
	want := []byte{%s}
	loadSize := RootLoadMeasure(wire)
	if loadSize < 0 { t.Fatal("load measure refused") }
	raw := make([]byte, int(loadSize)+63)
	offset := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
	region := raw[int(offset):int(offset)+int(loadSize)]
	retain := &TableRetain{Bytes: make([]byte, 1024), Ids: make([]TableRetainId, 8)}
	var report TableReport
	root := RootLoadRetain(region, wire, retain, &report)
	if root == nil || report.Malformed || report.Unknown != 1 || report.Retained != 1 || report.RetainLost != 0 || retain.Count != 1 {
		t.Fatalf("retaining load root=%%+v report=%%+v retain=%%+v", root, report, retain)
	}
	n := RootMeasureRetain(root, retain)
	if n != int64(len(want)) { t.Fatalf("retained measure %%d want %%d", n, len(want)) }
	out := make([]byte, n)
	report = TableReport{}
	if got := RootSaveRetain(root, retain, out, &report); got != n || report.RetainLost != 0 || !bytes.Equal(out, want) {
		t.Fatalf("retained save %%d/%%d report=%%+v got=%%x want=%%x", got, n, report, out, want)
	}
}
`, byteLiterals(wire), byteLiterals(want))
		runGenerated(t, oldSchema, source)
	})
	t.Run("fixed_root_refuses_retain_messages_by_name", func(t *testing.T) {
		t.Parallel()
		// A fixed table's root has no region, so the generated code refuses
		// LoadRetainMessages by name (docs/SPEC-TABLES.md §3.3, §6.6).
		runGenerated(t, `package probe
fixed table Root { b int32 }
`, `package probe
import "testing"
func TestRetainRefusal(t *testing.T){
	const want="Root: retention requires a region round trip through the VARIABLE form"
	if RootLoadRetainMessages!=want{t.Fatalf("refusal %q want %q",RootLoadRetainMessages,want)}
}
`)
	})
}
