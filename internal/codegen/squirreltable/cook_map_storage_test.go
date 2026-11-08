package squirreltable_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestSquirrelCookAuthoredOptionalMap(t *testing.T) {
	slowtest.Gate(t, "native authored optional-map storage")
	dir := filepath.Join("..", "..", "..", "testdata", "conformance", "cook-authored-optional-map")
	source, err := os.ReadFile(filepath.Join(dir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u, m, _ := nfLoadNamed(t, string(source))
	var cases []struct {
		Name    string
		Present bool
		Entries []struct{ Key, Value int64 }
		Refuse  bool
	}
	data, err := os.ReadFile(filepath.Join(dir, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 4 {
		t.Fatalf("authored cases=%d, want 4", len(cases))
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			v := m.New(m.Lookup("Root"))
			f := &v.Fields[0]
			f.Present = c.Present
			var entries strings.Builder
			for _, kv := range c.Entries {
				e := m.NewMapEntry(f.Def)
				e.Fields[0].Cell = tabletext.Cell{I: kv.Key, U: uint64(kv.Key)}
				e.Fields[1].Cell = tabletext.Cell{I: kv.Value, U: uint64(kv.Value)}
				f.Entries = append(f.Entries, tabletext.Cell{Tab: e})
				fmt.Fprintf(&entries, "local e = T.Descriptors[d.entry].make(); e.Key=%d; e.Value=%d; v.Entries.append(e);\n", kv.Key, kv.Value)
			}
			inputs := map[string][]byte{}
			if !c.Refuse {
				for i, big := range []bool{false, true} {
					b, err := tablecook.Cook(m, v, tablecook.Options{Big: big})
					if err != nil {
						t.Fatal(err)
					}
					inputs[fmt.Sprintf("cook%d", i+1)] = b
				}
			}
			pieces := ir.FieldPieces(u, f.Def, ir.RecordLayout(u, u.Tables["Root"]).Fields[0].Offset)
			presence := 64 + pieces[len(pieces)-1].Offset
			code := fmt.Sprintf(`{
local v = T.Root();
local d = T.Descriptors.Root.fields[0];
v.EntriesPresent = %t;
%s
local n = T.CookMeasure("Root", v);
if (%t) {
  local S=schema.require("schema_table_save"), J=schema.require("schema_table_json");
  local empty=T.Root(), a=S.save(T,"Root",v), b=S.save(T,"Root",empty);
  if(a==null || b==null || hex(a,a.len())!=hex(b,b.len())) throw "absent backing changed wire";
  if(J.ToJson(T,"Root",v)!=J.ToJson(T,"Root",empty)) throw "absent backing changed JSON";
  if (n != -1 || T.CookReason != "invalid_value") throw "absent backing measure accepted";
  for (local order=1; order<=2; order++) {
    local b=blob(512); for(local i=0;i<b.len();i++) b[i]=0xa5;
    if (T.Cook("Root",v,b,order) || T.CookReason != "invalid_value") throw "absent backing write accepted";
    for(local i=0;i<b.len();i++) if(b[i] != 0xa5) throw "refusal changed output";
  }
} else {
  for(local order=1;order<=2;order++) {
    local want=I["cook"+order];
    if(n!=want.len()) throw "measure mismatch";
    local b=blob(n);
    if(!T.Cook("Root",v,b,order)) throw "cook refused";
    if(hex(b,n)!=hex(want,n)) throw "cook mismatch";
    if(b[%d]!=%d) throw "presence mismatch";
  }
}
print("authored ok\n");
}`, c.Present, entries.String(), c.Refuse, presence, map[bool]int{false: 0, true: 1}[c.Present])
			rows := []fpRow{{key: "authored", code: code, want: "authored ok"}}
			fpCheck(t, rows, fpRun(t, u, rows, inputs, nil), false)
		})
	}
}

func TestSquirrelCookSignedMapOrderAndReplacement(t *testing.T) {
	slowtest.Gate(t, "native signed cooked map order")
	for _, width := range []int{8, 16, 32, 64} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			u, m, _ := nfLoadNamed(t, fmt.Sprintf("package probe\ntable Root {\n active bool\n if active {\n bias int32 = 7\n }\n entries map[int%d]int32\n}\n", width))
			v := m.New(m.Lookup("Root"))
			// Cook preserves storage under an inactive guard, including its nonzero default.
			f := &v.Fields[2]
			for _, pair := range [][2]int64{{-1, 7}, {1, 11}} {
				e := m.NewMapEntry(f.Def)
				e.Fields[0].Cell = tabletext.Cell{I: pair[0], U: uint64(pair[0])}
				e.Fields[1].Cell = tabletext.Cell{I: pair[1], U: uint64(pair[1])}
				f.Entries = append(f.Entries, tabletext.Cell{Tab: e})
			}
			inputs := map[string][]byte{}
			for i, big := range []bool{false, true} {
				b, err := tablecook.Cook(m, v, tablecook.Options{Big: big})
				if err != nil {
					t.Fatal(err)
				}
				inputs[fmt.Sprintf("cook%d", i+1)] = b
			}
			code := `{
local v=T.Root();
local d=T.Descriptors.Root.fields[2];
foreach(pair in [[1,3],[-1,7],[1,11]]) {
 local e=T.Descriptors[d.entry].make(); e.Key=pair[0];e.Value=pair[1];v.Entries.append(e);
}
for(local order=1;order<=2;order++) {
 local n=T.CookMeasure("Root",v), want=I["cook"+order];
 if(n!=want.len()) throw "signed map measure";
 local b=blob(n);
 if(!T.Cook("Root",v,b,order) || hex(b,n)!=hex(want,n)) throw "signed map bytes";
 if(v.Entries.len()!=3 || v.Entries[0].Key!=1 || v.Entries[0].Value!=3 ||
    v.Entries[1].Key!=-1 || v.Entries[2].Value!=11) throw "cook mutated caller map";
}
print("signed ok\n");
}`
			rows := []fpRow{{key: "signed", code: code, want: "signed ok"}}
			fpCheck(t, rows, fpRun(t, u, rows, inputs, nil), false)
		})
	}
}
