package tablewire_test

import (
	"bytes"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// §3, §2.11: kind 36, the timestamp, is an eight-byte value, so a field this
// reader cannot name under kind 36 is skipped at its width and counted, and
// the body after it still decodes. Kind 35, the fixed layout's optional
// wrapper, never rides a record and stays framing damage. The Go reader, this
// oracle and the conformance harness's frame scanner all read kind 36 so; a
// disagreement is what the wire fuzz found (tables-go-wire-fuzz).
func TestTimestampKindIsSkippedAtEightBytes(t *testing.T) {
	t.Parallel()
	m := model(t)
	inst := place(t, m, "GlobalSettings", `{ "tick_rate": 90 }`)
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	if got := ir.TableKindWidth(ir.TableKindTimestamp); got != 8 {
		t.Fatalf("kind 36 should be eight bytes wide, got %d", got)
	}
	payload := []byte{0x00, 0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70}

	hostile := withField(t, wire, 0xEEEEEEEEEEEEEEEE, ir.TableKindTimestamp, payload)
	back := m.New(m.Lookup("GlobalSettings"))
	var r tabletext.Report
	if ok, err := tablewire.Decode(m, back, hostile, &r); !ok || err != nil {
		t.Fatalf("an unknown kind-36 field should skip at eight bytes, not stop the decode: %v %+v", err, r)
	}
	if r.Unknown != 1 || r.Malformed {
		t.Fatalf("expected one unknown and no damage, got %+v", r)
	}
	fv, _ := back.FieldByKey("tick_rate")
	if fv.Cell.U != 90 {
		t.Fatal("the field after the kind-36 one did not decode")
	}

	// THE RETAIN PATH (§6.6) agrees: the unnamed kind-36 field is one retained
	// record, never a loss, and the retaining save writes its eight bytes back
	// so a plain read of the save meets it again as one unknown
	kept := m.New(m.Lookup("GlobalSettings"))
	retain := tablewire.Retain{Capacity: 8192, IdCapacity: 1024}
	r = tabletext.Report{}
	if ok, err := tablewire.DecodeRetain(m, kept, hostile, &retain, &r); !ok || err != nil || r.Malformed {
		t.Fatalf("a retaining read of a kind-36 field should not be damage: %v %+v", err, r)
	}
	if r.Retained != 1 || r.RetainLost != 0 {
		t.Fatalf("retained=%d retain_lost=%d, want 1 / 0", r.Retained, r.RetainLost)
	}
	var saved tabletext.Report
	out, err := tablewire.EncodeRetain(m, kept, &retain, &saved)
	if err != nil || saved.RetainLost != 0 {
		t.Fatalf("the retaining save lost the kind-36 field: %v %+v", err, saved)
	}
	if !bytes.Contains(out, append([]byte{ir.TableKindTimestamp}, payload...)) {
		t.Fatal("the retaining save does not carry the kind-36 field's eight bytes")
	}
	again := m.New(m.Lookup("GlobalSettings"))
	r = tabletext.Report{}
	if ok, err := tablewire.Decode(m, again, out, &r); !ok || err != nil || r.Unknown != 1 || r.Malformed {
		t.Fatalf("the save read back should meet one unknown and no damage: %v %+v", err, r)
	}

	damaged := withField(t, wire, 0xEEEEEEEEEEEEEEEE, 35, payload)
	back = m.New(m.Lookup("GlobalSettings"))
	r = tabletext.Report{}
	_, _ = tablewire.Decode(m, back, damaged, &r)
	if !r.Malformed {
		t.Fatalf("kind 35 should be framing damage, got %+v", r)
	}
}
