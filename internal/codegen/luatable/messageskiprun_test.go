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

	inputs := map[string][]byte{
		"ann_small": luaSkipRunAnnouncement(t, luaSkipRunArrayEntry(t, 1000, 0, 5)),
		"ann_large": luaSkipRunAnnouncement(t, luaSkipRunArrayEntry(t, 4000000000, 0, 5)),
		"ann_over":  luaSkipRunAnnouncement(t, luaSkipRunArrayEntry(t, 1000, 8, 0)),
		"ann_list":  luaSkipRunAnnouncement(t, luaSkipRunArrayEntry(t, 2, 8, 0)),
		// form 2, one body, the unknown entry's reference, no count, the
		// zero-width elements, the body's terminator, then the pad
		"batch": {2, 0, 1},
		// the same framing with two eight-bit elements between the reference
		// and the terminator
		"batch_list": {2, 0, 1, 0, 0},
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
print(small_steps, large_steps)
`, ModuleName(u))

	out, stderr, err := try(t, files, probe, inputs)
	if err != nil {
		t.Fatalf("probe: %v\n%s", err, stderr)
	}
	var small, large int
	if _, err := fmt.Sscanf(strings.TrimSpace(out), "%d %d", &small, &large); err != nil || small == 0 {
		t.Fatalf("probe printed %q", out)
	}
	t.Logf("zero-width skip: %d VM instructions at 1000 elements, %d at 4000000000", small, large)
	if large > 2*small {
		t.Errorf("the skip costs %d instructions at 4000000000 elements and %d at 1000: the cost follows the count", large, small)
	}
}
