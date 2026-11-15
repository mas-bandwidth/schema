// TestLuaLoadMessagesOneEncoding pins docs/SPEC-TABLES.md §3.3's one-encoding
// rule on the Lua message read: a batch has exactly one encoding for its value,
// so LoadMessages reads the canonical batch and refuses a body that writes a
// field at the default its writer elides, writes a record's fields out of
// declaration order, or writes a union field with the None reference. The
// vectors and their verdicts are testdata/wire/message-one-encoding/'s
// manifest (docs/rulings.json `message-one-encoding`); the refusal's reason is
// per-language detail, so this test asserts the verdict alone. It also pins
// the write side: SaveMessages of the same value reproduces the canonical
// batch's bytes.
package luatable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type luaMessageOneEncodingRow struct {
	file    string
	verdict string
}

// luaMessageOneEncodingManifest reads the vectors' manifest table: every row
// names one file beside its verdict.
func luaMessageOneEncodingManifest(t *testing.T, path string) []luaMessageOneEncodingRow {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("message-one-encoding manifest: %v", err)
	}
	var rows []luaMessageOneEncodingRow
	for line := range strings.SplitSeq(string(data), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 4 {
			continue
		}
		file := strings.Trim(strings.TrimSpace(cells[1]), "`")
		verdict := strings.TrimSpace(cells[3])
		if !strings.HasSuffix(file, ".bin") || (verdict != "read" && verdict != "refused") {
			continue
		}
		rows = append(rows, luaMessageOneEncodingRow{file: file, verdict: verdict})
	}
	if len(rows) == 0 {
		t.Fatal("message-one-encoding manifest names no vector rows")
	}
	return rows
}

func TestLuaLoadMessagesOneEncoding(t *testing.T) {
	t.Parallel()

	repo := filepath.Join("..", "..", "..")
	vectors := filepath.Join(repo, "testdata", "wire", "message-one-encoding")
	rows := luaMessageOneEncodingManifest(t, filepath.Join(vectors, "README.md"))

	var batches []luaMessageOneEncodingRow
	for _, row := range rows {
		if row.file == "announcement.bin" {
			continue // the announcement is read by AnnounceRead, not a batch
		}
		batches = append(batches, row)
	}
	if len(batches) != 4 {
		t.Fatalf("message-one-encoding manifest names %d batch rows, want 4", len(batches))
	}
	canonical := ""
	for _, row := range batches {
		if row.file == "canonical.bin" {
			if row.verdict != "read" {
				t.Fatalf("message-one-encoding manifest gives canonical.bin the verdict %q, want read", row.verdict)
			}
			canonical = row.file
		}
	}
	if canonical == "" {
		t.Fatal("message-one-encoding manifest names no canonical.bin row")
	}

	unit := luarun.LoadUnit(t, filepath.Join(repo, "test", "message-one-encoding", "One.schema"))
	files, err := Generate(unit)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	inputs := map[string][]byte{"announcement.bin": readFile(t, filepath.Join(vectors, "announcement.bin"))}
	var luaRows strings.Builder
	for _, row := range batches {
		inputs[row.file] = readFile(t, filepath.Join(vectors, row.file))
		fmt.Fprintf(&luaRows, "\t{%q, %q},\n", row.file, row.verdict)
	}

	probe := fmt.Sprintf(`local T = require(%q)
local inputs = require("inputs")

local vocab = {}
for i = 1, T.MessageEntries do vocab[i] = {} end
local ar = {}
assert(T.AnnounceRead(vocab, inputs["announcement.bin"], ar), "announcement refused or damaged")

local cases = {
%s}
for _, c in ipairs(cases) do
  local r = {}
  local values = T.LoadMessages("One", vocab, inputs[c[1]], r)
  if values == nil or #values == 0 then
    print(c[1] .. "=refused")
  else
    local v = values[1]
    local tag = (v.pick and v.pick.tag) or 0
    print(c[1] .. "=read:" .. #values .. ":" .. tostring(v.mode) .. ":" .. tostring(v.count) .. ":" .. tostring(tag))
  end
end

local m = T.NewOne()
m.count = 5
local saved = assert(T.SaveMessages("One", vocab, { m }), "SaveMessages refused the canonical value")
print("saved=" .. (saved:gsub(".", function(ch) return string.format("%%02x", ch:byte()) end)))
`, ModuleName(unit), luaRows.String())

	out, stderr, err := try(t, files, probe, inputs)
	if err != nil {
		t.Fatalf("message-one-encoding probe failed: %v\nstderr:\n%s\nstdout:\n%s", err, stderr, out)
	}

	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			got[k] = v
		}
	}

	for _, row := range batches {
		want := "refused"
		if row.verdict == "read" {
			// The manifest's value: one body, `mode` at its declared default
			// 7, `count` 5 and `pick` at None.
			want = "read:1:7:5:0"
		}
		if got[row.file] != want {
			t.Errorf("%s: LoadMessages = %s, want %s", row.file, got[row.file], want)
		}
	}

	saved, hasSaved := got["saved"]
	canonicalBytes := inputs[canonical]
	var wantHex strings.Builder
	for _, b := range canonicalBytes {
		fmt.Fprintf(&wantHex, "%02x", b)
	}
	if !hasSaved {
		t.Fatal("the probe printed no SaveMessages bytes")
	}
	if saved != wantHex.String() {
		t.Errorf("SaveMessages of the canonical value = %s, want canonical.bin %s", saved, wantHex.String())
	}
}

