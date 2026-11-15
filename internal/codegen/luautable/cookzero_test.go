package luautable_test

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// DEAD BYTES ARE ZERO IN A COOK (docs/SPEC-TABLES.md §7.2; docs/rulings.json
// fixed-dead-bytes-are-zero). An absent optional reads as its declared default,
// but in the cooked form its presence byte and its payload are zero; a counted
// array's slots past its live count are zero too. The Luau cook writer cooks
// keyed_config (absent optionals inside its records) and root_full (counted
// arrays below their capacity) from their committed wires in both byte orders,
// and each cook must be the committed fixture byte for byte, with every such
// dead span zero.
func TestLuauCookWriteZeroesDeadAndAbsentBytes(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	instances := map[string]reportRow{}
	for _, row := range manifestRows(t) {
		if row.kind == "instance" {
			instances[row.caseName] = row
		}
	}
	cases := []struct {
		name string
		want string // the dead-span kind the case must carry
	}{
		{"keyed_config", "absent"},
		{"root_full", "dead"},
	}
	cache := map[string]*cookUnit{}
	for _, c := range cases {
		row, ok := instances[c.name]
		if !ok {
			t.Fatalf("%s: no instance row", c.name)
		}
		cu := cache[row.unit]
		if cu == nil {
			cu = buildCookUnit(t, units, row.unit)
			cache[row.unit] = cu
		}
		wire, err := os.ReadFile(filepath.Join(root, row.wire))
		if err != nil {
			t.Fatal(err)
		}

		// the dead spans, from schema-go's read of the same wire
		st, ok := cu.u.Tables[row.root]
		if !ok {
			t.Fatalf("%s: no root %s", c.name, row.root)
		}
		inst := cu.m.New(st)
		var report tabletext.Report
		if ok, err := tablewire.Decode(cu.m, inst, wire, &report); !ok || err != nil {
			t.Fatalf("%s: schema-go's read: %v %+v", c.name, err, report)
		}
		var spans []deadSpan
		collectDeadSpans(cu.u, inst, tablecook.HeaderBytes, &spans)
		kinds := 0
		for _, s := range spans {
			if s.kind == c.want {
				kinds++
			}
		}
		if kinds == 0 {
			t.Fatalf("%s: the instance holds no %s span: the case proves nothing", c.name, c.want)
		}

		var mods []string
		for name := range cu.g.files {
			if strings.HasSuffix(name, "Table.luau") {
				mods = append(mods, "require("+strconv.Quote("./"+strings.TrimSuffix(name, ".luau"))+")")
			}
		}
		sort.Strings(mods)
		probe := strings.Join(mods, "\n") + fmt.Sprintf(`
local St = require("./schema_tables")
local L = require("./schema_load")
local C = require("./schema_cook")
local inputs = require("./inputs")
local d = St.lookup(%q)
local v = L.load(%q, inputs.wire, {})
assert(v ~= nil, "wire refused")
C.BuildVersion = 0x%016xi
for order = 1, 2 do
  local b = C.Cook(d, v, order)
  assert(b ~= nil, "cook refused")
  local parts = table.create(buffer.len(b))
  for i = 0, buffer.len(b) - 1 do parts[i + 1] = string.format("%%02x", buffer.readu8(b, i)) end
  print(table.concat(parts))
end
`, row.root, row.root, cu.build)
		out := runProbe(t, cu.g, probe, map[string][]byte{"wire": wire})
		lines := strings.Split(out, "\n")
		if len(lines) != 2 {
			t.Fatalf("%s: expected two cooks, got %q", c.name, out)
		}
		for order, line := range lines {
			got, err := hex.DecodeString(line)
			if err != nil {
				t.Fatal(err)
			}
			fixture := filepath.Join(root, "testdata/conformance/tables/cook-write", c.name+".cook")
			if order == 1 {
				fixture = filepath.Join(root, "testdata/conformance/tables/cook-write", c.name+"-be.cook")
			}
			want, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range spans {
				if s.to > int64(len(got)) {
					t.Fatalf("%s order %d: the cook is %d bytes, shorter than the %s span %d..%d", c.name, order+1, len(got), s.kind, s.from, s.to)
				}
				if span := got[s.from:s.to]; !bytes.Equal(span, make([]byte, len(span))) {
					t.Errorf("%s order %d: the %s span %s at %d..%d is not zero: %x", c.name, order+1, s.kind, s.what, s.from, s.to, span[:min(len(span), 16)])
				}
				if span := want[s.from:s.to]; !bytes.Equal(span, make([]byte, len(span))) {
					t.Fatalf("%s order %d: the fixture's %s span %s at %d..%d is not zero: the span walk is wrong", c.name, order+1, s.kind, s.what, s.from, s.to)
				}
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s order %d: the cook differs from the committed fixture", c.name, order+1)
			}
		}
	}
}

// deadSpan is one run of bytes a cook must leave zero: an absent optional's
// whole storage, or the slots of a counted array past its live count.
type deadSpan struct {
	kind     string // "absent" or "dead"
	what     string
	from, to int64
}

// collectDeadSpans walks one by-value record at its cooked offset, and every
// by-value record it holds in live storage.
func collectDeadSpans(u *ir.Unit, inst *tabletext.Instance, at int64, out *[]deadSpan) {
	layout := ir.RecordLayout(u, inst.Def)
	for _, fl := range layout.Fields {
		var fv *tabletext.Field
		for i := range inst.Fields {
			if inst.Fields[i].Def == fl.Field {
				fv = &inst.Fields[i]
			}
		}
		if fv == nil {
			continue
		}
		f := fl.Field
		pieces := ir.FieldPieces(u, f, at+fl.Offset)
		if len(pieces) == 0 {
			continue
		}
		name := inst.Def.Name + "." + f.Name
		if f.Type.Optional && !fv.Present {
			last := pieces[len(pieces)-1]
			*out = append(*out, deadSpan{"absent", name, pieces[0].Offset, last.Offset + last.Size})
			continue
		}
		if f.IsMap() || f.IsList() {
			continue
		}
		value := pieces[0]
		live := len(fv.Elems)
		var step int64
		if f.Array != ir.ArrayNone && live > 0 {
			step = value.Size / int64(live)
		}
		if f.Array == ir.ArrayCounted && live > 0 {
			live = min(fv.Count, len(fv.Elems))
			if live < len(fv.Elems) {
				*out = append(*out, deadSpan{"dead", name, value.Offset + int64(live)*step, value.Offset + value.Size})
			}
		}
		if f.Type.Kind != ir.TNamed || f.Type.Pointer {
			continue
		}
		if _, ok := f.Type.Ref.(*ir.Struct); !ok {
			continue
		}
		if f.Array == ir.ArrayNone {
			if fv.Cell.Tab != nil {
				collectDeadSpans(u, fv.Cell.Tab, value.Offset, out)
			}
			continue
		}
		for i := range live {
			if tab := fv.Elems[i].Tab; tab != nil {
				collectDeadSpans(u, tab, value.Offset+int64(i)*step, out)
			}
		}
	}
}
