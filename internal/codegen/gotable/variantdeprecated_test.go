// A DEPRECATED ENUM VARIANT READS AS THE FIELD DEFAULT AND IS REFUSED ON WRITE
// (docs/SPEC-TABLES.md §2.15), on the id-table wire, form 3, the message form
// and JSON/text. The wire is written by the SAME schema with the variant LIVE,
// so the bytes are those of an older writer; the same bytes read with the
// variant live yield it (the reversed witness), proving the test sees the rule.
package gotable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const variantDeprecatedLiveSchema = `package probe
enum Ship {
    Fighter
    Bomber
    Scout
}
fixed table Root {
    ship  Ship = Scout
    fleet [..2]Ship
}
`

const variantDeprecatedSchema = `package probe
enum Ship {
    Fighter
    Bomber | deprecated
    Scout
}
fixed table Root {
    ship  Ship = Scout
    fleet [..2]Ship
}
`

const variantDeprecatedLiveTest = `package probe
import ("os";"testing")
func TestLiveWritesAndReads(t *testing.T) {
	dir := "__DIR__"
	var v Root
	RootReset(&v)
	v.Ship = ShipBomber
	v.FleetCount = 1
	v.Fleet[0] = ShipBomber
	// The id-table wire (form 1).
	n := RootMeasure(&v)
	if n < 0 { t.Fatal("measure") }
	wire := make([]byte, n)
	if RootSave(&v, wire) != n { t.Fatal("save") }
	if err := os.WriteFile(dir+"/wire", wire, 0o600); err != nil { t.Fatal(err) }
	// Form 3.
	fixed := make([]byte, RootFixedMeasure(1))
	if RootFixedSave([]Root{v}, fixed) != int64(len(fixed)) { t.Fatal("fixed save") }
	if err := os.WriteFile(dir+"/fixed", fixed, 0o600); err != nil { t.Fatal(err) }
	// The message form.
	values := []*Root{&v}
	mn := RootMeasureMessages(values, nil)
	message := make([]byte, mn)
	if RootSaveMessages(values, message, nil) != mn { t.Fatal("message save") }
	if err := os.WriteFile(dir+"/message", message, 0o600); err != nil { t.Fatal(err) }
	announcement := make([]byte, AnnounceMeasure())
	Announce(announcement)
	if err := os.WriteFile(dir+"/announce", announcement, 0o600); err != nil { t.Fatal(err) }
	// JSON/text.
	jn := RootToJsonMeasure(&v)
	text := make([]byte, jn)
	if RootToJson(&v, text) != jn { t.Fatal("to json") }
	if err := os.WriteFile(dir+"/json", text, 0o600); err != nil { t.Fatal(err) }

	// THE REVERSED WITNESS: the same bytes read with Bomber LIVE yield Bomber.
	var w Root
	var r TableReport
	if !RootLoad(&w, wire, &r) || r != (TableReport{}) || w.Ship != ShipBomber || w.FleetCount != 1 || w.Fleet[0] != ShipBomber {
		t.Fatalf("live wire witness: %+v %+v", w, r)
	}
	var records [1]Root
	plan := make([]TableFixedEntry, 128)
	r = TableReport{}
	if n := RootFixedLoad(records[:], fixed, plan, &r); n != 1 || r != (TableReport{}) || records[0].Ship != ShipBomber || records[0].Fleet[0] != ShipBomber {
		t.Fatalf("live fixed witness: %d %+v %+v", n, records[0], r)
	}
	var storage [TableMessageEntriesHere]TableMessageEntry
	var vocab TableVocabulary
	vocab.Init(storage[:])
	r = TableReport{}
	if !AnnounceRead(&vocab, announcement, &r) { t.Fatalf("announce: %+v", r) }
	var decoded [1]Root
	r = TableReport{}
	if n, ok := RootLoadMessages(decoded[:], &vocab, message, &r); !ok || n != 1 || r != (TableReport{}) || decoded[0].Ship != ShipBomber || decoded[0].Fleet[0] != ShipBomber {
		t.Fatalf("live message witness: %d %v %+v %+v", n, ok, decoded[0], r)
	}
	var jv Root
	r = TableReport{}
	if !RootFromJson(&jv, text, &r) || r != (TableReport{}) || jv.Ship != ShipBomber || jv.Fleet[0] != ShipBomber {
		t.Fatalf("live json witness: %+v %+v", jv, r)
	}
}
`

