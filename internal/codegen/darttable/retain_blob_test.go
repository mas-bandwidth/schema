package darttable

import (
	"encoding/hex"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"maps"
	"strings"
	"testing"
)

func TestDartRetainingBlobReachability(t *testing.T) {
	slowtest.Gate(t, "dart")
	u := unitFrom(t, `package probe
table Child { data *bytes
 caption *string }
table BytesOnly { data *bytes }
table StringOnly { caption *string }
table Nested { child *Child }
table Mixed { data *bytes
 caption *string
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
	for _, tc := range []struct{ root, text string }{
		{"BytesOnly", `{"data":"AQID/w=="}`},
		{"StringOnly", `{"caption":"cap"}`},
		{"Nested", `{"child":{"data":"AQID/w==","caption":"cap"}}`},
		{"Mixed", `{"data":"3q2+738=","caption":"root","child":{"data":"AQID/w==","caption":"cap"}}`},
		{"BytesOnly", `{"data":""}`},
		{"StringOnly", `{"caption":""}`},
		{"Mixed", `{"data":null,"caption":null,"child":null}`},
	} {
		v := m.New(u.Tables[tc.root])
		var rep tabletext.Report
		if !m.Read(v, []byte(tc.text), &rep) || rep.Refused {
			t.Fatalf("JSON %+v", rep)
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatal(err)
		}
		store := &tablewire.Retain{Capacity: 65536, IdCapacity: 1024}
		back := m.New(u.Tables[tc.root])
		rep = tabletext.Report{}
		if ok, err := tablewire.DecodeRetain(m, back, wire, store, &rep); !ok || err != nil || rep.Unknown != 0 || rep.RetainLost != 0 {
			t.Fatalf("neutral retained blob %+v %v", rep, err)
		}
		saved, err := tablewire.EncodeRetain(m, back, store, &tabletext.Report{})
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, `{final wire=bytes(%q);final store=w.TableRetain(65536,List.filled(1024,0));final r=w.TableWireReport();
 final v=w.%sWireLoadRetain(wire,store,r);
 check(!r.refused&&r.unknown==0&&r.retained==0&&r.retainLost==0,'known blob report %s');
 check(hex(w.%sWireSaveRetain(v,store,r))==%q,'known blob bytes %s');
 }
`, hex.EncodeToString(wire), dartName(tc.root), tc.root, dartName(tc.root), hex.EncodeToString(saved), tc.root)
	}
	b.WriteString("}\n")
	runFormsProbe(t, files, b.String())
}
