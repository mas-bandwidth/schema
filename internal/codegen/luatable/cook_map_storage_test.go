package luatable

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestLuaCookAuthoredOptionalMap(t *testing.T) {
	slowtest.Gate(t, "native authored optional-map storage")
	dir := filepath.Join("..", "..", "..", "testdata", "conformance", "cook-authored-optional-map")
	u := luarun.LoadUnit(t, filepath.Join(dir, "Probe.schema"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
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
			m := tabletext.NewModel(u)
			v := m.New(m.Lookup("Root"))
			f := &v.Fields[0]
			f.Present = c.Present
			var entries strings.Builder
			for _, kv := range c.Entries {
				e := m.NewMapEntry(f.Def)
				e.Fields[0].Cell = tabletext.Cell{I: kv.Key, U: uint64(kv.Key)}
				e.Fields[1].Cell = tabletext.Cell{I: kv.Value, U: uint64(kv.Value)}
				f.Entries = append(f.Entries, tabletext.Cell{Tab: e})
				fmt.Fprintf(&entries, "v.entries[%d] = %d\n", kv.Key, kv.Value)
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
			probe := fmt.Sprintf(`local T=require(%q)
local C=require("cookwrite")
local S=require("save")
local J=require("json")
local inputs=require("inputs")
local empty=T.NewRoot()
local v=T.NewRoot()
v.entries_present=%t
%s
if %t then
  assert(S.Save(T,"Root",v)==S.Save(T,"Root",empty))
  assert(J.ToJson(T,"Root",v)==J.ToJson(T,"Root",empty))
  local n,why=C.CookMeasure(T,"Root",v)
  assert(n==-1 and why:match("^invalid_value"))
  for order=1,2 do
    local bytes,reason=C.Cook(T,"Root",v,nil,order)
    assert(bytes==nil and reason:match("^invalid_value"))
  end
else
  for order=1,2 do
    local bytes=assert(C.Cook(T,"Root",v,nil,order))
    assert(bytes==inputs["cook"..order])
    assert(C.CookMeasure(T,"Root",v)==#bytes)
    assert(bytes:byte(%d)==%d)
  end
end
print("authored optional-map state passed")
`, ModuleName(u), c.Present, entries.String(), c.Refuse, presence+1, map[bool]int{false: 0, true: 1}[c.Present])
			out := declaredFormsRun(t, files, probe, inputs)
			if !strings.Contains(string(out), "authored optional-map state passed") {
				t.Fatalf("%s", out)
			}
		})
	}
}

func TestLuaCookSignedMapKeyOrder(t *testing.T) {
	slowtest.Gate(t, "native signed cooked map key order")
	for _, width := range []int{8, 16, 32, 64} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			u, _ := declaredFormsUnit(t, fmt.Sprintf("package probe\ntable Root { entries map[int%d]int32 }\n", width))
			m := tabletext.NewModel(u)
			v := m.New(m.Lookup("Root"))
			f := &v.Fields[0]
			for _, key := range []int64{-1, 1} {
				e := m.NewMapEntry(f.Def)
				e.Fields[0].Cell = tabletext.Cell{I: key, U: uint64(key)}
				e.Fields[1].Cell = tabletext.Cell{I: 7, U: 7}
				f.Entries = append(f.Entries, tabletext.Cell{Tab: e})
			}
			good, err := tablecook.Cook(m, v, tablecook.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if got := runCookProbe(t, u, "Root", good); got != "open" {
				t.Fatalf("valid signed order: %s", got)
			}
			slot := 64 + ir.RecordLayout(u, u.Tables["Root"]).Fields[0].Offset
			start := slot + int64(binary.LittleEndian.Uint64(good[slot:]))
			stride := ir.RecordLayout(u, f.Def.MapEntry).Size
			swapped := append([]byte(nil), good...)
			copy(swapped[start:start+stride], good[start+stride:start+2*stride])
			copy(swapped[start+stride:start+2*stride], good[start:start+stride])
			if got := runCookProbe(t, u, "Root", swapped); got != "bad_layout" {
				t.Fatalf("descending signed keys accepted: %s", got)
			}
			duplicate := append([]byte(nil), good...)
			copy(duplicate[start+stride:start+2*stride], good[start:start+stride])
			if got := runCookProbe(t, u, "Root", duplicate); got != "bad_layout" {
				t.Fatalf("duplicate signed keys accepted: %s", got)
			}
		})
	}
}
