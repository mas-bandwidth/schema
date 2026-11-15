package luatable

// A quantized float32 rides as its step index at bits_required(0, count), and
// an enum-keyed array rides as the count of present slots then one (key
// reference, element) pair per slot (docs/SPEC-TABLES.md §3.2, §3.3). The
// message batch matches the reference codec's bytes. A reader that does not
// declare the float skips that entry at the announced width and still reads
// the keyed array.

import (
	"bytes"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const luaQuantizedFloatSchema = `package probe

enum Slot { red, green, blue }

fixed table Root
{
    speed float32 | min = 0.0, max = 10.0, resolution = 0.01
    by [Slot]int32
}
`

const luaQuantizedFloatReaderSchema = `package probe

enum Slot { red, green, blue }

fixed table Root
{
    by [Slot]int32
}
`

const luaQuantizedFloatJSON = `{"speed":3.5,"by":{"red":11,"blue":22}}`

func TestMessageQuantizedFloatAndEnumKeyedArray(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Lua interpreter")

	unit := mqUnit(t, "Probe", luaQuantizedFloatSchema)
	model := tabletext.NewModel(unit)
	inst := model.New(model.Lookup("Root"))
	var report tabletext.Report
	if !model.Read(inst, []byte(luaQuantizedFloatJSON), &report) || !report.Silent() {
		t.Fatalf("source: %+v", report)
	}
	batch, err := tablewire.EncodeMessages(model, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatal(err)
	}
	announce := tablewire.Announce(unit)
	shape := quantizedFloatShape(t, unit, "speed")
	if shape.Bits != 10 {
		t.Fatalf("quantized width %d, want 10", shape.Bits)
	}
	if got := ir.TableMessageQuantize(shape, float32(3.5)); got != 350 {
		t.Fatalf("step index %d, want 350", got)
	}
	want := float64(ir.TableMessageDequantize(shape, 350))

	got := luaQuantizedFloatWrite(t, unit, announce, batch, want)
	if !bytes.Equal(got, batch) {
		t.Fatalf("lua batch %s\nwant      %s", hex.EncodeToString(got), hex.EncodeToString(batch))
	}

	reader := mqUnit(t, "Probe", luaQuantizedFloatReaderSchema)
	luaQuantizedFloatSkips(t, reader, announce, batch)
}

func quantizedFloatShape(t *testing.T, unit *ir.Unit, name string) ir.TableMessageShape {
	t.Helper()
	for _, e := range tablewire.MessageVocabulary(unit) {
		if e.Kind != ir.TableKindF32 {
			continue
		}
		st := unit.Tables["Root"]
		if st == nil {
			break
		}
		for _, f := range st.Fields {
			if f.Name == name && ir.TableFieldEntry(f).Id == e.Id {
				if e.Shape.Packing != ir.TableMessageQuantized {
					t.Fatalf("%s packing %d, want quantized", name, e.Shape.Packing)
				}
				return e.Shape
			}
		}
	}
	t.Fatalf("the unit announces no quantized float32 %s", name)
	return ir.TableMessageShape{}
}

func luaQuantizedFloatWrite(t *testing.T, unit *ir.Unit, announce, batch []byte, want float64) []byte {
	t.Helper()
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	writeFile(t, filepath.Join(dir, "main.lua"), `
local T = require("`+ModuleName(unit)+`")
local inputs = require("inputs")
local function hex(s)
  return (s:gsub(".", function(c) return string.format("%02x", string.byte(c)) end))
end
local report = {}
local vocab = {}
assert(T.AnnounceRead(vocab, inputs.announce, report), "announce")
local loaded = T.LoadMessages("Root", vocab, inputs.batch, report)
assert(loaded ~= nil and #loaded == 1, "reference batch refused")
assert(loaded[1].speed == `+mqLuaNumber(want)+`, "speed = " .. tostring(loaded[1].speed))
assert(loaded[1].by[1] == 11 and loaded[1].by[2] == nil and loaded[1].by[3] == 22, "keyed slots")
local fresh = T.NewRoot()
fresh.speed = `+mqLuaNumber(want)+`
fresh.by[1] = 11
fresh.by[3] = 22
local written = T.SaveMessages("Root", vocab, { fresh })
assert(written ~= nil, "save refused")
io.write(hex(written), "\n")
`)
	out := luarun.Run(t, dir, "main.lua", map[string][]byte{"announce": announce, "batch": batch})
	line := strings.TrimSpace(string(out))
	decoded, err := hex.DecodeString(line)
	if err != nil {
		t.Fatalf("probe batch %q: %v", line, err)
	}
	return decoded
}

func luaQuantizedFloatSkips(t *testing.T, unit *ir.Unit, announce, batch []byte) {
	t.Helper()
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	writeFile(t, filepath.Join(dir, "main.lua"), `
local T = require("`+ModuleName(unit)+`")
local inputs = require("inputs")
local report = {}
local vocab = {}
assert(T.AnnounceRead(vocab, inputs.announce, report), "announce")
local loaded = T.LoadMessages("Root", vocab, inputs.batch, report)
assert(loaded ~= nil and #loaded == 1, "a reader without the float refused the batch")
assert(loaded[1].speed == nil, "the reader named a float it does not declare")
assert(loaded[1].by[1] == 11 and loaded[1].by[2] == nil and loaded[1].by[3] == 22, "keyed slots after the skip")
print("skip-ok")
`)
	out := luarun.Run(t, dir, "main.lua", map[string][]byte{"announce": announce, "batch": batch})
	if strings.TrimSpace(string(out)) != "skip-ok" {
		t.Fatalf("skip probe printed %q", out)
	}
}
