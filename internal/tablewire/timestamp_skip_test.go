package tablewire_test

import (
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

	damaged := withField(t, wire, 0xEEEEEEEEEEEEEEEE, 35, payload)
	back = m.New(m.Lookup("GlobalSettings"))
	r = tabletext.Report{}
	_, _ = tablewire.Decode(m, back, damaged, &r)
	if !r.Malformed {
		t.Fatalf("kind 35 should be framing damage, got %+v", r)
	}
}
