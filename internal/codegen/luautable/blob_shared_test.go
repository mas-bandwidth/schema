package luautable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// blobSharedSchema is the probe unit: two *string slots that may name one
// blob, and a bounded string that clamps (docs/SPEC-TABLES.md §2.5, §3.1).
const blobSharedSchema = `package blobprobe

table Probe
{
    note  *string
    note2 *string
    title string(2)
}
`

// blobSlots walks the probe's fields in declaration order and returns how many
// slots name a blob, how many distinct blobs they name, and the first field
// that named a blob another slot already named. The value is what the Go
// reference engine's writers count as blob_shared (§2.5): every extra slot.
func blobSlots(inst *tabletext.Instance) (slots, distinct int, firstRepeat string) {
	seen := map[*tabletext.Blob]bool{}
	for i := range inst.Fields {
		f := &inst.Fields[i]
		if !f.Def.Type.Blob() || f.Cell.Blob == nil {
			continue
		}
		slots++
		if seen[f.Cell.Blob] {
			if firstRepeat == "" {
				firstRepeat = f.Def.Name
			}
			continue
		}
		seen[f.Cell.Blob] = true
		distinct++
	}
	return slots, distinct, firstRepeat
}

// blobWire leb is the form-1 wire (docs/SPEC-TABLES.md §3): a LEB128 integer,
// a little-endian 64-bit value, and the FNV-1a 64 id of a schema name (§5).
func blobLeb(v uint64) []byte {
	var b []byte
	for {
		o := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			o |= 0x80
		}
		b = append(b, o)
		if v == 0 {
			return b
		}
	}
}

func blobU64(v uint64) []byte {
	var b []byte
	for i := 0; i < 8; i++ {
		b = append(b, byte(v>>(8*i)))
	}
	return b
}

func blobFnv(s string) uint64 {
	h := uint64(0xcbf29ce484222325)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= uint64(0x100000001b3)
	}
	return h
}

// blobClampedWire builds the hostile form-1 wire of Probe{note, note2} naming
// one blob record and title = "abc" past its string(2) bound: the read clamps
// title and counts one blob_shared, so the strict read refuses blob_shared
// (§2.5, §6.5). Node index 2 is the one blob record; the id table intern the
// field ids in first-use order and the reserved blob id last.
func blobClampedWire() []byte {
	note, note2, title := blobFnv("note"), blobFnv("note2"), blobFnv("title")
	ids := []uint64{note, note2, title, 0xffffffffffffffff, 0x704be0d8faaffc58}
	var w []byte
	w = append(w, 0x01)
	w = append(w, blobLeb(1)...)
	w = append(w, 0x11)
	w = append(w, blobLeb(2)...)
	w = append(w, blobLeb(2)...)
	w = append(w, 0x11)
	w = append(w, blobLeb(2)...)
	w = append(w, blobLeb(3)...)
	w = append(w, 0x0c)
	w = append(w, blobLeb(3)...)
	w = append(w, "abc"...)
	var sub []byte
	sub = append(sub, blobLeb(1)...)
	sub = append(sub, blobLeb(5)...)
	sub = append(sub, blobLeb(uint64(len("shared")))...)
	sub = append(sub, "shared"...)
	w = append(w, blobLeb(4)...)
	w = append(w, 0x0c)
	w = append(w, blobLeb(uint64(len(sub)))...)
	w = append(w, sub...)
	w = append(w, 0x00)
	for _, id := range ids {
		w = append(w, blobU64(id)...)
	}
	w = append(w, blobU64(uint64(len(ids)))...)
	return w
}

