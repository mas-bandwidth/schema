package luatable

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const luaSkipRunSchema = `package skiprun

fixed table Probe {
    x uint8
}
`

// luaSkipRunAnnouncement builds an announcement carrying one array entry that
// the Probe module does not declare: no count rides where min equals max, and
// the element's shape decides whether it is a fixed-width run.
func luaSkipRunAnnouncement(t *testing.T, entry ir.TableVocabularyEntry) []byte {
	t.Helper()
	vocabulary := ir.TableVocabularyBytes([]ir.TableVocabularyEntry{entry})
	out := []byte{ir.TableWireForm}
	out = append(out, 1, uint8(ir.TableKindU64))
	out = binary.LittleEndian.AppendUint64(out, 0x0102030405060708)
	out = append(out, 2, uint8(ir.TableKindArray))
	body := []byte{uint8(ir.TableKindU8)}
	body = binary.AppendUvarint(body, uint64(len(vocabulary)))
	body = append(body, vocabulary...)
	out = binary.AppendUvarint(out, uint64(len(body)))
	out = append(out, body...)
	out = append(out, 0)
	out = binary.LittleEndian.AppendUint64(out, ir.TableBuildVersionWireId)
	out = binary.LittleEndian.AppendUint64(out, ir.TableMessageVocabularyWireId)
	return binary.LittleEndian.AppendUint64(out, 2)
}

func luaSkipRunArrayEntry(t *testing.T, n, bits, base int64) ir.TableVocabularyEntry {
	t.Helper()
	inner := &ir.TableMessageShape{Packing: ir.TableMessageRanged, Bits: bits, Base: big.NewInt(base)}
	return ir.TableVocabularyEntry{
		Id:   0x5a5a5a5a00000001,
		Kind: ir.TableKindArray,
		Shape: ir.TableMessageShape{
			Min: n, Max: n, Elem: ir.TableKindU32, Inner: inner,
		},
	}
}

// luaSkipRunFixedEntry announces a plain U32 array with no inner packing, so
// one element rides exactly 32 bits and a run of them is one arithmetic step.
func luaSkipRunFixedEntry(t *testing.T, n int64) ir.TableVocabularyEntry {
	t.Helper()
	return ir.TableVocabularyEntry{
		Id:   0x5a5a5a5a00000002,
		Kind: ir.TableKindArray,
		Shape: ir.TableMessageShape{
			Min: n, Max: n, Elem: ir.TableKindU32,
		},
	}
}

// luaSkipRunQuantizedEntry announces a quantized f32 array. Its element rides
// the step index the triple derives, so a stepped element consumes its width
// and each index is held to the count the triple names.
func luaSkipRunQuantizedEntry(t *testing.T, n int64) (ir.TableVocabularyEntry, uint32, int) {
	t.Helper()
	inner := &ir.TableMessageShape{Packing: ir.TableMessageQuantized, QMin: 0, QMax: 1, QRes: 0.1}
	count, _, ok := ir.TableMessageQuantization(*inner)
	if !ok {
		t.Fatal("the quantized shape does not derive")
	}
	inner.Bits = int64(ir.TableMessageBitsRequired(0, int64(count)))
	return ir.TableVocabularyEntry{
		Id:   0x5a5a5a5a00000003,
		Kind: ir.TableKindArray,
		Shape: ir.TableMessageShape{
			Min: n, Max: n, Elem: ir.TableKindF32, Inner: inner,
		},
	}, count, int(inner.Bits)
}

// luaSkipRunBatch packs a form-2 batch holding one body: the entry's reference
// and its payload at the given width, terminated by the zero reference.
func luaSkipRunBatch(t *testing.T, refBits int, ref, value uint64, width int) []byte {
	t.Helper()
	bits := make([]byte, 0, 2*refBits+width)
	put := func(v uint64, n int) {
		for i := 0; i < n; i++ {
			bits = append(bits, byte((v>>i)&1))
		}
	}
	put(ref, refBits)
	put(value, width)
	put(0, refBits)
	out := []byte{2, 0}
	var cur byte
	for i, b := range bits {
		cur |= b << (i & 7)
		if i&7 == 7 {
			out = append(out, cur)
			cur = 0
		}
	}
	if len(bits)&7 != 0 {
		out = append(out, cur)
	}
	return out
}