const variantDeprecatedReadTest = `package probe
import ("os";"testing")
func readFile(t *testing.T, dir, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(dir+"/"+name)
	if err != nil { t.Fatal(err) }
	return data
}
func TestDeprecatedReadsDefaultAndWritersRefuse(t *testing.T) {
	dir := "__DIR__"
	wire := readFile(t, dir, "wire")
	fixed := readFile(t, dir, "fixed")
	message := readFile(t, dir, "message")
	announcement := readFile(t, dir, "announce")
	text := readFile(t, dir, "json")

	// THE ID-TABLE WIRE: the field reads its declared default (Scout), the
	// array element reads the ELEMENT default (None), and nothing is unknown.
	var v Root
	var r TableReport
	if !RootLoad(&v, wire, &r) { t.Fatalf("deprecated wire refused: %+v", r) }
	if r.Unknown != 0 || r.Clamped != 0 || r.KindMismatch != 0 || r.Malformed {
		t.Fatalf("deprecated wire counted an event: %+v", r)
	}
	if v.Ship != ShipScout || v.FleetCount != 1 || v.Fleet[0] != ShipNone {
		t.Fatalf("deprecated wire: %+v", v)
	}
	// FORM 3.
	var records [1]Root
	plan := make([]TableFixedEntry, 128)
	r = TableReport{}
	if n := RootFixedLoad(records[:], fixed, plan, &r); n != 1 { t.Fatalf("deprecated fixed: %d %+v", n, r) }
	if r.Unknown != 0 || r.Clamped != 0 || r.KindMismatch != 0 || r.Malformed {
		t.Fatalf("deprecated fixed counted an event: %+v", r)
	}
	if records[0].Ship != ShipScout || records[0].Fleet[0] != ShipNone {
		t.Fatalf("deprecated fixed: %+v", records[0])
	}
	// THE MESSAGE FORM.
	var storage [TableMessageEntriesHere]TableMessageEntry
	var vocab TableVocabulary
	vocab.Init(storage[:])
	r = TableReport{}
	if !AnnounceRead(&vocab, announcement, &r) { t.Fatalf("announce: %+v", r) }
	var decoded [1]Root
	r = TableReport{}
	if n, ok := RootLoadMessages(decoded[:], &vocab, message, &r); !ok || n != 1 { t.Fatalf("deprecated message: %d %v %+v", n, ok, r) }
	if r.Unknown != 0 || r.Clamped != 0 || r.KindMismatch != 0 || r.Malformed {
		t.Fatalf("deprecated message counted an event: %+v", r)
	}
	if decoded[0].Ship != ShipScout || decoded[0].Fleet[0] != ShipNone {
		t.Fatalf("deprecated message: %+v", decoded[0])
	}
	// JSON/TEXT.
	var jv Root
	r = TableReport{}
	if !RootFromJson(&jv, text, &r) { t.Fatalf("deprecated json refused: %+v", r) }
	if r.Unknown != 0 || r.Clamped != 0 || r.KindMismatch != 0 || r.Malformed {
		t.Fatalf("deprecated json counted an event: %+v", r)
	}
	if jv.Ship != ShipScout || jv.Fleet[0] != ShipNone {
		t.Fatalf("deprecated json: %+v", jv)
	}

	// EVERY WRITER REFUSES A VALUE HOLDING THE DEPRECATED VARIANT, and the
	// reason twin names invalid_value.
	var bad Root
	RootReset(&bad)
	bad.Ship = ShipBomber
	if n := RootSave(&bad, make([]byte, 4096)); n != -1 { t.Fatalf("Save accepted a deprecated variant: %d", n) }
	if size, err := RootMeasureReason(&bad); size != -1 || err != TableRefuseInvalidValue { t.Fatalf("MeasureReason: %d %v", size, err) }
	if n := RootFixedSave([]Root{bad}, make([]byte, 4096)); n != -1 { t.Fatalf("FixedSave accepted a deprecated variant: %d", n) }
	if _, err := RootFixedSaveReason([]Root{bad}, make([]byte, 4096)); err != TableRefuseInvalidValue { t.Fatalf("FixedSaveReason: %v", err) }
	if n := RootSaveMessages([]*Root{&bad}, make([]byte, 4096), nil); n != -1 { t.Fatalf("SaveMessages accepted a deprecated variant: %d", n) }
	if n := RootToJsonMeasure(&bad); n != -1 { t.Fatalf("ToJsonMeasure accepted a deprecated variant: %d", n) }
	if n := RootToJson(&bad, make([]byte, 4096)); n != -1 { t.Fatalf("ToJson accepted a deprecated variant: %d", n) }
	if RootCookFrom(&bad, make([]byte, 1<<20), TableByteOrderLittle) { t.Fatal("CookFrom accepted a deprecated variant") }

	// An ARRAY element is the caller's value too.
	var arr Root
	RootReset(&arr)
	arr.FleetCount = 1
	arr.Fleet[0] = ShipBomber
	if n := RootSave(&arr, make([]byte, 4096)); n != -1 { t.Fatalf("Save accepted a deprecated array element: %d", n) }
}
`

