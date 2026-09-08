package tablewire_test

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"os"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Rowan's #725 review: cooking the decoded value directly must agree with
// cooking its text round trip, which drops slots above the live count.
func TestRepeatedCountedUnionTailCooks(t *testing.T) {
	source, err := os.ReadFile("../../tables/arms/Carry.schema")
	if err != nil {
		t.Fatal(err)
	}
	m := listModel(t, string(source))
	wire, err := hex.DecodeString("01010e1e0f0202040401000000030d12040e0e040305000000060000000700000000010e1e0f0202047f01000000030d12040e0e0403050000000600000007000000000504020000000053a245082ca7b2c507b252164e194dfdd50802a2ad84ad246f2c414fbf84783ee9ea716f0f0182bf0500000000000000")
	if err != nil {
		t.Fatal(err)
	}
	for _, decode := range []struct {
		name string
		fn   func(*tabletext.Model, *tabletext.Instance, []byte, *tabletext.Report) (bool, error)
	}{
		{"ordinary", tablewire.Decode}, {"region", tablewire.DecodeRegion},
		{"retaining", func(m *tabletext.Model, inst *tabletext.Instance, b []byte, r *tabletext.Report) (bool, error) {
			var retained tablewire.Retain
			return tablewire.DecodeRetain(m, inst, b, &retained, r)
		}},
	} {
		t.Run(decode.name, func(t *testing.T) {
			inst := m.New(m.Lookup("Hand"))
			var report tabletext.Report
			if ok, err := decode.fn(m, inst, wire, &report); !ok || err != nil || !report.Malformed {
				t.Fatalf("decode: %v, %+v", err, report)
			}
			entries := fieldByName(t, inst, "entries")
			if entries.Count != 0 {
				t.Fatalf("count %d", entries.Count)
			}
			for _, cell := range entries.Elems {
				if cell.U != 0 || cell.Tab != nil {
					t.Fatal("stale counted union tail")
				}
			}
			want := place(t, m, "Hand", `{"after":2}`)
			for _, big := range []bool{false, true} {
				got, err := tablecook.Cook(m, inst, tablecook.Options{Big: big})
				if err != nil {
					t.Fatal(err)
				}
				expected, err := tablecook.Cook(m, want, tablecook.Options{Big: big})
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, expected) {
					t.Fatalf("direct cook differs from clean value, big=%v", big)
				}
			}
		})
	}
}

func TestRepeatedCountedRecordTailDefaults(t *testing.T) {
	m := listModel(t, `package probe
 type Child { n int32 = 7 }
 table Root { values [..3]Child }
 `)
	before := place(t, m, "Root", `{"values":[{"n":1},{"n":2},{"n":3}]}`)
	wire, err := tablewire.Encode(m, before)
	if err != nil {
		t.Fatal(err)
	}
	body, ids := trailerOf(t, wire)
	ref := uint64(0)
	for i, id := range ids {
		if id == ir.TableWireId("values") {
			ref = uint64(i + 1)
		}
	}
	if ref == 0 || body[len(body)-1] != 0 {
		t.Fatal("source framing")
	}
	for _, tc := range []struct {
		name      string
		payload   []byte
		count     int
		malformed bool
	}{
		{"empty", []byte{13, 0}, 0, false},
		{"short", []byte{13, 1, 1, 0}, 1, false},
		{"damaged", []byte{13, 2, 1, 0}, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := append([]byte{ir.TableWireForm}, body[:len(body)-1]...)
			data = append(data, leb(ref)...)
			data = append(data, 14)
			data = append(data, leb(uint64(len(tc.payload)))...)
			data = append(data, tc.payload...)
			data = append(data, 0)
			for _, id := range ids {
				data = binary.LittleEndian.AppendUint64(data, id)
			}
			data = binary.LittleEndian.AppendUint64(data, uint64(len(ids)))
			inst := m.New(m.Lookup("Root"))
			var report tabletext.Report
			if ok, err := tablewire.Decode(m, inst, data, &report); !ok || err != nil || report.Malformed != tc.malformed {
				t.Fatalf("decode: %v, %+v", err, report)
			}
			values := fieldByName(t, inst, "values")
			if values.Count != tc.count {
				t.Fatalf("count %d", values.Count)
			}
			for i, cell := range values.Elems {
				if got := fieldByName(t, cell.Tab, "n").Cell.I; got != 7 {
					t.Fatalf("slot %d default %d, want 7", i, got)
				}
			}
		})
	}
}
