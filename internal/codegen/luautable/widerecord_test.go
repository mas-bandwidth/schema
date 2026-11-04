package luautable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/memceiling"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// wideRecordDir holds a record past 4 GiB (docs/SPEC-TABLES.md §11): Huge is
// three 2 GiB Parts and a field past 4 GiB. Every leg generates and loads it
// with each stored size whole.
var wideRecordDir = filepath.Join("..", "..", "..", "testdata", "wire", "tables", "storage-cap", "wide")

// wideRecordProbe prints every size the descriptors hold for the wide records
// (the record's storage and alignment, and each list or map term's element
// size and alignment), as integers. It never constructs a record.
const wideRecordProbe = `local S = require("./schema_tables")
local T = require("./MODULE")
local function n(x: any): string
	if x == nil then return "-" end
	return string.format("%.0f", x)
end
for _, name in { NAMES } do
	local d = S.lookup(name)
	print(name .. ".size=" .. n(d.size))
	print(name .. ".align=" .. n(d.align))
	for _, f in d.fields do
		if f.es ~= nil then
			print(name .. "." .. tostring(f.name) .. ".es=" .. n(f.es))
			print(name .. "." .. tostring(f.name) .. ".ea=" .. n(f.ea))
		end
	end
end
`

// wideRecordUnit loads Wide.schema as the compiler does, and answers its
// source for the generator.
func wideRecordUnit(t *testing.T) (*ir.Unit, string) {
	t.Helper()
	path := filepath.Join(wideRecordDir, "Wide.schema")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	return u, string(src)
}

// wideRecordRun generates the wide unit, applies patch to every generated
// file (nil for none), loads it in Luau and answers every size that
// disagrees with ir.
func wideRecordRun(t *testing.T, patch func(string) string) []string {
	t.Helper()
	u, src := wideRecordUnit(t)
	g := generateFiles(t, map[string]string{"Wide.schema": src})
	module := ""
	for name, data := range g.files {
		// the module stays proportional to the schema, not to Part's 2^28
		// elements
		if len(data) > 1<<20 && !strings.HasPrefix(name, "schema_") {
			t.Fatalf("%s is %d bytes; an array default was written out element by element", name, len(data))
		}
		if strings.HasSuffix(name, "Table.luau") && strings.Contains(string(data), "_T.RootDescriptor = ") {
			module = strings.TrimSuffix(name, ".luau")
		}
	}
	if module == "" {
		t.Fatal("no emitted module declares RootDescriptor")
	}
	entry := u.Tables["Root"].Fields[2].MapEntry
	want := map[string]string{}
	var names []string
	for _, st := range []*ir.Struct{u.Tables["Part"], u.Tables["Huge"], u.Tables["Root"], entry} {
		names = append(names, strconv.Quote(st.Name))
		l := ir.RecordLayout(u, st)
		want[st.Name+".size"] = strconv.FormatInt(l.Size, 10)
		want[st.Name+".align"] = strconv.FormatInt(l.Align, 10)
		for _, f := range st.Fields {
			var size, align int64
			switch {
			case f.IsMap():
				el := ir.RecordLayout(u, f.MapEntry)
				size, align = el.Size, el.Align
			case f.Array == ir.ArrayList:
				size, align = ir.ListElementLayout(u, f)
			default:
				continue
			}
			want[st.Name+"."+f.Name+".es"] = strconv.FormatInt(size, 10)
			want[st.Name+"."+f.Name+".ea"] = strconv.FormatInt(align, 10)
		}
	}
	if patch != nil {
		patched := false
		for name, data := range g.files {
			if out := patch(string(data)); out != string(data) {
				g.files[name], patched = []byte(out), true
			}
		}
		if !patched {
			t.Fatal("the patch matched no generated module")
		}
	}
	main := strings.NewReplacer("MODULE", module, "NAMES", strings.Join(names, ", ")).Replace(wideRecordProbe)
	got := map[string]string{}
	for line := range strings.SplitSeq(runProbe(t, g, main, nil), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("probe line %q is not key=value", line)
		}
		got[k] = v
	}
	var bad []string
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

// TestLuauWideRecord: the Luau leg generates a record past 4 GiB, the
// generated modules load, and every record size and alignment and every list
// or map term size the descriptors hold equals ir's, past 2^32 where ir's is.
func TestLuauWideRecord(t *testing.T) {
	t.Parallel()
	memceiling.Guard(t, memceiling.Default)
	if bad := wideRecordRun(t, nil); len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
}

// TestLuauWideRecordNegativeControl narrows Huge's size to 32 bits in the
// generated descriptor and requires the probe to go red.
func TestLuauWideRecordNegativeControl(t *testing.T) {
	t.Parallel()
	memceiling.Guard(t, memceiling.Default)
	u, _ := wideRecordUnit(t)
	size := ir.RecordLayout(u, u.Tables["Huge"]).Size
	from := fmt.Sprintf("size = %d,", size)
	bad := wideRecordRun(t, func(src string) string {
		return strings.Replace(src, from, fmt.Sprintf("size = %d,", size&0xffffffff), 1)
	})
	if len(bad) == 0 {
		t.Fatal("a Huge size narrowed to 32 bits still matched ir")
	}
}
