package luatable

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

// THE SKIP WALK IS BOUNDED AT DEPTH 128 (docs/SPEC-TABLES.md §3.3,
// message_skip_depth): a nested body past 128 levels is refused as damage, and
// LoadMessages answers a value or a refusal without raising (§6.5).
const luaSkipDepthSender = `package sender

fixed table Root {
    safe Probe
    doomed Probe
}

fixed table Probe {
    x uint8
}
`

const luaSkipDepthReceiver = `package receiver

fixed table Root {
    safe Probe
}

fixed table Probe {
    x uint8
}
`

func loadLuaSkipDepthUnit(t *testing.T, name, schema string) *ir.Unit {
	t.Helper()
	path := filepath.Join(t.TempDir(), name+".schema")
	writeFile(t, path, schema)
	return luarun.LoadUnit(t, path)
}

// luaSkipBatch packs one message body of refBits-wide references, form byte 2
// and a count of one body.
func luaSkipBatch(refs []uint64, refBits int) []byte {
	out := make([]byte, 2+(len(refs)*refBits+7)/8)
	out[0] = 2
	out[1] = 0
	at := 16
	for _, ref := range refs {
		for b := range refBits {
			if (ref>>uint(b))&1 != 0 {
				out[at>>3] |= 1 << uint(at&7)
			}
			at++
		}
	}
	return out
}

// luaSkipDeepBatch fills exactly size bytes with references to slot and no
// terminator: the depth bound refuses it before the walk reaches the end.
func luaSkipDeepBatch(size, refBits int, slot uint64) []byte {
	out := make([]byte, size)
	out[0] = 2
	out[1] = 0
	at := 16
	for at+refBits <= size*8 {
		for b := range refBits {
			if (slot>>uint(b))&1 != 0 {
				out[at>>3] |= 1 << uint(at&7)
			}
			at++
		}
	}
	return out
}

func TestLuaLoadMessagesSkipDepthBound(t *testing.T) {
	t.Parallel()
	sender := loadLuaSkipDepthUnit(t, "Sender", luaSkipDepthSender)
	receiver := loadLuaSkipDepthUnit(t, "Receiver", luaSkipDepthReceiver)
	vocabEntries := tablewire.MessageVocabulary(sender)
	slot := uint64(0)
	for i, e := range vocabEntries {
		if e.Id == ir.TableWireId("doomed") {
			if e.Kind != ir.TableKindTable {
				t.Fatalf("doomed entry kind is %d, want table", e.Kind)
			}
			slot = uint64(i + 1)
		}
	}
	if slot == 0 {
		t.Fatal("the sender vocabulary carries no doomed entry")
	}
	refBits := ir.TableMessageRefBits(len(vocabEntries))

	nested := func(levels int) []byte {
		refs := make([]uint64, 0, 2*levels+1)
		for range levels {
			refs = append(refs, slot)
		}
		for range levels + 1 {
			refs = append(refs, 0)
		}
		return luaSkipBatch(refs, refBits)
	}

	model := tabletext.NewModel(sender)
	inst := model.New(model.Lookup("Root"))
	var report tabletext.Report
	if !model.Read(inst, []byte(`{"safe":{"x":7}}`), &report) || !report.Silent() {
		t.Fatalf("sender value: %+v", report)
	}
	lawful, err := tablewire.EncodeMessages(model, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatal(err)
	}

	inputs := map[string][]byte{
		"announce":   tablewire.Announce(sender),
		"nested128":  nested(128),
		"nested129":  nested(129),
		"deep40000":  luaSkipDeepBatch(40000, refBits, slot),
		"deep200000": luaSkipDeepBatch(200000, refBits, slot),
		"lawful":     lawful,
	}
	files, err := Generate(receiver)
	if err != nil {
		t.Fatal(err)
	}

	probe := fmt.Sprintf(`local T = require(%q)
local inputs = require("inputs")
local vocab, ar = {}, {}
assert(T.AnnounceRead(vocab, inputs.announce, ar), "announcement refused: " .. tostring(ar.reason))
local function load(name)
  local report = {}
  local ok, values = pcall(T.LoadMessages, "Root", vocab, inputs[name], report)
  if not ok then return "raised" end
  if values ~= nil and #values == 1 then return "accepted", values[1] end
  return "refused"
end
local v128 = load("nested128")
print("nested128=" .. v128)
local v129 = load("nested129")
print("nested129=" .. v129)
local d40 = load("deep40000")
print("deep40000=" .. d40)
local d200 = load("deep200000")
print("deep200000=" .. d200)
local lawful, value = load("lawful")
print("lawful=" .. lawful)
if lawful == "accepted" then
  assert(value.safe ~= nil and value.safe.x == 7, "lawful nested table lost")
end
`, ModuleName(receiver))

	out, stderr, err := try(t, files, probe, inputs)
	if err != nil {
		t.Fatalf("skip depth probe failed: %v\nout=%s\nstderr=%s", err, out, stderr)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			got[k] = v
		}
	}
	for name, want := range map[string]string{
		"nested128":  "accepted",
		"nested129":  "refused",
		"deep40000":  "refused",
		"deep200000": "refused",
		"lawful":     "accepted",
	} {
		if got[name] != want {
			t.Errorf("%s: %s, want %s", name, got[name], want)
		}
	}
}
