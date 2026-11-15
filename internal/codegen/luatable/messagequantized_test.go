package luatable

// A MESSAGE'S COMPRESSED FLOAT QUANTIZES BY THE PACKET WIRE'S RULE
// (docs/SPEC-TABLES.md §3.3, docs/SPEC.md §4.3): an entry announced at packing
// 2 rides as its step index in bits_required(0, count) bits, the index is
// refused above count, and an entry this reader does not name is skipped at
// that same width.

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func mqUnit(t *testing.T, name, schema string) *ir.Unit {
	t.Helper()
	path := filepath.Join(t.TempDir(), name+".schema")
	writeFile(t, path, schema)
	return luarun.LoadUnit(t, path)
}

func mqInstance(t *testing.T, unit *ir.Unit, text string) *tabletext.Instance {
	t.Helper()
	model := tabletext.NewModel(unit)
	inst := model.New(model.Lookup("Root"))
	var report tabletext.Report
	if !model.Read(inst, []byte(text), &report) || !report.Silent() {
		t.Fatalf("source %s: %+v", text, report)
	}
	return inst
}

func mqCellF(t *testing.T, model *tabletext.Model, inst *tabletext.Instance, name string) float64 {
	t.Helper()
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == name {
			return inst.Fields[i].Cell.F
		}
	}
	t.Fatalf("field %s is absent", name)
	return 0
}

// mqBatch is a form-2 batch whose one body is the reference `slot` carrying
// index at the announced width, followed by the body terminator. It is the
// malformed-index witness the writer never emits.
func mqBatch(vocab []ir.TableVocabularyEntry, slot, index uint64, width int) []byte {
	var bytes []byte
	bits := 0
	put := func(v uint64, n int) {
		for i := range n {
			if bits>>3 == len(bytes) {
				bytes = append(bytes, 0)
			}
			if v>>i&1 == 1 {
				bytes[bits>>3] |= 1 << (bits & 7)
			}
			bits++
		}
	}
	refBits := ir.TableMessageRefBits(len(vocab))
	put(0, 8) // the body count, one body
	put(slot, refBits)
	put(index, width)
	put(0, refBits)
	for bits&7 != 0 {
		put(0, 1)
	}
	return append([]byte{2}, bytes...)
}

// mqLuaNumber renders a float as a Lua numeral that keeps its float type.
func mqLuaNumber(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eEn") {
		s += ".0"
	}
	return s
}

func mqLuaWrite(t *testing.T, unit *ir.Unit, announce, engine []byte, value, want float64) []byte {
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
local values = T.LoadMessages("Root", vocab, inputs.batch, report)
assert(values ~= nil and #values == 1, "engine batch refused")
assert(values[1].cf == `+mqLuaNumber(want)+`, "engine batch value = " .. tostring(values[1].cf))
local fresh = T.NewRoot()
fresh.cf = `+mqLuaNumber(value)+`
local written = T.SaveMessages("Root", vocab, { fresh })
assert(written ~= nil, "save refused")
io.write((written:gsub(".", function(c) return string.format("%02x", c:byte()) end)), "\n")
`)
	out := luarun.Run(t, dir, "main.lua", map[string][]byte{"announce": announce, "batch": engine})
	line := strings.TrimSpace(string(out))
	if len(line) == 0 {
		t.Fatalf("probe printed no batch: %q", out)
	}
	decoded := make([]byte, len(line)/2)
	for i := range decoded {
		if _, err := fmt.Sscanf(line[2*i:2*i+2], "%02x", &decoded[i]); err != nil {
			t.Fatalf("probe batch %q: %v", line, err)
		}
	}
	return decoded
}

func mqLuaReads(t *testing.T, unit *ir.Unit, announce, batch []byte, want float64) []byte {
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
local values = T.LoadMessages("Root", vocab, inputs.batch, report)
assert(values ~= nil and #values == 1, "batch refused")
assert(values[1].cf == `+mqLuaNumber(want)+`, "value = " .. tostring(values[1].cf))
print("read-ok")
`)
	return luarun.Run(t, dir, "main.lua", map[string][]byte{"announce": announce, "batch": batch})
}

