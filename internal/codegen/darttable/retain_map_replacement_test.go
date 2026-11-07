package darttable

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
	"maps"
	"strings"
	"testing"
)

func TestDartRetainedMapReplacement(t *testing.T) {
	t.Skip("layer 4 (dart): leg tolerates what the strict read refuses, re-pin as a verdict: docs/plans/strict-only.md")
	slowtest.Gate(t, "dart")
	u := unitFrom(t, `package probe
table Node { known int32 }
table Value { known int32
 peer *Node }
table Root { entries map[int32]Value }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, code := range files {
		if strings.HasSuffix(name, "Wire.dart") {
			files[name] = append(code, []byte("\nint retainedMapEntriesForTest(TableRetain r) => r._maps.values.fold(0, (n, fields) => n + fields.values.fold(0, (m, entries) => m + entries.length));\n")...)
		}
	}
	maps.Copy(files, packetFiles(t, u))
	m := tabletext.NewModel(u)
	entry := func(unknown, pointer bool) []byte {
		out := []byte{2, 4, 1, 0, 0, 0}
		if unknown {
			out = append(out, 3, 8, 7, 0, 0, 0)
		}
		value := []byte{}
		if unknown {
			value = append(value, 5, 8, 9, 0, 0, 0)
		}
		if pointer {
			value = append(value, 6, 17, 2)
		}
		value = append(value, 0)
		out = append(out, 4, 13, byte(len(value)))
		out = append(out, value...)
		return append(out, 0)
	}
	field := func(entries ...[]byte) []byte {
		payload := binary.AppendUvarint([]byte{13}, uint64(len(entries)))
		for _, e := range entries {
			payload = binary.AppendUvarint(payload, uint64(len(e)))
			payload = append(payload, e...)
		}
		return append(binary.AppendUvarint([]byte{1, 14}, uint64(len(payload))), payload...)
	}
	first := entry(true, false)
	last := entry(false, false)
	const knownEntries = 2048
	known := make([][]byte, knownEntries)
	for i := range known {
		known[i] = entry(false, false)
		binary.LittleEndian.PutUint32(known[i][2:6], uint32(i))
	}
	var b strings.Builder
	b.WriteString(dartClosurePrelude)
	for _, tc := range []struct {
		name                               string
		body                               []byte
		unknown, duplicate, mismatch, lost int
	}{
		{"known_only_large", field(known...), 0, 0, 0, 0},
		{"duplicate", field(first, last), 2, 1, 0, 0},
		{"replace_empty", append(field(first), field()...), 2, 0, 0, 0},
		{"inert_repeat", append(field(first), 1, 14, 2, 8, 0), 2, 0, 1, 0},
		{"cleared_then_replaced", append(field(first, []byte{2, 12, 1, 'x', 0}), field()...), 2, 0, 1, 0},
		{"discarded_body_keeps_pointer_node", append(field(entry(true, true), last), 7, 12, 10, 1, 8, 7, 10, 8, 11, 0, 0, 0, 0), 3, 1, 0, 1},
	} {
		wire := append([]byte{1}, tc.body...)
		wire = append(wire, 0)
		ids := []uint64{ir.TableWireId("entries"), ir.MapKeyWireId, ir.TableWireId("futureEntry"), ir.MapValueWireId, ir.TableWireId("futureValue"), ir.TableWireId("peer"), ir.TableNodeWireId, ir.TableWireId("Node"), ir.TableWireId("known"), ir.TableWireId("futureNode")}
		for _, id := range ids {
			wire = binary.LittleEndian.AppendUint64(wire, id)
		}
		wire = binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
		v := m.New(u.Tables["Root"])
		var r tabletext.Report
		store := &tablewire.Retain{Capacity: 65536, IdCapacity: 1024}
		if ok, err := tablewire.DecodeRetain(m, v, wire, store, &r); !ok || err != nil || r.Unknown != tc.unknown || r.Retained != tc.unknown || r.RetainLost != 0 || tc.duplicate != 0 || tc.mismatch != 0 {
			t.Fatalf("%s neutral read %+v %v", tc.name, r, err)
		}
		var sr tabletext.Report
		saved, err := tablewire.EncodeRetain(m, v, store, &sr)
		if err != nil || sr.RetainLost != tc.lost {
			t.Fatalf("%s neutral save %+v %v", tc.name, sr, err)
		}
		fmt.Fprintf(&b, `{final data=bytes(%q);final store=w.TableRetain(65536,List.filled(1024,0));final r=w.TableWireReport();final v=w.rootWireLoadRetain(data,store,r);
 check(!r.malformed&&!r.refused&&r.unknown==%d&&r.retained==%d&&r.retainLost==0&&r.duplicate==%d&&r.kindMismatch==%d,'map replacement read %s');
 check(w.retainedMapEntriesForTest(store)<=r.retained,'retained map metadata bound');
 if(r.retained==0){final empty=w.TableRetain(0,List.filled(0,0));final er=w.TableWireReport();w.rootWireLoadRetain(data,empty,er);check(!er.malformed&&!er.refused&&er.retained==0&&w.retainedMapEntriesForTest(empty)==0,'zero-capacity map metadata');}
 for(var repeat=0;repeat<3;repeat++){
 final sr=w.TableWireReport();final saved=w.rootWireSaveRetain(v,store,sr);
 check(sr.retainLost==%d&&hex(saved)==%q,'map replacement save %s');
 }}
`, hex.EncodeToString(wire), tc.unknown, tc.unknown, tc.duplicate, tc.mismatch, tc.name, tc.lost, hex.EncodeToString(saved), tc.name)
	}
	b.WriteString("}\n")
	runFormsProbe(t, files, b.String())
}
