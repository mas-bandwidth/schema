package mootable_test

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// valueDump is the canonical moo value dump the cook probes compare: every
// key sorted by its spelling, a buffer by length and bytes, so a load and a
// cook read of one instance are equal only when every slot agrees (§7.2, §4).
const valueDump = `local function keyrepr(k: any): string
	local t = type(k)
	if t == "number" then return "n" .. tostring(k) end
	if t == "string" then return "s" .. k end
	if t == "boolean" then return if k then "b1" else "b0" end
	return t
end

local function dump(v: any): string
	local t = type(v)
	if t == "nil" then return "nil" end
	if t == "boolean" then return if v then "true" else "false" end
	if t == "number" then return "n" .. tostring(v) end
	if t == "string" then return "s" .. #v .. ":" .. v end
	if t == "buffer" then
		local parts = table.create(buffer.len(v))
		for i = 0, buffer.len(v) - 1 do parts[i + 1] = string.format("%02x", buffer.readu8(v, i)) end
		return "b" .. buffer.len(v) .. ":" .. table.concat(parts)
	end
	if t == "table" then
		local keys = {}
		for k in pairs(v) do keys[#keys + 1] = k end
		table.sort(keys, function(a: any, b: any): boolean return keyrepr(a) < keyrepr(b) end)
		local parts = {}
		for _, k in keys do parts[#parts + 1] = keyrepr(k) .. "=" .. dump(v[k]) end
		return "{" .. table.concat(parts, ",") .. "}"
	end
	return t
end
`

// cookProbe loads inputs.wire through table_load.moo, opens inputs.cook through
// table_cookread.moo at base `lead` and the claimed extent, reads it, and
// prints "ok" when the two values are equal, or the refusal.
func cookProbe(g *generatedUnit, rootName string, lead, extent int, null, compare bool) string {
	var b strings.Builder
	b.WriteString("--!strict\n" + valueDump)
	b.WriteString(g.requireAll())
	b.WriteString("local inputs = require(\"./inputs\")\nlocal load = require(\"./table_load\")\nlocal C = require(\"./table_cookread\")\n")
	data := "inputs.cook"
	if null {
		data = "nil"
	}
	fmt.Fprintf(&b, "local h, why = C.open(%q, %s, %d, %d)\n", rootName, data, lead, extent)
	b.WriteString("if h == nil then print(\"refuse \" .. tostring(why)) return end\n")
	if !compare {
		b.WriteString("print(\"open\")\nreturn\n")
		return b.String()
	}
	fmt.Fprintf(&b, "local cooked, refusal = C.read(h)\nif cooked == nil then print(\"READ \" .. refusal.name .. \" \" .. refusal.field) return end\n")
	fmt.Fprintf(&b, "local report = load.newReport()\nlocal wire = load.loadTolerant(%q, inputs.wire, report)\n", rootName)
	b.WriteString(`local a, z = dump(wire), dump(cooked)
if a ~= z then
	local i, n = 1, math.min(#a, #z)
	while i <= n and string.sub(a, i, i) == string.sub(z, i, i) do i += 1 end
	print("DIFF " .. i .. " wire=" .. string.sub(a, math.max(1, i - 80), i + 80) .. " cook=" .. string.sub(z, math.max(1, i - 80), i + 80))
else
	print("ok")
end
`)
	return b.String()
}

// TestMooCookReadAgreesWithTheLoad cooks every instance row of the manifest
// through the tool (internal/tablecook, the reference, §7.6), opens and reads
// it through table_cookread.moo, and requires the value to equal moo's own
// variable-form load of the same wire (which TestMooTableLoadMatchesTheReports
// and the save gate hold to the Go engine).
func TestMooCookReadAgreesWithTheLoad(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_cookread.moo)")
	t.Parallel()
	units, _ := readManifest(t)
	cache := map[string]*generatedUnit{}
	compared := 0
	for _, row := range manifestRows(t) {
		if row.kind != "instance" {
			continue
		}
		g, ok := cache[row.unit]
		if !ok {
			g = generateUnit(t, units, row.unit)
			cache[row.unit] = g
		}
		if g.err != nil {
			t.Fatalf("unit %s: %v", row.unit, g.err)
		}
		if g.files["table_cookread.moo"] == nil {
			continue
		}
		m := tabletext.NewModel(g.unit)
		st := m.Lookup(row.root)
		if st == nil {
			t.Fatalf("%s: no table %s", row.caseName, row.root)
		}
		wire := rowWire(t, row)
		inst := m.New(st)
		var rep tabletext.Report
		if _, err := tablewire.Decode(m, inst, wire, &rep); err != nil {
			t.Fatalf("decode %s: %v", row.caseName, err)
		}
		cooked, err := tablecook.Cook(m, inst, tablecook.Options{})
		if err != nil {
			continue // the tool writes no cook for this one (§7.6's own refusals)
		}
		out := g.run(t, cookProbe(g, row.root, 0, len(cooked), false, true), map[string][]byte{"wire": wire, "cook": cooked})
		if out != "ok" {
			t.Errorf("%s (%s/%s): %s", row.caseName, row.unit, row.root, out)
			continue
		}
		compared++
	}
	if compared == 0 {
		t.Fatal("no instance cooked and compared")
	}
	t.Logf("%d instances cooked by the tool and read back equal", compared)
}