func TestLuaLoadMessagesOneEncodingTracksUnknownFields(t *testing.T) {
	t.Parallel()

	load := func(name, schema string) *ir.Unit {
		path := filepath.Join(t.TempDir(), name+".schema")
		writeFile(t, path, schema)
		return luarun.LoadUnit(t, path)
	}
	sender := load("Sender", `package sender
fixed table One {
    first uint8
    hidden uint8
    last uint8
}
`)
	receiver := load("Receiver", `package receiver
fixed table One {
    first uint8
    last uint8
}
`)
	model := tabletext.NewModel(sender)
	root := model.Lookup("One")
	inst := model.New(root)
	var modelReport tabletext.Report
	if !model.Read(inst, []byte(`{"first": 1, "hidden": 2, "last": 3}`), &modelReport) || !modelReport.Silent() {
		t.Fatalf("sender value: %+v", modelReport)
	}
	batch, err := tablewire.EncodeMessages(model, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatalf("encode canonical sender batch: %v", err)
	}
	var vocab tablewire.Vocabulary
	if err := vocab.AnnounceRead(tablewire.Announce(sender), &modelReport); err != nil {
		t.Fatalf("read sender announcement: %v", err)
	}
	spots := tablewire.MessageSpots(model, root, batch, &vocab)
	if len(spots) != 4 || spots[0].Value == 0 || spots[1].Value == 0 || spots[2].Value == 0 || spots[3].Value != 0 {
		t.Fatalf("sender references = %+v, want three fields then terminator", spots)
	}
	// Swap the unknown middle field and the known last field. Their uint8
	// payloads have equal widths, so the body remains parseable; only its
	// writer order is wrong.
	bad := append([]byte(nil), batch...)
	first, second := 8+spots[1].Off, 8+spots[2].Off
	for bit := 0; bit < spots[1].Width; bit++ {
		a, b := msgBit(bad, first+bit), msgBit(bad, second+bit)
		msgSetBit(bad, first+bit, b)
		msgSetBit(bad, second+bit, a)
	}

	files, err := Generate(receiver)
	if err != nil {
		t.Fatalf("Generate receiver: %v", err)
	}
	probe := fmt.Sprintf(`local T = require(%q)
local inputs = require("inputs")
local vocab, report = {}, {}
for i = 1, T.MessageEntries do vocab[i] = {} end
assert(T.AnnounceRead(vocab, inputs.announce, report), "sender announcement refused")
local values = T.LoadMessages("One", vocab, inputs.batch, report)
if values == nil or #values == 0 then print("refused") else print("read") end
`, ModuleName(receiver))
	out, stderr, err := try(t, files, probe, map[string][]byte{
		"announce": tablewire.Announce(sender),
		"batch":    bad,
	})
	if err != nil {
		t.Fatalf("unknown-field order probe failed: %v\nstderr:\n%s\nstdout:\n%s", err, stderr, out)
	}
	if strings.TrimSpace(out) != "refused" {
		t.Fatalf("LoadMessages = %q, want refusal when an unknown field is out of writer order", strings.TrimSpace(out))
	}
}

func luaOneEncodingFieldID(t *testing.T, unit *ir.Unit, name string) uint64 {
	t.Helper()
	search := func(st *ir.Struct) (uint64, bool) {
		for _, f := range st.Fields {
			if f.Name == name {
				return ir.TableFieldWireId(f), true
			}
		}
		return 0, false
	}
	for _, st := range unit.Tables {
		if id, ok := search(st); ok {
			return id
		}
	}
	for _, st := range unit.Structs {
		if id, ok := search(st); ok {
			return id
		}
	}
	t.Fatalf("schema names no field %q", name)
	return 0
}

func luaOneEncodingSlot(t *testing.T, unit *ir.Unit, id uint64) uint64 {
	t.Helper()
	for i, e := range tablewire.MessageVocabulary(unit) {
		if e.Id == id {
			return uint64(i + 1)
		}
	}
	t.Fatalf("vocabulary names no entry for id %#x", id)
	return 0
}