// blobProbeMain is the Luau probe: it loads the shared wire, the
// two-distinct-blobs wire and the clamped wire through the emitted
// load.luau, prints the reports' blobShared (and clamped) and the two
// buffers' identity and bytes, asks refusal.luau's ReadStrict for its name
// and witness, and saves the shared and distinct values through save.luau
// (docs/SPEC-TABLES.md §2.5, §6.5).
func blobProbeMain(g *generatedUnit) string {
	var b strings.Builder
	b.WriteString("local inputs = require(\"./inputs\")\n")
	b.WriteString("local L = require(\"./load\")\nlocal S = require(\"./save\")\nlocal R = require(\"./refusal\")\nlocal St = require(\"./schema_tables\")\n")
	var mods []string
	for name := range g.files {
		if strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)
	for _, m := range mods {
		fmt.Fprintf(&b, "local %s = require(\"./%s\")\n", m, m)
	}
	b.WriteString(`local function hexstr(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
local function fresh()
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0, blobShared = 0 }
end
local function text(b: buffer): string
	return buffer.readstring(b, 0, buffer.len(b))
end
local d = St.lookup("Probe")
local rep = fresh()
local v = L.load("Probe", inputs.shared, rep)
print("shared|" .. tostring(rep.blobShared) .. "|" .. tostring(v.Note == v.Note2) .. "|" .. text(v.Note) .. "|" .. text(v.Note2))
local strict = fresh()
local sv = R.ReadStrict(ProbeTable, "Probe", inputs.shared, strict)
print("strict|" .. tostring(strict.reason) .. "|" .. tostring(strict.field) .. "|" .. tostring(sv == nil))
local sharedOut = S.save(d, v)
print("sharedsave|" .. tostring(sharedOut == nil))
local rep2 = fresh()
local v2 = L.load("Probe", inputs.distinct, rep2)
print("distinctreport|" .. tostring(rep2.blobShared))
local distinctOut = S.save(d, v2)
print("distinct|" .. tostring(distinctOut == nil))
if distinctOut ~= nil then print("distincthex|" .. hexstr(distinctOut)) end
local rep3 = fresh()
local v3 = L.load("Probe", inputs.clamped, rep3)
print("clampedreport|" .. tostring(rep3.blobShared) .. "|" .. tostring(rep3.clamped) .. "|" .. text(v3.Title))
local strict3 = fresh()
local sv3 = R.ReadStrict(ProbeTable, "Probe", inputs.clamped, strict3)
print("clampedstrict|" .. tostring(strict3.reason) .. "|" .. tostring(strict3.field) .. "|" .. tostring(sv3 == nil))
`)
	return b.String()
}