func TestDeprecatedVariantReadsDefaultWriteRefused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	runGenerated(t, variantDeprecatedLiveSchema, strings.Replace(variantDeprecatedLiveTest, "__DIR__", dir, 1))
	runGenerated(t, variantDeprecatedSchema, strings.Replace(variantDeprecatedReadTest, "__DIR__", dir, 1))

	// THE ORACLE AGREES: internal/tablewire and internal/tabletext read the
	// same older-writer bytes as the field default, with no unknown counted.
	t.Run("oracle", func(t *testing.T) {
		t.Parallel()
		u := unitFrom(t, variantDeprecatedSchema)
		m := tabletext.NewModel(u)
		wire, err := os.ReadFile(filepath.Join(dir, "wire"))
		if err != nil {
			t.Fatal(err)
		}
		inst := m.New(m.Lookup("Root"))
		var report tabletext.Report
		if ok, err := tablewire.Decode(m, inst, wire, &report); !ok || err != nil {
			t.Fatalf("oracle wire: %v %+v", err, report)
		}
		if report.Unknown != 0 || report.Clamped != 0 || report.KindMismatch != 0 || report.Malformed {
			t.Fatalf("oracle wire counted an event: %+v", report)
		}
		if inst.Fields[0].Cell.U != uint64(tabletext.EnumValue(u.Enums["Ship"], "Scout")) {
			t.Fatalf("oracle wire field: %d", inst.Fields[0].Cell.U)
		}
		if inst.Fields[1].Elems[0].U != 0 {
			t.Fatalf("oracle wire element: %d", inst.Fields[1].Elems[0].U)
		}

		text, err := os.ReadFile(filepath.Join(dir, "json"))
		if err != nil {
			t.Fatal(err)
		}
		inst = m.New(m.Lookup("Root"))
		report = tabletext.Report{}
		if !m.Read(inst, text, &report) {
			t.Fatalf("oracle text: %+v", report)
		}
		if report.Unknown != 0 || report.Clamped != 0 || report.KindMismatch != 0 || report.Malformed {
			t.Fatalf("oracle text counted an event: %+v", report)
		}
		if inst.Fields[0].Cell.U != uint64(tabletext.EnumValue(u.Enums["Ship"], "Scout")) || inst.Fields[1].Elems[0].U != 0 {
			t.Fatalf("oracle text: %+v", inst.Fields)
		}

		message, err := os.ReadFile(filepath.Join(dir, "message"))
		if err != nil {
			t.Fatal(err)
		}
		announcement, err := os.ReadFile(filepath.Join(dir, "announce"))
		if err != nil {
			t.Fatal(err)
		}
		var vocab tablewire.Vocabulary
		report = tabletext.Report{}
		if err := vocab.AnnounceRead(announcement, &report); err != nil {
			t.Fatalf("oracle announcement: %v %+v", err, report)
		}
		inst = m.New(m.Lookup("Root"))
		report = tabletext.Report{}
		if n, ok, err := tablewire.DecodeMessages(m, []*tabletext.Instance{inst}, message, &vocab, &report); err != nil || !ok || n != 1 {
			t.Fatalf("oracle message: %d %v %v %+v", n, ok, err, report)
		}
		if inst.Fields[0].Cell.U != uint64(tabletext.EnumValue(u.Enums["Ship"], "Scout")) || inst.Fields[1].Elems[0].U != 0 {
			t.Fatalf("oracle message: %+v", inst.Fields)
		}
		inst.Fields[0].Cell.U = uint64(tabletext.EnumValue(u.Enums["Ship"], "Bomber"))
		inst.Fields[1].Elems[0].U = uint64(tabletext.EnumValue(u.Enums["Ship"], "Bomber"))
		if _, err := tablewire.Encode(m, inst); err == nil {
			t.Fatal("oracle wire writer accepted a deprecated variant")
		}
		if _, err := m.Write(inst); err == nil {
			t.Fatal("oracle text writer accepted a deprecated variant")
		}
	})
}
