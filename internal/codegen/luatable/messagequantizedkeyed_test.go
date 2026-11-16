package luatable

// A MESSAGE BODY CARRIES A QUANTIZED FLOAT32 AND AN ENUM-KEYED ARRAY
// (docs/SPEC-TABLES.md §3.3): the float rides its step index in
// bits_required(0, count) bits (packing 2), and the keyed array rides the
// number of present slots then one (key reference, element) pair per slot
// (kind 16). A fixed table with both round-trips through the message form to
// the Go reference leg's bytes, and a reader built without the float skips it
// at its announced width and still reads the keyed array.

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// mqaProbe runs one Lua probe over a unit, its announcement and a batch, and
// returns stdout. The probe requires the module and the inputs module the
// runner writes.
func mqaProbe(t *testing.T, unit *ir.Unit, announce, batch []byte, main string) string {
	t.Helper()
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	writeFile(t, filepath.Join(dir, "main.lua"), main)
	return string(luarun.Run(t, dir, "main.lua", map[string][]byte{"announce": announce, "batch": batch}))
}

// mqaHex reads a probe that printed one hex line of the batch it saved.
func mqaHex(t *testing.T, out string) []byte {
	t.Helper()
	line := strings.TrimSpace(out)
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

func TestMessageQuantizedFloatAndEnumKeyedArray(t *testing.T) {
	t.Parallel()
	schema := `package probe

enum Slot { Alpha, Beta, Gamma }

fixed table Root
{
    cf     float32 | min = 0.0, max = 10.0, resolution = 0.01
    tokens [Slot]int32
}
`
	unit := mqUnit(t, "Probe", schema)
	model := tabletext.NewModel(unit)
	announce := tablewire.Announce(unit)

	// the announced shape the float rides at, from the unit's own vocabulary
	var cfShape ir.TableMessageShape
	found := false
	for _, e := range tablewire.MessageVocabulary(unit) {
		if e.Kind == ir.TableKindF32 {
			cfShape = e.Shape
			found = true
		}
	}
	if !found {
		t.Fatalf("the unit announces no quantized f32: %+v", tablewire.MessageVocabulary(unit))
	}

	// the Go reference leg's bytes for one body carrying both fields
	inst := mqInstance(t, unit, `{"cf":3.5,"tokens":{"Alpha":101,"Gamma":303}}`)
	batch, err := tablewire.EncodeMessages(model, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatal(err)
	}

	// the value the float's step index names, read back in Lua and re-saved
	want := float64(ir.TableMessageDequantize(cfShape, ir.TableMessageQuantize(cfShape, float32(3.5))))

	roundTrip := `
local T = require("` + ModuleName(unit) + `")
local inputs = require("inputs")
local report = {}
local vocab = {}
assert(T.AnnounceRead(vocab, inputs.announce, report), "announce")
local values = T.LoadMessages("Root", vocab, inputs.batch, report)
assert(values ~= nil and #values == 1, "batch refused")
assert(values[1].cf == ` + mqLuaNumber(want) + `, "cf = " .. tostring(values[1].cf))
assert(values[1].tokens[1] == 101 and values[1].tokens[3] == 303, "tokens")
local fresh = T.NewRoot()
fresh.cf = ` + mqLuaNumber(3.5) + `
fresh.tokens = { [1] = 101, [3] = 303 }
local written = T.SaveMessages("Root", vocab, { fresh })
assert(written ~= nil, "save refused")
io.write((written:gsub(".", function(c) return string.format("%02x", c:byte()) end)), "\n")
`
	got := mqaHex(t, mqaProbe(t, unit, announce, batch, roundTrip))
	if string(got) != string(batch) {
		t.Fatalf("Lua's batch differs from the Go leg's: got %x, want %x", got, batch)
	}

	// a reader built without the float skips it at its announced width and
	// still reads the keyed array; the batch is not damaged by the unknown
	// field, and the value the reader knows re-saves cleanly.
	receiver := mqUnit(t, "Receiver", `package receiver

enum Slot { Alpha, Beta, Gamma }

fixed table Root
{
    tokens [Slot]int32
}
`)
	skip := `
local T = require("` + ModuleName(receiver) + `")
local inputs = require("inputs")
local report = {}
local vocab = {}
assert(T.AnnounceRead(vocab, inputs.announce, report), "announce")
local values = T.LoadMessages("Root", vocab, inputs.batch, report)
assert(values ~= nil and #values == 1, "batch refused")
assert(values[1].tokens[1] == 101 and values[1].tokens[3] == 303, "tokens")
local written = T.SaveMessages("Root", vocab, values)
assert(written ~= nil, "save refused")
print("skip-ok")
`
	if out := strings.TrimSpace(mqaProbe(t, receiver, announce, batch, skip)); out != "skip-ok" {
		t.Fatalf("a reader without the float did not skip it cleanly: %q", out)
	}
}