// TestLuauBlobNamedTwice pins the Luau table runtime's blob half of
// docs/SPEC-TABLES.md §2.5 and §6.5 against schema-go's own engine: one blob
// record named by two *string slots loads with report.blobShared 1, the two
// slots hold the same buffer and its bytes, ReadStrict refuses it as
// blob_shared with the second naming field as witness, and the save refuses
// that value. Two equal but DISTINCT buffers are two records and save to the
// Go oracle's two-record wire. A wire that is both clamped and blob-shared
// refuses as blob_shared (BlobShared precedes Clamped in Go's Err()). Every
// expected wire, count and field is computed here from internal/tabletext and
// internal/tablewire, never typed.
func TestLuauBlobNamedTwice(t *testing.T) {
	t.Parallel()
	g := generateFiles(t, map[string]string{"Blob.schema": blobSharedSchema})
	src := filepath.Join(t.TempDir(), "Blob.schema")
	if err := os.WriteFile(src, []byte(blobSharedSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{src})
	if err != nil {
		t.Fatalf("the probe unit does not load: %v", err)
	}
	m := tabletext.NewModel(u)

	// THE SHARED VALUE: the text names one blob, and the second slot is made
	// to name it too, which is exactly the graph the committed blob_shared
	// corpus carries (one record, two slots).
	shared := m.New(m.Lookup("Probe"))
	var readRep tabletext.Report
	if !m.Read(shared, []byte(`{"note": "shared", "note2": "shared", "title": "ab"}`), &readRep) || readRep != (tabletext.Report{}) {
		t.Fatalf("the oracle did not read the shared probe text cleanly: %+v", readRep)
	}
	var note, note2 *tabletext.Field
	for i := range shared.Fields {
		switch shared.Fields[i].Def.Name {
		case "note":
			note = &shared.Fields[i]
		case "note2":
			note2 = &shared.Fields[i]
		}
	}
	if note == nil || note2 == nil {
		t.Fatal("the probe unit declares no note/note2")
	}
	note2.Cell.Blob = note.Cell.Blob
	sharedSlots, sharedDistinct, sharedField := blobSlots(shared)
	wantShared := sharedSlots - sharedDistinct
	if wantShared != 1 || sharedField != "note2" {
		t.Fatalf("the oracle's shared value counts %d extras and first names %q, want 1 and note2", wantShared, sharedField)
	}
	sharedWire, err := tablewire.Encode(m, shared)
	if err != nil {
		t.Fatalf("the oracle did not encode the shared value: %v", err)
	}

	// TWO EQUAL BUT DISTINCT BUFFERS: two records in the oracle's wire.
	distinct := m.New(m.Lookup("Probe"))
	if !m.Read(distinct, []byte(`{"note": "shared", "note2": "shared", "title": "ab"}`), &readRep) || readRep != (tabletext.Report{}) {
		t.Fatalf("the oracle did not read the distinct probe text cleanly: %+v", readRep)
	}
	distinctSlots, distinctDistinct, distinctField := blobSlots(distinct)
	if distinctSlots != 2 || distinctDistinct != 2 || distinctField != "" {
		t.Fatalf("the oracle's distinct value counts %d slots, %d blobs, first repeat %q; want 2, 2, none", distinctSlots, distinctDistinct, distinctField)
	}
	distinctWire, err := tablewire.Encode(m, distinct)
	if err != nil {
		t.Fatalf("the oracle did not encode two distinct blobs: %v", err)
	}
	if string(sharedWire) == string(distinctWire) {
		t.Fatal("the oracle wrote the shared and two-blob values the same bytes")
	}

	// THE CLAMPED WIRE: one blob from two slots and title past its bound. The
	// oracle's own decode sees the clamp and the shared blob, so the strict
	// read names blob_shared (§6.5, BlobShared before Clamped).
	clampedWire := blobClampedWire()
	cinst := m.New(m.Lookup("Probe"))
	var crep tabletext.Report
	if ok, derr := tablewire.Decode(m, cinst, clampedWire, &crep); !ok || derr != nil || crep.Malformed {
		t.Fatalf("the oracle did not decode the clamped wire: ok=%v err=%v report=%+v", ok, derr, crep)
	}
	if crep.Clamped != 1 {
		t.Fatalf("the oracle's clamped wire counts clamped %d, want 1", crep.Clamped)
	}
	cSlots, cDistinct, cField := blobSlots(cinst)
	if cSlots-cDistinct != 1 || cField != "note2" {
		t.Fatalf("the oracle's clamped wire counts %d extras and first names %q, want 1 and note2", cSlots-cDistinct, cField)
	}

	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(blobProbeMain(g)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{
		"shared":   sharedWire,
		"distinct": distinctWire,
		"clamped":  clampedWire,
	}))
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	got := map[string]string{}
	for _, line := range lines {
		key, value, ok := strings.Cut(line, "|")
		if !ok {
			t.Fatalf("probe line %q is not key|value", line)
		}
		got[key] = value
	}
	if want := fmt.Sprintf("%d|true|shared|shared", wantShared); got["shared"] != want {
		t.Errorf("shared load: got %q, want %q (blobShared|same buffer|note|note2)", got["shared"], want)
	}
	if want := fmt.Sprintf("%s|%s|true", "blob_shared", sharedField); got["strict"] != want {
		t.Errorf("strict read: got %q, want %q (reason|witness|refused)", got["strict"], want)
	}
	if got["sharedsave"] != "true" {
		t.Errorf("the save wrote a value whose two slots name one buffer: %q", got["sharedsave"])
	}
	if got["distinctreport"] != "0" {
		t.Errorf("the two-distinct-blobs load counts blobShared %q, want 0", got["distinctreport"])
	}
	if got["distinct"] != "false" {
		t.Errorf("the save refused two equal but distinct buffers: %q", got["distinct"])
	}
	if want := fmt.Sprintf("%x", distinctWire); got["distincthex"] != want {
		t.Errorf("the two-distinct-blobs save is not the oracle's wire:\ngot  %s\nwant %s", got["distincthex"], want)
	}
	if want := fmt.Sprintf("%d|1|ab", cSlots-cDistinct); got["clampedreport"] != want {
		t.Errorf("clamped load: got %q, want %q (blobShared|clamped|title)", got["clampedreport"], want)
	}
	if want := fmt.Sprintf("%s|%s|true", "blob_shared", cField); got["clampedstrict"] != want {
		t.Errorf("clamped strict read: got %q, want %q (reason|witness|refused)", got["clampedstrict"], want)
	}
}
