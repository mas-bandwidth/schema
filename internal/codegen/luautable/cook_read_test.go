package luautable_test

import (
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const cookMagic = uint64(0x4B4F4F434D484353)

// goCookReason is Go's generated Open check (docs/SPEC-TABLES.md §7.4) over
// the same header bytes and build facts, so the expected verdict in this test
// comes from schema-go's own constants and order rather than from a literal
// written beside a damaged file.
func goCookReason(cook []byte, build uint64, rootSize, rootAlign int64) string {
	if cook == nil {
		return "unaligned_base"
	}
	n := int64(len(cook))
	if n < tablecook.HeaderBytes {
		return "truncated"
	}
	le := binary.LittleEndian
	if le.Uint64(cook[0:]) != cookMagic {
		if le.Uint64(cook[0:]) == 0x5343484D434F4F4B {
			return "foreign_order"
		}
		return "not_a_cook"
	}
	if le.Uint64(cook[16:]) != 1 {
		return "not_a_cook"
	}
	if le.Uint64(cook[8:]) != build {
		return "wrong_build_version"
	}
	if le.Uint64(cook[48:]) != 0 || le.Uint64(cook[56:]) != 0 {
		return "reserved_not_zero"
	}
	alignment := int64(le.Uint64(cook[40:]))
	if alignment < 8 || alignment > 64 || alignment&(alignment-1) != 0 || alignment%rootAlign != 0 {
		return "bad_alignment"
	}
	dataOffset := (int64(64) + alignment - 1) &^ (alignment - 1)
	dataLength := int64(le.Uint64(cook[24:]))
	attribution := int64(le.Uint64(cook[32:]))
	if dataOffset > n || dataLength > n-dataOffset || attribution != n-dataOffset-dataLength || dataLength < rootSize {
		return "truncated"
	}
	return "ok"
}

// cookDump is the canonical value dump the probe compares: every key sorted
// by its spelling, a buffer by length and bytes, so a load and a cook read of
// one instance are equal only when every slot agrees (§7.2, §4).
const cookDump = `local function keyrepr(k: any): string
	local t = type(k)
	if t == "integer" then return "i" .. tostring(k) end
	if t == "number" then return string.format("f%.17g", k) end
	if t == "string" then return "s" .. k end
	if t == "boolean" then return if k then "b1" else "b0" end
	return t .. tostring(k)
end

local function dump(v: any): string
	local t = type(v)
	if t == "nil" then return "nil" end
	if t == "boolean" then return if v then "true" else "false" end
	if t == "integer" then return "i" .. tostring(v) end
	if t == "number" then return string.format("f%.17g", v) end
	if t == "string" then return "s" .. #v .. ":" .. v end
	if t == "buffer" then
		local parts = table.create(buffer.len(v))
		for i = 0, buffer.len(v) - 1 do parts[i + 1] = string.format("%02x", buffer.readu8(v, i)) end
		return "b" .. buffer.len(v) .. ":" .. table.concat(parts)
	end
	if t == "table" then
		local keys = {}
		for k in pairs(v) do keys[#keys + 1] = k end
		table.sort(keys, function(a, b)
			local x, y = keyrepr(a), keyrepr(b)
			if x == y then return type(a) < type(b) end
			return x < y
		end)
		local parts = {}
		for _, k in ipairs(keys) do parts[#parts + 1] = keyrepr(k) .. "=" .. dump(v[k]) end
		return "{" .. table.concat(parts, ",") .. "}"
	end
	return t
end
`

// cookProbe builds one Luau main: load the wire through schema_load.luau, open the
// cook through schema_cookread.luau at this build, and compare the two dumps.
func cookProbe(g *generatedUnit, rootName, build string, nilBase bool) string {
	var mods []string
	for name := range g.files {
		if strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)
	var b strings.Builder
	b.WriteString(cookDump)
	for _, m := range mods {
		fmt.Fprintf(&b, "require(\"./%s\")\n", m)
	}
	b.WriteString("local inputs = require(\"./inputs\")\nlocal load = require(\"./schema_load\")\nlocal cook = require(\"./schema_cookread\")\n")
	fmt.Fprintf(&b, "local root = %q\nlocal build = %si\n", rootName, build)
	b.WriteString("local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }\n")
	if nilBase {
		b.WriteString("local opened, why = cook.open(root, nil, build)\n")
		b.WriteString("if opened ~= nil then print(\"opened\") else print(why) end\nreturn\n")
	} else {
		b.WriteString("local wire = load.loadTolerant(root, inputs.wire, report)\n")
		b.WriteString("local opened, why = cook.open(root, inputs.cook, build)\n")
		b.WriteString("if opened == nil then print(why) return end\n")
		b.WriteString("local a, z = dump(wire), dump(opened)\n")
		b.WriteString("if a ~= z then\n")
		b.WriteString("  local i, n = 1, math.min(#a, #z)\n")
		b.WriteString("  while i <= n and string.sub(a, i, i) == string.sub(z, i, i) do i = i + 1 end\n")
		b.WriteString("  print(\"DIFF \" .. i .. \" wire=\" .. string.sub(a, math.max(1, i - 80), i + 80) .. \" cook=\" .. string.sub(z, math.max(1, i - 80), i + 80))\n")
		b.WriteString("else print(\"ok\") end\n")
	}
	return b.String()
}

type cookUnit struct {
	g     *generatedUnit
	m     *tabletext.Model
	u     *ir.Unit
	build uint64
}

// buildCookUnit compiles one manifest unit, emits its Luau surface and writes
// it into a fresh t.TempDir(). It returns the generated unit and the model
// the Go engine reads the same schema through.
func buildCookUnit(t *testing.T, units map[string][]string, key string) *cookUnit {
	t.Helper()
	paths, err := compiler.GatherPaths(prefix(units[key]))
	if err != nil {
		t.Fatalf("unit %s: %v", key, err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("unit %s does not load: %v", key, err)
	}
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatalf("unit %s: %v", key, err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatalf("unit %s: %v", key, err)
	}
	maps.Copy(files, tables)
	g := &generatedUnit{dir: t.TempDir(), files: files, loaded: true}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(g.dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &cookUnit{g: g, m: tabletext.NewModel(u), u: u, build: ir.BuildVersion(u)}
}

// runProbe writes a fresh copy of one unit's modules, adds main.luau, and
// runs it under the pinned interpreter.
func runProbe(t *testing.T, g *generatedUnit, main string, inputs map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range g.files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", inputs)))
}

// TestLuauCookOpenAgreesWithGo cooks every instance row the Luau target can
// generate (internal/tablecook, the Go writer), opens it through schema_cookread.luau
// and requires the value to equal Go's variable-form read of the same wire
// (internal/tabletext + internal/tablewire). One damaged file per §7.4
// refusal answers the name Go's own check computes. The build version is
// passed to Open because the Luau table surface does not yet export it.
func TestLuauCookOpenAgreesWithGo(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	cache := map[string]*cookUnit{}
	compared, refused := 0, 0
	damaged := false
	for _, row := range manifestRows(t) {
		if row.kind != "instance" {
			continue
		}
		row := row
		t.Run(row.unit+"/"+row.caseName, func(t *testing.T) {
			cu, ok := cache[row.unit]
			if !ok {
				cu = buildCookUnit(t, units, row.unit)
				cache[row.unit] = cu
			}
			rootName := row.root
			table := cu.m.Lookup(rootName)
			if table == nil {
				t.Fatalf("%s: the unit has no table %s", row.caseName, rootName)
			}
			wire, err := os.ReadFile(filepath.Join(root, row.wire))
			if err != nil {
				t.Fatal(err)
			}
			inst := cu.m.New(table)
			var report tabletext.Report
			if _, err := tablewire.Decode(cu.m, inst, wire, &report); err != nil {
				t.Fatalf("decode %s: %v", row.caseName, err)
			}
			cooked, err := tablecook.Cook(cu.m, inst, tablecook.Options{})
			if err != nil {
				t.Skipf("the tool writes no cook for %s: %v", row.caseName, err)
			}
			build := fmt.Sprintf("0x%016x", cu.build)
			out := runProbe(t, cu.g, cookProbe(cu.g, rootName, build, false), map[string][]byte{"wire": wire, "cook": cooked})
			if out != "ok" {
				t.Errorf("%s: %s", row.caseName, out)
				return
			}
			compared++
			if damaged {
				return
			}
			damaged = true
			layout := ir.RecordLayout(cu.u, table)
			cases := []struct {
				name    string
				bytes   []byte
				nilBase bool
			}{
				{"not_a_cook", mutate(cooked, func(b []byte) { b[0] ^= 1 }), false},
				{"foreign_order", mutate(cooked, func(b []byte) { binary.BigEndian.PutUint64(b, cookMagic) }), false},
				// THE HALF NO HARNESS SURFACE FORGES (docs/PORTING.md I7): the
				// magic intact and the order word saying the other order
				{"order_word", mutate(cooked, func(b []byte) { binary.LittleEndian.PutUint64(b[16:], 2) }), false},
				{"wrong_build_version", mutate(cooked, func(b []byte) { b[8] ^= 1 }), false},
				{"reserved_not_zero", mutate(cooked, func(b []byte) { b[48] = 1 }), false},
				{"bad_alignment", mutate(cooked, func(b []byte) { b[40] = 3 }), false},
				{"truncated", cooked[:len(cooked)-1], false},
				{"unaligned_base", nil, true},
			}
			for _, tc := range cases {
				t.Run("damaged/"+tc.name, func(t *testing.T) {
					want := goCookReason(tc.bytes, cu.build, layout.Size, layout.Align)
					got := runProbe(t, cu.g, cookProbe(cu.g, rootName, build, tc.nilBase), map[string][]byte{"wire": wire, "cook": tc.bytes})
					if got != want {
						t.Errorf("%s: got %s, want %s", tc.name, got, want)
						return
					}
					refused++
				})
			}
		})
	}
	if compared == 0 || refused == 0 {
		t.Fatalf("comparisons %d, refusals %d", compared, refused)
	}
}

func mutate(in []byte, f func([]byte)) []byte {
	out := append([]byte(nil), in...)
	f(out)
	return out
}
