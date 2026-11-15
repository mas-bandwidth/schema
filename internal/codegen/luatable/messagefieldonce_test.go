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

const luaMsgFieldOnceSchema = `package msgfieldonce

enum Mode { Run, Fly, Rest }

fixed table Msg {
    x int32
    by [Mode]int32
}
`

type luaMsgFieldOnceBundle struct {
	files    map[string][]byte
	module   string
	model    *tabletext.Model
	root     *ir.Struct
	vocab    tablewire.Vocabulary
	announce []byte
	inputA   []byte
	inputB   []byte
	inputKey []byte
}

// buildLuaMsgFieldOnce generates the unit, takes the leg's own lawful
// SaveMessages batches for the scalar field, and a lawful keyed batch from the
// reference engine.
func buildLuaMsgFieldOnce(t *testing.T) luaMsgFieldOnceBundle {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "msgfieldonce.schema")
	writeFile(t, path, luaMsgFieldOnceSchema)
	u := luarun.LoadUnit(t, path)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	module := ModuleName(u)

	probe := fmt.Sprintf(`local T = require(%q)
local function hex(s)
  return (s:gsub(".", function(c) return string.format("%%02x", string.byte(c)) end))
end
local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
assert(T.AnnounceRead(vocab, T.Announcement, {}), "AnnounceRead")

local a = T.NewMsg()
a.x = 7
local wa = assert(T.SaveMessages("Msg", vocab, { a }), "save x=7")

local b = T.NewMsg()
b.x = 9
local wb = assert(T.SaveMessages("Msg", vocab, { b }), "save x=9")

local ra = {}
local ga = T.LoadMessages("Msg", vocab, wa, ra)
assert(ga ~= nil and #ga == 1 and ga[1].x == 7, "the lawful batch with x=7 is not read")
assert(not ra.damaged and ra.refused == 0, "the lawful batch with x=7 is not silent")

print("ANNOUNCE=" .. hex(T.Announcement))
print("A=" .. hex(wa))
print("B=" .. hex(wb))
`, module)

	out, stderr, err := try(t, files, probe, nil)
	if err != nil {
		t.Fatalf("SaveMessages probe failed: %v\nout=%s\nstderr=%s", err, out, stderr)
	}
	kv := map[string][]byte{}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			kv[k] = unhexTestBytes(t, v)
		}
	}

	var vocab tablewire.Vocabulary
	var report tabletext.Report
	if err := vocab.AnnounceRead(kv["ANNOUNCE"], &report); err != nil {
		t.Fatalf("the leg's announcement is refused: %v", err)
	}

	model := tabletext.NewModel(u)
	root := model.Lookup("Msg")
	inst := model.New(root)
	var keyedReport tabletext.Report
	if !model.Read(inst, []byte(`{"by":{"Run":11,"Fly":22,"Rest":33}}`), &keyedReport) || !keyedReport.Silent() {
		t.Fatalf("the keyed sample is not silent: %+v", keyedReport)
	}
	inputKey, err := tablewire.EncodeMessage(model, inst)
	if err != nil {
		t.Fatalf("the reference engine refused the keyed batch: %v", err)
	}

	return luaMsgFieldOnceBundle{
		files:    files,
		module:   module,
		model:    model,
		root:     root,
		vocab:    vocab,
		announce: kv["ANNOUNCE"],
		inputA:   kv["A"],
		inputB:   kv["B"],
		inputKey: inputKey,
	}
}

func msgBit(data []byte, at int) int {
	return int(data[at>>3]>>uint(at&7)) & 1
}

func msgSetBit(data []byte, at, bit int) {
	if bit != 0 {
		data[at>>3] |= 1 << uint(at&7)
	} else {
		data[at>>3] &^= 1 << uint(at&7)
	}
}

func msgCopyBits(dst []byte, dstat int, src []byte, srcat, n int) {
	for i := range n {
		msgSetBit(dst, dstat+i, msgBit(src, srcat+i))
	}
}

// msgBodyEnd is the absolute bit offset of a batch-of-one's body terminator.
func msgBodyEnd(t *testing.T, b luaMsgFieldOnceBundle, wire []byte) int {
	t.Helper()
	spots := tablewire.MessageSpots(b.model, b.root, wire, &b.vocab)
	if len(spots) == 0 {
		t.Fatal("the lawful batch carries no reference spots")
	}
	last := spots[len(spots)-1]
	if last.Value != 0 {
		t.Fatalf("the last reference of a batch-of-one is %d, not the terminator", last.Value)
	}
	return 8 + last.Off
}

