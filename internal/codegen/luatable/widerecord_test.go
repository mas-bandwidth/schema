package luatable

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/memceiling"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// wideRecordSchema is a record past 4 GiB (docs/SPEC-TABLES.md §11): Huge
// holds three 2 GiB Parts and a field past 4 GiB. Every leg generates and
// compiles it with each stored size and offset whole.
const wideRecordSchema = "../../../testdata/wire/tables/storage-cap/wide"

// wideRecordProbe loads the generated module and prints every size and
// offset the descriptors hold for the wide records. It never constructs one.
const wideRecordProbe = `local T = require(%q)
local D = require("descriptor")
for _, name in ipairs({ "Part", "Huge", "Root", "RootIndexEntry" }) do
  local desc = T.Tables[name]
  print(name .. ".size=" .. D.record_size(desc))
  print(name .. ".align=" .. desc.align)
  for _, f in ipairs(desc.fields) do
    print(name .. "." .. f.name .. ".at=" .. f.at)
    if f.shape == "map" then
      print(name .. "." .. f.name .. ".elem=" .. D.record_size(T.Tables[f.entry]))
      print(name .. "." .. f.name .. ".elem_size=" .. f.elem_size)
    elseif f.list then
      local size = D.list_layout(T, f)
      print(name .. "." .. f.name .. ".elem=" .. size)
      print(name .. "." .. f.name .. ".elem_size=" .. f.elem_size)
    end
  end
end
`

// wideRecordWant is what ir answers for every line the probe prints.
func wideRecordWant(u *ir.Unit) map[string]string {
	want := map[string]string{}
	for _, name := range []string{"Part", "Huge", "Root", "RootIndexEntry"} {
		st := u.Tables[name]
		if st == nil {
			st = u.Tables["Root"].Fields[2].MapEntry
		}
		l := ir.RecordLayout(u, st)
		want[name+".size"] = strconv.FormatInt(l.Size, 10)
		want[name+".align"] = strconv.FormatInt(l.Align, 10)
		for _, fl := range l.Fields {
			want[name+"."+fl.Field.Name+".at"] = strconv.FormatInt(fl.Offset, 10)
			switch {
			case fl.Field.IsMap():
				want[name+"."+fl.Field.Name+".elem"] = strconv.FormatInt(ir.RecordLayout(u, fl.Field.MapEntry).Size, 10)
				want[name+"."+fl.Field.Name+".elem_size"] = want[name+"."+fl.Field.Name+".elem"]
			case fl.Field.IsList():
				size, _ := ir.ListElementLayout(u, fl.Field)
				want[name+"."+fl.Field.Name+".elem"] = strconv.FormatInt(size, 10)
				want[name+"."+fl.Field.Name+".elem_size"] = want[name+"."+fl.Field.Name+".elem"]
			}
		}
	}
	return want
}

// wideRecordRun generates the wide unit, applies patch to the table module
// (nil for none), loads it in Lua and answers what disagrees with ir.
func wideRecordRun(t *testing.T, patch func(string) string) []string {
	t.Helper()
	u := luarun.LoadUnit(t, wideRecordSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	module := ModuleName(u)
	// an array default is a loop past arrayInlineMax, so the module stays
	// proportional to the schema rather than to Part's 2^28 elements
	if n := len(files[module+".lua"]); n > 1<<20 {
		t.Fatalf("the generated module is %d bytes; an array default was written out element by element", n)
	}
	if patch != nil {
		files[module+".lua"] = []byte(patch(string(files[module+".lua"])))
	}
	stdout, stderr, err := try(t, files, fmt.Sprintf(wideRecordProbe, module), nil)
	if err != nil {
		t.Fatalf("probe: %v\n%s\n%s", err, stdout, stderr)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(stdout, "\n"), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("probe line %q is not key=value", line)
		}
		got[k] = v
	}
	var bad []string
	want := wideRecordWant(u)
	for k, w := range want {
		if got[k] != w {
			bad = append(bad, fmt.Sprintf("%s: got %q, want %s", k, got[k], w))
		}
	}
	if huge, _ := strconv.ParseInt(want["Huge.size"], 10, 64); huge <= 1<<32 {
		bad = append(bad, "Huge is not past 4 GiB, so the unit holds nothing wide")
	}
	return bad
}

// TestLuaWideRecordCompiles: the Lua leg generates a record past 4 GiB, the
// generated module loads, and every record size, field offset, list element
// size and map entry size it holds equals ir's, past 2^32 where ir's is.
func TestLuaWideRecordCompiles(t *testing.T) {
	memceiling.Guard(t, memceiling.Default)
	t.Parallel()
	if bad := wideRecordRun(t, nil); len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
}

// TestLuaWideRecordNegativeControl narrows Huge's size to 32 bits in the
// generated module and requires the probe to go red.
func TestLuaWideRecordNegativeControl(t *testing.T) {
	memceiling.Guard(t, memceiling.Default)
	t.Parallel()
	u := luarun.LoadUnit(t, filepath.Clean(wideRecordSchema))
	size := ir.RecordLayout(u, u.Tables["Huge"]).Size
	from := fmt.Sprintf("size = %d,", size)
	bad := wideRecordRun(t, func(src string) string {
		if !strings.Contains(src, from) {
			t.Fatalf("the generated module holds no %q", from)
		}
		return strings.Replace(src, from, fmt.Sprintf("size = %d,", size&0xffffffff), 1)
	})
	if len(bad) == 0 {
		t.Fatal("a Huge size narrowed to 32 bits still matched ir")
	}
}
