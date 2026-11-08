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

func TestDartRetainedUnionArmFrames(t *testing.T) {
	slowtest.Gate(t, "dart")
	u := unitFrom(t, `package probe
table Root { known uint32
 values []int32 }
`)
	m := tabletext.NewModel(u)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	var b strings.Builder
	b.WriteString(dartClosurePrelude)
	for _, tc := range []struct {
		name    string
		kind    byte
		payload []byte
		lost    int
	}{
		{"scalar", 8, []byte{0x78, 0x56, 0x34, 0x12}, 0},
		{"text", 12, []byte("frame text"), 0},
		{"empty_text", 12, nil, 0},
		{"enum", 30, []byte{4}, 0},
		{"none_enum", 30, []byte{0}, 0},
		{"nested_union", 15, []byte{3, 8, 4, 0x78, 0x56, 0x34, 0x12}, 0},
		{"table", 13, []byte{4, 8, 1, 0, 0, 0, 0}, 0},
		{"array", 14, []byte{8, 2, 1, 0, 0, 0, 2, 0, 0, 0}, 0},
		{"keyed", 16, []byte{8, 1, 4, 4, 1, 0, 0, 0}, 0},
		{"pointer_excluded", 17, []byte{0}, 1},
	} {
		wire := []byte{1, 1, 8, 7, 0, 0, 0, 2, 15, 3, tc.kind, byte(len(tc.payload))}
		wire = append(wire, tc.payload...)
		wire = append(wire, 0)
		for _, id := range []uint64{ir.TableWireId("known"), ir.TableWireId("future"), ir.TableWireId("arm"), ir.TableWireId("key"), 4} {
			wire = binary.LittleEndian.AppendUint64(wire, id)
		}
		v := m.New(u.Tables["Root"])
		store := &tablewire.Retain{Capacity: 65536, IdCapacity: 1024}
		var r tabletext.Report
		if ok, err := tablewire.DecodeRetain(m, v, wire, store, &r); !ok || err != nil || r.Refused || r.Retained != 1-tc.lost || r.RetainLost != tc.lost {
			t.Fatalf("%s neutral retain %+v %v", tc.name, r, err)
		}
		expected, err := tablewire.EncodeRetain(m, v, store, &tabletext.Report{})
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, `{final data=bytes(%q);final store=w.TableRetain(65536,List.filled(1024,0));final r=w.TableWireReport();final v=w.rootWireLoadRetain(data,store,r);
check(v.known==7&&!r.refused&&r.unknown==1&&r.retained==%d&&r.retainLost==%d,'framed retain %s');
final saved=w.rootWireSaveRetain(v,store,w.TableWireReport());
check(hex(saved)==%q,'independent framed resave %s');
}
`, hex.EncodeToString(wire), 1-tc.lost, tc.lost, tc.name, hex.EncodeToString(expected), tc.name)
	}
	b.WriteString("}\n")
	runFormsProbe(t, files, b.String())
}