var cookgenOnce struct {
	sync.Once
	path string
	err  error
}

// cookgen builds test/cookgen once: the conformance harness's own fixture
// generator for the `cook` rows (test/conformance/harness/run.go).
func cookgen(t *testing.T) string {
	t.Helper()
	cookgenOnce.Do(func() {
		dir, err := os.MkdirTemp("", "moo-cookgen")
		if err != nil {
			cookgenOnce.err = err
			return
		}
		cookgenOnce.path = filepath.Join(dir, "cookgen")
		cmd := exec.Command("go", "build", "-o", cookgenOnce.path, "./test/cookgen")
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			cookgenOnce.err = fmt.Errorf("building cookgen: %w\n%s", err, out)
		}
	})
	if cookgenOnce.err != nil {
		t.Fatal(cookgenOnce.err)
	}
	return cookgenOnce.path
}

// cookShape is the harness's fixture shape per cook case
// (test/conformance/harness/run.go cookShape).
var cookShape = map[string][]string{
	"Scene":       {"--ref", "head", "--chain", "ListNode", "--next", "next"},
	"SceneValued": {"--ref", "head", "--chain", "ListNode", "--next", "next", "--values"},
	"Depot":       {"--ref", "head", "--chain", "ListNode", "--next", "next"},
	"Album":       {"--ref", "head", "--chain", "ListNode", "--next", "next"},
	"TreeNode":    {"--ref", "left", "--chain", "TreeNode", "--next", "left"},
	"ListNode":    {"--ref", "next", "--chain", "ListNode", "--next", "next"},
}

// cookFixture writes one `cook` row's file exactly as the harness does.
func cookFixture(t *testing.T, caseName, rootName string) []byte {
	t.Helper()
	out := filepath.Join(t.TempDir(), caseName+".cook")
	args := append([]string{"--bytes", "4096", "--root", rootName, "--out", out}, cookShape[caseName]...)
	cmd := exec.Command(cookgen(t), args...)
	cmd.Dir = root
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cookgen %s: %v\n%s", caseName, err, b)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestMooCookFixturesAndForgeries is §7.4's battery: every `cook` row's
// fixture (test/cookgen, as the harness writes it) opens and reads back to
// the value the Go tool's Uncook finds in it (re-encoded and loaded by moo);
// and every `cook` forgery, materialised over its fixture as the harness does,
// opens or refuses as its verdict says, a refusal naming its `refusal` row's
// reason.
func TestMooCookFixturesAndForgeries(t *testing.T) {
	slowtest.Gate(t, "moo and go build (it runs the emitted table_cookread.moo over test/cookgen's fixtures)")
	t.Parallel()
	units, _ := readManifest(t)
	g := generateUnit(t, units, "graphdemo")
	if g.err != nil {
		t.Fatal(g.err)
	}
	m := tabletext.NewModel(g.unit)
	fixtures := map[string][]byte{}
	for _, row := range manifestLines(t, "cook") {
		caseName, rootName := row[1], row[3]
		data := cookFixture(t, caseName, rootName)
		fixtures[caseName] = data
		inst, err := tablecook.Uncook(m, m.Lookup(rootName), data)
		if err != nil {
			t.Fatalf("%s: the tool does not uncook its own fixture: %v", caseName, err)
		}
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("%s: %v", caseName, err)
		}
		if out := g.run(t, cookProbe(g, rootName, 0, len(data), false, true), map[string][]byte{"wire": wire, "cook": data}); out != "ok" {
			t.Errorf("cook %s: %s", caseName, out)
		}
	}
	reasons := refusalReasons(t)
	fgs := forgeries(t, "cook")
	if len(fgs) == 0 {
		t.Fatal("the manifest names no cook forgery")
	}
	for _, fg := range fgs {
		base, ok := fixtures[fg.base]
		if !ok {
			t.Fatalf("%s: no fixture %s", fg.name, fg.base)
		}
		data, lead, extent, null := fg.materialize(t, base)
		got := g.run(t, cookProbe(g, fg.subject, lead, extent, null, false), map[string][]byte{"cook": data})
		want := fg.verdict
		if fg.verdict == "refuse" {
			reason, pinned := reasons[fg.name]
			if !pinned {
				st := g.unit.Tables[fg.subject]
				l := ir.RecordLayout(g.unit, st)
				reason = goCookReason(data, lead, extent, null, ir.BuildVersion(g.unit), l.Size, l.Align)
			}
			want = "refuse " + reason
		}
		if got != want {
			t.Errorf("%s: moo answers %q, want %q", fg.name, got, want)
		}
	}
	if out := g.analyze(t); out != "" {
		t.Fatalf("moo-analyze is not silent:\n%s", out)
	}
}

