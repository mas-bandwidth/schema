package tablewire

import (
	"encoding/binary"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// nestedUnionFile is a form-1 file whose root body carries one unknown union
// field: `unions` unions, each holding the next as its arm (kind 15), around
// an innermost payload. inner is the innermost union's own content: [0] for
// None, or an arm reference, kind and framed payload.
func nestedUnionFile(unions int, inner []byte) []byte {
	payload := inner
	for range unions {
		next := []byte{1, byte(ir.TableKindUnion)}
		next = binary.AppendUvarint(next, uint64(len(payload)))
		payload = append(next, payload...)
	}
	out := []byte{ir.TableWireForm, 1, byte(ir.TableKindUnion)}
	out = append(out, payload...)
	out = append(out, 0)
	out = binary.LittleEndian.AppendUint64(out, 0x1234567890abcdef)
	return binary.LittleEndian.AppendUint64(out, 1)
}

// TestBinaryDepthCountsStructuralUnionArms holds §4's bound through unions: a
// union whose arm is structural (a table, an array, a keyed or map body, or
// another union) is one level, and a union whose arm is a scalar or None is
// none. 128 unions around None read and 129 refuse; 127 unions around a
// scalar arm at level 128 read; and a table arm under a union at level 128 is
// level 129 and refuses. Every engine read answers the same.
func TestBinaryDepthCountsStructuralUnionArms(t *testing.T) {
	t.Parallel()
	m := covModel(t, depthProbeUnit)
	root := m.Lookup("Probe")
	scalarArm := []byte{1, byte(ir.TableKindI32), 4, 7, 0, 0, 0}
	tableArm := []byte{1, byte(ir.TableKindTable), 1, 0}
	cases := []struct {
		name string
		file []byte
		want bool
	}{
		{"128 unions around None", nestedUnionFile(ir.TableBinaryMaxDepth, []byte{0}), true},
		{"129 unions around None", nestedUnionFile(ir.TableBinaryMaxDepth+1, []byte{0}), false},
		{"a scalar arm under 128 unions", nestedUnionFile(ir.TableBinaryMaxDepth, scalarArm), true},
		{"a table arm under 127 unions (the union 128, its body 129)", nestedUnionFile(ir.TableBinaryMaxDepth-1, tableArm), false},
		{"a table arm under 126 unions (its body 128)", nestedUnionFile(ir.TableBinaryMaxDepth-2, tableArm), true},
	}
	for _, c := range cases {
		reads := map[string]func(*tabletext.Report) (bool, error){
			"Decode":       func(r *tabletext.Report) (bool, error) { return Decode(m, m.New(root), c.file, r) },
			"DecodeRegion": func(r *tabletext.Report) (bool, error) { return DecodeRegion(m, m.New(root), c.file, r) },
			"DecodeRetain": func(r *tabletext.Report) (bool, error) {
				return DecodeRetain(m, m.New(root), c.file, &Retain{Capacity: 1 << 16, IdCapacity: 64}, r)
			},
		}
		for name, read := range reads {
			var r tabletext.Report
			ok, err := read(&r)
			if ok != c.want {
				t.Errorf("%s: %s: ok=%v err=%v %+v, want ok=%v", name, c.name, ok, err, r, c.want)
			}
		}
		// an accepted retaining load writes the union back byte for byte
		if c.want {
			inst := m.New(root)
			store := &Retain{Capacity: 1 << 16, IdCapacity: 64}
			var r tabletext.Report
			if ok, _ := DecodeRetain(m, inst, c.file, store, &r); !ok || r.Retained != 1 {
				t.Errorf("%s: the retaining load kept %d records: %+v", c.name, r.Retained, r)
				continue
			}
			var w tabletext.Report
			out, err := EncodeRetain(m, inst, store, &w)
			if err != nil || string(out) != string(c.file) {
				t.Errorf("%s: the retained write-back differs: %v\n got %x\nwant %x", c.name, err, out, c.file)
			}
		}
	}
}