// fieldTwice splices the body of each lawful batch, terminator excluded, under
// one terminator: the body names x twice.
func fieldTwice(t *testing.T, b luaMsgFieldOnceBundle) []byte {
	t.Helper()
	endA := msgBodyEnd(t, b, b.inputA)
	endB := msgBodyEnd(t, b, b.inputB)
	const bodyStart = 16
	nA, nB := endA-bodyStart, endB-bodyStart
	n := bodyStart + nA + nB + b.vocab.RefBits()
	out := make([]byte, (n+7)/8)
	msgSetBit(out, 1, 1)
	msgCopyBits(out, bodyStart, b.inputA, bodyStart, nA)
	msgCopyBits(out, bodyStart+nA, b.inputB, bodyStart, nB)
	return out
}

// slotTwice copies the first keyed slot's key reference over the second's, so
// one enum-keyed array names Run twice.
func slotTwice(t *testing.T, b luaMsgFieldOnceBundle) []byte {
	t.Helper()
	spots := tablewire.MessageSpots(b.model, b.root, b.inputKey, &b.vocab)
	if len(spots) < 4 {
		t.Fatalf("the lawful keyed batch carries %d spots, want the field, the keys and the terminator", len(spots))
	}
	field, run, fly := spots[0], spots[1], spots[2]
	if field.Value == 0 || run.Value == 0 || fly.Value == 0 || run.Value == fly.Value {
		t.Fatalf("unexpected keyed spots: field=%d run=%d fly=%d", field.Value, run.Value, fly.Value)
	}
	out := append([]byte(nil), b.inputKey...)
	for i := 0; i < fly.Width; i++ {
		msgSetBit(out, 8+fly.Off+i, msgBit(b.inputKey, 8+run.Off+i))
	}
	return out
}

func TestLuaLoadMessagesFieldNamedOnce(t *testing.T) {
	t.Parallel()
	b := buildLuaMsgFieldOnce(t)
	fieldOnce := fieldTwice(t, b)
	keyedOnce := slotTwice(t, b)

	// THE GO REFERENCE REFUSES BOTH: the same bytes must not read in either leg.
	for name, wire := range map[string][]byte{"field": fieldOnce, "keyed": keyedOnce} {
		var report tabletext.Report
		vocab := b.vocab
		if ok, err := tablewire.DecodeMessage(b.model, b.model.New(b.root), wire, &vocab, &report); ok {
			t.Fatalf("the Go engine accepted a body naming a %s twice: err=%v", name, err)
		}
	}

	probe := fmt.Sprintf(`local T = require(%q)
local inputs = require("inputs")
local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
local ar = {}
assert(T.AnnounceRead(vocab, inputs.announce, ar), "AnnounceRead: " .. tostring(ar.reason))

local rLawful = {}
local lawful = T.LoadMessages("Msg", vocab, inputs.lawful, rLawful)
assert(lawful ~= nil and #lawful == 1 and lawful[1].x == 7, "the lawful batch is not read")
assert(not rLawful.damaged and rLawful.refused == 0, "the lawful batch is not silent")

local rKeyLawful = {}
local keyLawful = T.LoadMessages("Msg", vocab, inputs.keyedLawful, rKeyLawful)
assert(keyLawful ~= nil and #keyLawful == 1 and keyLawful[1].by[1] == 11 and keyLawful[1].by[2] == 22
  and keyLawful[1].by[3] == 33, "the lawful keyed batch is not read")
assert(not rKeyLawful.damaged and rKeyLawful.refused == 0, "the lawful keyed batch is not silent")

local rField = {}
local field = T.LoadMessages("Msg", vocab, inputs.field, rField)
assert(field ~= nil and #field == 0, "a body naming x twice was not refused")
assert(rField.damaged or rField.refused ~= 0, "a body naming x twice was not damage")

local rKeyed = {}
local keyed = T.LoadMessages("Msg", vocab, inputs.keyed, rKeyed)
assert(keyed ~= nil and #keyed == 0, "a body naming a keyed slot twice was not refused")
assert(rKeyed.damaged or rKeyed.refused ~= 0, "a body naming a keyed slot twice was not damage")

print("field-once-ok")
`, b.module)

	inputs := map[string][]byte{
		"announce":    b.announce,
		"lawful":      b.inputA,
		"keyedLawful": b.inputKey,
		"field":       fieldOnce,
		"keyed":       keyedOnce,
	}

	out, stderr, err := try(t, b.files, probe, inputs)
	if err != nil || strings.TrimSpace(out) != "field-once-ok" {
		t.Fatalf("message field-once probe failed: %v\nout=%s\nstderr=%s", err, out, stderr)
	}
}