// goCookReason is Go's GENERATED Open check (internal/codegen/gotable cook.go
// emitOpen, docs/SPEC-TABLES.md §7.4) over the placed bytes, clause for clause
// and in its order: the reason a forgery with no `refusal` row of its own owes.
func goCookReason(data []byte, lead, extent int, null bool, build uint64, rootSize, rootAlign int64) string {
	if null {
		return "unaligned_base"
	}
	if extent < 64 {
		return "truncated"
	}
	h := data[lead:]
	le := binary.LittleEndian
	if magic := le.Uint64(h); magic != 0x4B4F4F434D484353 {
		if magic == 0x5343484D434F4F4B {
			return "foreign_order"
		}
		return "not_a_cook"
	}
	if le.Uint64(h[16:]) != 1 {
		return "not_a_cook"
	}
	if le.Uint64(h[8:]) != build {
		return "wrong_build_version"
	}
	if le.Uint64(h[48:]) != 0 || le.Uint64(h[56:]) != 0 {
		return "reserved_not_zero"
	}
	alignment := le.Uint64(h[40:])
	if alignment < 8 || alignment > 64 || alignment&(alignment-1) != 0 || alignment%uint64(rootAlign) != 0 {
		return "bad_alignment"
	}
	bytes := uint64(extent)
	dataOffset := (uint64(64) + alignment - 1) &^ (alignment - 1)
	dataLength, attribution := le.Uint64(h[24:]), le.Uint64(h[32:])
	if dataOffset > bytes || dataLength > bytes-dataOffset || attribution != bytes-dataOffset-dataLength || dataLength < uint64(rootSize) {
		return "truncated"
	}
	if uint64(lead)%alignment != 0 {
		return "unaligned_base"
	}
	return "ok"
}

// TestMooCookAndBlockRefuseNonFiniteFloats: a float whose bits are a NaN or
// an infinity is never returned by an accelerator read (SPEC-TABLES §3.3 as
// ruled 2026-10-27): the cook read and the block read each refuse it as
// non_finite_float, naming the field. Each is the control of the other's
// clean read of the same bytes before the NaN is planted.
func TestMooCookAndBlockRefuseNonFiniteFloats(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted accelerator reads)")
	t.Parallel()
	// the cook: the tool cooks 1.5, the float32 0x3fc00000, and the probe
	// plants a quiet NaN over those bits
	u := unitFromSource(t, "package probe\ntable F\n{\n    id uint32\n    a float32\n}\n")
	m := tabletext.NewModel(u)
	inst := m.New(u.Tables["F"])
	var rep tabletext.Report
	if !m.Read(inst, []byte(`{"id":7,"a":1.5}`), &rep) {
		t.Fatalf("the Go engine does not read the JSON: %+v", rep)
	}
	cooked, err := tablecook.Cook(m, inst, tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	g := writeUnit(t, u, files)
	read := func(plant bool) string {
		main := "--!strict\nlocal inputs = require(\"./inputs\")\nlocal C = require(\"./table_cookread\")\n" + g.requireAll() + `local b = inputs.cook
`
		if plant {
			main += `for i = 0, buffer.len(b) - 4 do
	if buffer.readu32(b, i) == 0x3fc00000 then buffer.writeu32(b, i, 0x7fc00000) end
end
`
		}
		main += `local h = C.open("F", b)
local v, why = C.read(h)
if v == nil then print(why.name .. " " .. why.field) else print("read " .. string.format("%08x", v.A)) end
`
		return g.run(t, main, map[string][]byte{"cook": cooked})
	}
	if got := read(false); got != "read 3fc00000" {
		t.Fatalf("the control, the clean cook: %s", got)
	}
	if got := read(true); got != "non_finite_float F.a" {
		t.Fatalf("a planted NaN in a cook: got %q, want non_finite_float F.a", got)
	}

	// the block: block_render's camera fov, a float32, set to +Inf
	bg := blockUnit(t)
	data, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "block_render.bin"))
	if err != nil {
		t.Fatal(err)
	}
	blockRead := func(plant bool) string {
		var b strings.Builder
		b.WriteString("--!strict\nlocal inputs = require(\"./inputs\")\nlocal B = require(\"./table_blockread\")\nlocal T = require(\"./RenderTable\")\n")
		b.WriteString("local blk = B.BlockOpen(T.RenderFrameBlockInfo, inputs.block, 0, buffer.len(inputs.block))\n")
		if plant {
			b.WriteString(`for _, f in T.RenderFrameBlockInfo.fields do
	if f.name == "cameras" then
		local offsetOf = B.Triple(blk, f)
		for _, e in f.element.fields do
			if e.name == "fov" then buffer.writeu32(inputs.block, offsetOf + e.offset, 0x7f800000) end
		end
	end
end
`)
		}
		b.WriteString("local v, why = B.Read(blk)\nif v == nil then print(why.name .. \" \" .. why.field) else print(\"read\") end\n")
		return bg.run(t, b.String(), map[string][]byte{"block": data})
	}
	if got := blockRead(false); got != "read" {
		t.Fatalf("the control, the clean block: %s", got)
	}
	if got := blockRead(true); got != "non_finite_float RenderCamera.fov" {
		t.Fatalf("a planted infinity in a block: got %q, want non_finite_float RenderCamera.fov", got)
	}
}

