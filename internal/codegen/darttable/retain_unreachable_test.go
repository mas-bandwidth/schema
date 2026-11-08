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

func TestDartRetainedUnreachableRecords(t *testing.T) {
	t.Skip("layer 4 (dart): leg tolerates what the strict read refuses, re-pin as a verdict: docs/plans/strict-only.md")
	slowtest.Gate(t, "dart")
	u := unitFrom(t, `package probe
table Child { value int32 }
table Root { child *Child }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	m := tabletext.NewModel(u)
	var b strings.Builder
	b.WriteString(dartClosurePrelude)
	for _, reachable := range []bool{false, true} {
		index := byte(0)
		lost := 1
		if reachable {
			index = 2
			lost = 0
		}
		wire := []byte{1, 1, 17, index, 2, 12, 10, 1, 3, 7, 4, 8, 7, 0, 0, 0, 0, 0}
		for _, id := range []uint64{ir.TableWireId("child"), ir.TableNodeWireId, ir.TableWireId("Child"), ir.TableWireId("future"), 4} {
			wire = binary.LittleEndian.AppendUint64(wire, id)
		}
		v := m.New(u.Tables["Root"])
		var r tabletext.Report
		store := &tablewire.Retain{Capacity: 65536, IdCapacity: 1024}
		if ok, err := tablewire.DecodeRetain(m, v, wire, store, &r); !ok || err != nil || r.Unknown != 1 || r.Retained != 1 || r.RetainLost != 0 {
			t.Fatalf("neutral read %+v %v", r, err)
		}
		var sr tabletext.Report
		saved, err := tablewire.EncodeRetain(m, v, store, &sr)
		if err != nil || sr.RetainLost != lost {
			t.Fatalf("neutral save %+v %v", sr, err)
		}
		fmt.Fprintf(&b, `{final data=bytes(%q);final store=w.TableRetain(65536,List.filled(1024,0));final r=w.TableWireReport();
 final v=w.rootWireLoadRetain(data,store,r);
 check(!r.malformed&&!r.refused&&r.unknown==1&&r.retained==1&&r.retainLost==0,'retained node read');
 for(var repeat=0;repeat<3;repeat++){
 final sr=w.TableWireReport();final saved=w.rootWireSaveRetain(v,store,sr);
 check(sr.retainLost==%d&&hex(saved)==%q,'unreachable retained node loss');
 check(w.rootWireMeasureRetain(v,store)==saved.length,'repeat retained measure');
 }
`, hex.EncodeToString(wire), lost, hex.EncodeToString(saved))
		if reachable {
			v.Fields[0].Cell.Node = nil
			sr = tabletext.Report{}
			saved, err = tablewire.EncodeRetain(m, v, store, &sr)
			if err != nil || sr.RetainLost != 1 {
				t.Fatalf("neutral detached save %+v %v", sr, err)
			}
			fmt.Fprintf(&b, `v.child=null;final sr=w.TableWireReport();final saved=w.rootWireSaveRetain(v,store,sr);
 check(sr.retainLost==1&&hex(saved)==%q,'authored detached node loss');
`, hex.EncodeToString(saved))
		}
		b.WriteString("}\n")
	}
	b.WriteString("}\n")
	runFormsProbe(t, files, b.String())
}