// TestLuaLoadMessagesSkipCostFollowsTheWire holds docs/SPEC-TABLES.md §3.3:
// skipping an array entry this reader cannot name costs time bounded by the
// bits the batch carries, never by the count the announcement claims. The
// three-byte batch names a zero-width array once; its cost at two billion or
// so claimed elements is the same arithmetic step as at a thousand. A
// fixed-width run that passes the bits left is refused, and a lawful skipped
// list of real elements still reads.
func TestLuaLoadMessagesSkipCostFollowsTheWire(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "skiprun.schema")
	if err := os.WriteFile(path, []byte(luaSkipRunSchema), 0o600); err != nil {
		t.Fatal(err)
	}
	u := luarun.LoadUnit(t, path)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	quantEntry, quantCount, quantBits := luaSkipRunQuantizedEntry(t, 1)
	if quantCount+1 >= uint32(1)<<uint(quantBits) {
		t.Fatalf("the quantized count %d does not leave room for an index above it in %d bits", quantCount, quantBits)
	}
	quantRefBits := ir.TableMessageRefBits(1)
	quantOK := luaSkipRunBatch(t, quantRefBits, 1, uint64(quantCount/2), quantBits)
	quantOver := luaSkipRunBatch(t, quantRefBits, 1, uint64(quantCount+1), quantBits)

	inputs := map[string][]byte{
		"ann_small": luaSkipRunAnnouncement(t, luaSkipRunArrayEntry(t, 1000, 0, 5)),
		"ann_large": luaSkipRunAnnouncement(t, luaSkipRunArrayEntry(t, 4000000000, 0, 5)),
		"ann_over":  luaSkipRunAnnouncement(t, luaSkipRunFixedEntry(t, 1000)),
		"ann_list":  luaSkipRunAnnouncement(t, luaSkipRunArrayEntry(t, 2, 8, 0)),
		"ann_quant": luaSkipRunAnnouncement(t, quantEntry),
		// form 2, one body, the unknown entry's reference, no count, the
		// zero-width elements, the body's terminator, then the pad
		"batch": {2, 0, 1},
		// the same framing with two eight-bit elements between the reference
		// and the terminator
		"batch_list":       {2, 0, 1, 0, 0},
		"batch_quant":      quantOK,
		"batch_quant_over": quantOver,
	}

	probe := fmt.Sprintf(`local T = require(%q)
local inputs = require("inputs")

local function load(ann, data, cap)
  local vocab = {}
  for i = 1, T.MessageEntries do vocab[i] = {} end
  local report = {}
  assert(T.AnnounceRead(vocab, ann, report), "announcement refused: " .. tostring(report.reason))
  local steps = 0
  debug.sethook(function()
    steps = steps + 1
    if cap > 0 and steps > cap then
      error("instruction cap", 0)
    end
  end, "", 1)
  local batch = {}
  local ok, got = pcall(T.LoadMessages, "Probe", vocab, data, batch)
  debug.sethook()
  if not ok then return nil, -1, tostring(got) end
  if got == nil or #got ~= 1 then return nil, steps, tostring(batch.reason) end
  return got, steps
end

local small, small_steps = load(inputs.ann_small, inputs.batch, 0)
assert(small ~= nil, "a lawful zero-width array was refused")
local large, large_steps, large_err = load(inputs.ann_large, inputs.batch, small_steps * 2 + 1000)
assert(large ~= nil, "a lawful zero-width array under a wide count was refused: " .. tostring(large_err))
local over = load(inputs.ann_over, inputs.batch, 0)
assert(over == nil, "a fixed-width run past the bits left was accepted")
local list = load(inputs.ann_list, inputs.batch_list, 0)
assert(list ~= nil, "a lawful unknown list of real elements was refused")
local quant, quant_steps, quant_err = load(inputs.ann_quant, inputs.batch_quant, 0)
assert(quant ~= nil, "a lawful quantized array was refused: " .. tostring(quant_err))
local quant_over = load(inputs.ann_quant, inputs.batch_quant_over, 0)
assert(quant_over == nil, "a quantized index above its count was accepted")
print(small_steps, large_steps, quant_steps)
`, ModuleName(u))

	out, stderr, err := try(t, files, probe, inputs)
	if err != nil {
		t.Fatalf("probe: %v\n%s", err, stderr)
	}
	var small, large, quant int
	if _, err := fmt.Sscanf(strings.TrimSpace(out), "%d %d %d", &small, &large, &quant); err != nil || small == 0 || quant == 0 {
		t.Fatalf("probe printed %q", out)
	}
	t.Logf("zero-width skip: %d VM instructions at 1000 elements, %d at 4000000000", small, large)
	t.Logf("quantized skip: %d VM instructions", quant)
	if large > 2*small {
		t.Errorf("the skip costs %d instructions at 4000000000 elements and %d at 1000: the cost follows the count", large, small)
	}
}