// TestMooAcceleratorsHonourForms: a table whose forms declaration leaves the
// cook or block form out refuses that accessor by name (docs/SPEC-TABLES.md
// §2.12), while its sibling that carries the form opens; the sibling is the
// control.
func TestMooAcceleratorsHonourForms(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted accelerator reads)")
	t.Parallel()
	u := unitFromSource(t, "package probe\nfixed table Open\n{\n    a int32\n}\nfixed table Shut | forms = \"wire\"\n{\n    a int32\n}\n")
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	g := writeUnit(t, u, files)
	main := "--!strict\nlocal C = require(\"./table_cookread\")\nlocal B = require(\"./table_blockread\")\nlocal T = require(\"./ProbeTable\")\n" + `local function try(f: () -> ()): string
	local ok, e = pcall(f)
	if ok then return "ok" end
	return if string.find(tostring(e), "does not carry", 1, true) then "refused" else tostring(e)
end
print(try(function() C.open("Open", buffer.create(0)) end))
print(try(function() C.open("Shut", buffer.create(0)) end))
print(try(function() B.BlockOpen(T.OpenBlockInfo, buffer.create(0)) end))
print(try(function() B.BlockOpen(T.ShutBlockInfo, buffer.create(0)) end))
`
	if got := g.run(t, main, nil); got != "ok\nrefused\nok\nrefused" {
		t.Fatalf("forms: got %q, want ok/refused/ok/refused", got)
	}
}

// TestMooCookNegativeControls plants one defect per cook gate into the emitted
// table_cookread.moo and requires that gate to go red: a string's used length
// read one short (the value comparison), and the byte-order check removed (the
// cook_byte_order forgeries open).
func TestMooCookNegativeControls(t *testing.T) {
	slowtest.Gate(t, "moo and go build (it runs the emitted table_cookread.moo)")
	t.Parallel()
	units, _ := readManifest(t)
	for _, c := range []struct{ name, anchor, repl string }{
		{"value", "\tlocal n = buffer.readi32(ctx.buffer, la)\n", "\tlocal n = buffer.readi32(ctx.buffer, la) - 1\n"},
		{"order", "\tif buffer.readu32(b, at + 16) ~= 1 or buffer.readu32(b, at + 20) ~= 0 then\n", "\tif false then\n"},
	} {
		g := generateUnit(t, units, "graphdemo")
		path := filepath.Join(g.dir, "table_cookread.moo")
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(src), c.anchor) != 1 {
			t.Fatalf("%s: the anchor is not in table_cookread.moo exactly once", c.name)
		}
		if err := os.WriteFile(path, []byte(strings.Replace(string(src), c.anchor, c.repl, 1)), 0o644); err != nil {
			t.Fatal(err)
		}
		data := cookFixture(t, "SceneValued", "Scene")
		m := tabletext.NewModel(g.unit)
		inst, err := tablecook.Uncook(m, m.Lookup("Scene"), data)
		if err != nil {
			t.Fatal(err)
		}
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatal(err)
		}
		switch c.name {
		case "value":
			if out := g.run(t, cookProbe(g, "Scene", 0, len(data), false, true), map[string][]byte{"wire": wire, "cook": data}); out == "ok" {
				t.Errorf("value: the planted length still reads equal")
			}
		case "order":
			fg := forgery{name: "order", pointer: "0", offsets: []int64{0x10}, widths: []int64{8}, values: []uint64{2}, extent: -1}
			d, lead, extent, null := fg.materialize(t, data)
			if out := g.run(t, cookProbe(g, "Scene", lead, extent, null, false), map[string][]byte{"cook": d}); out == "refuse not_a_cook" {
				t.Errorf("order: the byte-order forgery still refuses with the check removed")
			}
		}
	}
}
