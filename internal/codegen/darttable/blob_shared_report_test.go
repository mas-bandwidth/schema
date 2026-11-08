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

func TestDartBlobSharedReportCensus(t *testing.T) {
	slowtest.Gate(t, "dart")
	u := unitFrom(t, `package probe
table Root { a *string
 b *string
 c *bytes
 d *bytes }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	m := tabletext.NewModel(u)
	var b strings.Builder
	b.WriteString(dartClosurePrelude)
	for _, tc := range []struct {
		name            string
		refs            []byte
		distinct, empty bool
		shared, km      int
	}{
		{"shared_empty", []byte{2, 2, 3, 3}, false, true, 2, 0},
		{"shared_equal", []byte{2, 2, 3, 3}, false, false, 2, 0},
		{"distinct_empty", []byte{2, 3, 4, 5}, true, true, 0, 0},
		{"distinct_equal", []byte{2, 3, 4, 5}, true, false, 0, 0},
		{"null_not_named", []byte{0, 2, 3, 3}, false, true, 1, 0},
		{"wrong_string_kind", []byte{3, 2, 3, 3}, false, false, 1, 1},
		{"wrong_bytes_kind", []byte{2, 2, 2, 3}, false, false, 1, 1},
	} {
		wire := []byte{1}
		for i, index := range tc.refs {
			wire = append(wire, byte(i+1), 17, index)
		}
		textData, byteData := []byte("same"), []byte{1, 2}
		if tc.empty {
			textData = nil
			byteData = nil
		}
		types := []byte{6, 7}
		datas := [][]byte{textData, byteData}
		if tc.distinct {
			types = []byte{6, 6, 7, 7}
			datas = [][]byte{textData, textData, byteData, byteData}
		}
		payload := []byte{byte(len(types))}
		for i, typ := range types {
			payload = append(payload, typ, byte(len(datas[i])))
			payload = append(payload, datas[i]...)
		}
		wire = append(wire, 5, 12, byte(len(payload)))
		wire = append(wire, payload...)
		wire = append(wire, 0)
		ids := []uint64{ir.TableWireId("a"), ir.TableWireId("b"), ir.TableWireId("c"), ir.TableWireId("d"), ir.TableNodeWireId, ir.StringWireTypeId, ir.BytesWireTypeId}
		for _, id := range ids {
			wire = binary.LittleEndian.AppendUint64(wire, id)
		}
		wire = binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
		v := m.New(u.Tables["Root"])
		// STRICT VERDICT (docs/plans/strict-only.md decision 6): a shared
		// blob record or a wrong record kind refuses the whole load; only a
		// distinct-record wire reads.
		expectRefused := tc.shared > 0 || tc.km > 0
		var r tabletext.Report
		ok, derr := tablewire.Decode(m, v, wire, &r)
		if expectRefused {
			if ok || !r.Refused {
				t.Fatalf("%s oracle must refuse: ok=%v err=%v report %+v", tc.name, ok, derr, r)
			}
		} else if !ok || derr != nil || r.Refused {
			t.Fatalf("%s oracle report %+v %v", tc.name, r, derr)
		}
		// The writer's own blob_shared refusal is exercised on a hand-built
		// shared value by the writer tests; a refused load leaves defaults,
		// which encode cleanly, so only the distinct case round-trips here.
		saved, err := tablewire.Encode(m, v)
		if !expectRefused && err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, `for(final retaining in [false,true]){
 final data=bytes(%q);final store=w.TableRetain(65536,List.filled(1024,0));final r=w.TableWireReport();
 for(var repeat=1;repeat<=2;repeat++){
 final v=retaining?w.rootWireLoadRetain(data,store,r):w.rootWireLoad(data,r);
 check(r.refused==%s,'shared blob verdict %s refused=${r.refused} reason=${r.reason}');
`, hex.EncodeToString(wire), map[bool]string{true: "true", false: "false"}[expectRefused], tc.name)
		if !expectRefused {
			fmt.Fprintf(&b, "check(hex(w.rootWireSave(v))==%q,'distinct blobs stay distinct');\n", hex.EncodeToString(saved))
		}
		b.WriteString("}}\n")
	}
	b.WriteString("}\n")
	runFormsProbe(t, files, b.String())
}