func TestLuaMessageCompressedFloatQuantized(t *testing.T) {
	t.Parallel()
	schema := `package probe

fixed table Root
{
    cf float32 | min = 0.0, max = 10.0, resolution = 0.01
}
`
	unit := mqUnit(t, "Probe", schema)
	model := tabletext.NewModel(unit)
	announce := tablewire.Announce(unit)
	entries := tablewire.MessageVocabulary(unit)
	slot, shape := 0, ir.TableMessageShape{}
	for i, e := range entries {
		if e.Kind == ir.TableKindF32 {
			slot, shape = i+1, e.Shape
		}
	}
	if slot == 0 || shape.Packing != ir.TableMessageQuantized {
		t.Fatalf("the unit announces no quantized f32: %+v", entries)
	}
	var vocab tablewire.Vocabulary
	var announced tabletext.Report
	if err := vocab.AnnounceRead(announce, &announced); err != nil {
		t.Fatal(err)
	}

	// THE ENGINE'S BATCH READS IN LUA, AND LUA'S BATCH READS IN THE ENGINE,
	// AT THE SAME STEP INDEX AND TO THE SAME VALUE.
	for _, value := range []float64{3.5, 0.0, 10.0, 0.005, 3.333, 9.999, 7.77} {
		want := float64(ir.TableMessageDequantize(shape, ir.TableMessageQuantize(shape, float32(value))))
		batch, err := tablewire.EncodeMessages(model, []*tabletext.Instance{mqInstance(t, unit, fmt.Sprintf(`{"cf":%s}`, mqLuaNumber(value)))})
		if err != nil {
			t.Fatal(err)
		}
		luaBatch := mqLuaWrite(t, unit, announce, batch, value, want)
		back := model.New(model.Lookup("Root"))
		var report tabletext.Report
		if ok, err := tablewire.DecodeMessage(model, back, luaBatch, &vocab, &report); !ok || err != nil || !report.Silent() {
			t.Fatalf("value %v: the engine refused Lua's batch: %v %v %+v", value, ok, err, report)
		}
		if got := mqCellF(t, model, back, "cf"); got != want {
			t.Fatalf("value %v: the engine read %v from Lua's batch, want %v", value, got, want)
		}
		mqLuaReads(t, unit, announce, batch, want)
	}

	// AN INDEX ABOVE COUNT REFUSES THE BATCH.
	count, _, ok := ir.TableMessageQuantization(shape)
	if !ok {
		t.Fatal("the quantized triple does not derive")
	}
	over := mqBatch(entries, uint64(slot), uint64(count)+1, int(shape.Bits))
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
local values = T.LoadMessages("Root", vocab, inputs.batch, report)
assert(values == nil or #values == 0, "an index above count was read")
print("refused-ok")
`)
	luarun.Run(t, dir, "main.lua", map[string][]byte{"announce": announce, "batch": over})

	// AN UNKNOWN QUANTIZED ENTRY BEFORE A KNOWN FIELD IS SKIPPED AT ITS
	// ANNOUNCED WIDTH, AND THE KNOWN FIELD READS.
	sender := mqUnit(t, "Sender", `package sender

fixed table Root
{
    extra float32 | min = 0.0, max = 10.0, resolution = 0.01
    cf    float32 | min = 0.0, max = 10.0, resolution = 0.01
}
`)
	senderModel := tabletext.NewModel(sender)
	crossBatch, err := tablewire.EncodeMessages(senderModel, []*tabletext.Instance{
		mqInstance(t, sender, `{"extra":2.5,"cf":3.5}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	mqLuaReads(t, unit, tablewire.Announce(sender), crossBatch, 3.5)
}