func luaOneEncodingPayload(t *testing.T, spots []tablewire.BitSpot, slot uint64) int {
	t.Helper()
	for _, s := range spots {
		if s.Value == slot {
			return 8 + s.Off + s.Width
		}
	}
	t.Fatalf("batch carries no reference to slot %d: %+v", slot, spots)
	return 0
}

// TestLuaLoadMessagesOneEncodingCanonicalFields pins the elision and presence
// rows §3.3 names beside the manifest's integer default: a bool, an array and
// a nested body written at the value a conforming writer elides are damage, a
// present optional is read with its presence whatever its payload holds, and a
// compressed float's canonical index is read even where it dequantizes to the
// declared default.
func TestLuaLoadMessagesOneEncodingCanonicalFields(t *testing.T) {
	t.Parallel()

	type row struct {
		name   string
		schema string
		value  string
		patch  string
		width  int
		bits   uint64
		probe  string
		want   string
	}
	rows := []row{
		{
			name:   "present optional int at its default",
			schema: "fixed table Root {\n  opt ?int32\n}\n",
			value:  `{"opt":0}`,
			probe:  `print(("opt=%s/%s"):format(tostring(v.opt), tostring(v.opt_present)))`,
			want:   "opt=0/true",
		},
		{
			name:   "present optional enum at its default",
			schema: "enum Grade {\n  A\n  B\n}\nfixed table Root {\n  grade ?Grade\n}\n",
			value:  `{"grade":"None"}`,
			probe:  `print(("grade=%s/%s"):format(tostring(v.grade), tostring(v.grade_present)))`,
			want:   "grade=nil/true",
		},
		{
			name:   "explicit bool default",
			schema: "fixed table Root {\n  flag bool\n}\n",
			value:  `{"flag":true}`,
			patch:  "flag",
			width:  1,
			want:   "refused",
		},
		{
			name:   "explicit all-default array",
			schema: "fixed table Root {\n  xs [4]int16\n}\n",
			value:  `{"xs":[1,0,0,0]}`,
			patch:  "xs",
			width:  16,
			want:   "refused",
		},
		{
			name:   "explicit all-default nested body",
			schema: "fixed table Root {\n  inner Inner\n}\nfixed table Inner {\n  n uint8\n}\n",
			value:  `{"inner":{"n":1}}`,
			patch:  "n",
			width:  8,
			want:   "refused",
		},
		{
			name:   "compressed float at its default index",
			schema: "fixed table Root {\n  cf float32 | min = 0.0, max = 10.0, resolution = 0.01\n}\n",
			value:  `{"cf":0.001}`,
			probe:  `print("cf=" .. tostring(v.cf))`,
			want:   "cf=0.0",
		},
	}

	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "One.schema")
			writeFile(t, path, "package luaoneencoding\n"+r.schema)
			unit := luarun.LoadUnit(t, path)
			model := tabletext.NewModel(unit)
			root := model.Lookup("Root")
			inst := model.New(root)
			var report tabletext.Report
			if !model.Read(inst, []byte(r.value), &report) || !report.Silent() {
				t.Fatalf("source %s: %+v", r.value, report)
			}
			batch, err := tablewire.EncodeMessages(model, []*tabletext.Instance{inst})
			if err != nil {
				t.Fatalf("encode canonical batch: %v", err)
			}
			if r.patch != "" {
				var vocab tablewire.Vocabulary
				var announced tabletext.Report
				if err := vocab.AnnounceRead(tablewire.Announce(unit), &announced); err != nil {
					t.Fatalf("read announcement: %v", err)
				}
				spots := tablewire.MessageSpots(model, root, batch, &vocab)
				slot := luaOneEncodingSlot(t, unit, luaOneEncodingFieldID(t, unit, r.patch))
				at := luaOneEncodingPayload(t, spots, slot)
				for bit := 0; bit < r.width; bit++ {
					msgSetBit(batch, at+bit, int(r.bits>>bit&1))
				}
			}

			files, err := Generate(unit)
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			probe := fmt.Sprintf(`local T = require(%q)
local inputs = require("inputs")
local vocab, report = {}, {}
for i = 1, T.MessageEntries do vocab[i] = {} end
assert(T.AnnounceRead(vocab, inputs.announce, report), "announcement refused")
local values = T.LoadMessages("Root", vocab, inputs.batch, report)
if values == nil or #values == 0 then
  print("refused")
else
  local v = values[1]
  %s
end
`, ModuleName(unit), r.probe)
			out, stderr, err := try(t, files, probe, map[string][]byte{
				"announce": tablewire.Announce(unit),
				"batch":    batch,
			})
			if err != nil {
				t.Fatalf("probe failed: %v\nstderr:\n%s\nstdout:\n%s", err, stderr, out)
			}
			if got := strings.TrimSpace(out); got != r.want {
				t.Errorf("LoadMessages = %q, want %q (stderr %q)", got, r.want, stderr)
			}
		})
	}
}
