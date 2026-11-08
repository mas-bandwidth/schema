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

func TestDartRetainedUnknownEnumLists(t *testing.T) {
	slowtest.Gate(t, "dart")
	u := unitFrom(t, `package probe
enum Mode { Good }
table Child { value int32 }
table Bounded { values [..3]Mode }
table Open { values []Mode }
table Fixed { values [3]Mode
 child *Child }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	m := tabletext.NewModel(u)
	var b strings.Builder
	b.WriteString(dartClosurePrelude)
	for _, root := range []string{"Bounded", "Open", "Fixed"} {
		for _, tc := range []struct {
			refs []byte
			lost int
		}{{[]byte{2, 3, 0}, 1}, {[]byte{3, 3, 2}, 2}, {[]byte{2, 0, 2}, 0}} {
			wire := []byte{1, 1, 14, 5, 30, 3}
			wire = append(wire, tc.refs...)
			wire = append(wire, 0)
			for _, id := range []uint64{ir.TableWireId("values"), ir.TableWireId(u.Enums["Mode"].VariantWireName(0)), ir.TableWireId("Future"), 3} {
				wire = binary.LittleEndian.AppendUint64(wire, id)
			}
			v := m.New(u.Tables[root])
			var r tabletext.Report
			store := &tablewire.Retain{Capacity: 65536, IdCapacity: 1024}
			if ok, err := tablewire.DecodeRetain(m, v, wire, store, &r); !ok || err != nil || r.Unknown != tc.lost || r.RetainLost != tc.lost || r.Retained != 0 {
				t.Fatalf("%s neutral %+v %v", root, r, err)
			}
			saved, err := tablewire.EncodeRetain(m, v, store, &tabletext.Report{})
			if err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&b, `for(final retaining in [false,true]){
 final data=bytes(%q);final store=w.TableRetain(65536,List.filled(1024,0));final r=w.TableWireReport();
 final v=retaining?w.%sWireLoadRetain(data,store,r):w.%sWireLoad(data,r);
 check(!r.refused&&r.unknown==%d&&r.retained==0&&r.retainLost==(retaining?%d:0),'enum list report %s');
 final saved=retaining?w.%sWireSaveRetain(v,store,w.TableWireReport()):w.%sWireSave(v);
 check(hex(saved)==%q,'enum list values %s');
 }
`, hex.EncodeToString(wire), dartName(root), dartName(root), tc.lost, tc.lost, root, dartName(root), dartName(root), hex.EncodeToString(saved), root)
		}
	}
	b.WriteString("}\n")
	runFormsProbe(t, files, b.String())
}
