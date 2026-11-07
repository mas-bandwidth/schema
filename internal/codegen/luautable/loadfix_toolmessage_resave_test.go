package luautable_test

// CAUSE G of the Luau wire-fuzz differential: on messagedemo:ToolMessage the
// tolerant load kept an optional array (or a union's array arm) whose body
// carried a FOREIGN ELEMENT KIND, or whose count was damaged, and the save
// wrote it back where the engine (internal/tablewire) leaves it absent/None.
// The regression rebuilds the mutant of seed 0x74c532fb72d33d6f (unit
// messagedemo, root ToolMessage, case message_trace) and requires the six
// counters, the verdict and the re-saved bytes to agree (docs/SPEC-TABLES.md
// §2.3, §2.6, §3, §4.2).

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// loadFixGSplitmix64 is the splitmix64 generator the differential uses, so
// this card's regression rebuilds the same mutant from the same seed.
type loadFixGSplitmix64 struct{ state uint64 }

func (r *loadFixGSplitmix64) next() uint64 {
	r.state += 0x9E3779B97F4A7C15
	z := r.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// loadFixGMutate reproduces the differential's one-to-four-byte mutation of
// base for a seed (docs/PORT-LUAU.md, THE CARD MAP: schema-luau-fuzz-
// differential).
func loadFixGMutate(base []byte, seed uint64) []byte {
	r := &loadFixGSplitmix64{state: seed}
	w := append([]byte(nil), base...)
	if len(w) == 0 {
		return []byte{byte(r.next())}
	}
	edits := 1 + int(r.next()%4)
	for range edits {
		pos := int(r.next() % uint64(len(w)))
		switch r.next() % 3 {
		case 0:
			w[pos] = byte(r.next())
		case 1:
			w[pos] ^= byte(1 << (r.next() % 8))
		default:
			w[pos] = []byte{0x00, 0x01, 0x7f, 0x80, 0xff}[r.next()%5]
		}
	}
	if r.next()%8 == 0 {
		w = w[:int(r.next()%uint64(len(w)))]
	}
	return w
}

// loadFixGCounts is the report's six counters.
type loadFixGCounts struct {
	unknown, kindMismatch, widened, clamped, duplicate int
	malformed                                          bool
}

// loadFixGAnswer is one leg's answer: the six counters, the verdict, and the
// re-saved bytes (or a refused save).
type loadFixGAnswer struct {
	loaded, refused bool
	saveFail        bool
	counts          loadFixGCounts
	saved           []byte
}

// loadFixGProbe is the pinned interpreter's answer for one wire: the
// harness's 47-byte header (loaded, five counters, malformed, refused,
// measure -1, retained, retain_lost, saved length) and the saved bytes.
const loadFixGProbe = `local S = require("./schema_tables")
local load = require("./schema_load")
local save = require("./schema_save")
local inputs = require("./inputs")

local function hex(b)
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end

local report = {
	malformed = false, refused = 0, unknown = 0, kindMismatch = 0,
	clamped = 0, widened = 0, duplicate = 0,
}
local desc = S.lookup("ToolMessage")
local v = load.loadTolerant("ToolMessage", inputs.wire, report)
if v == nil and desc ~= nil and not desc.variable then v = desc.new() end
local saved = nil
if v ~= nil then saved = save.save(desc, v) end
local n = 47 + (if saved ~= nil then buffer.len(saved) else 0)
local b = buffer.create(n)
buffer.writeu8(b, 0, if v ~= nil then 1 else 0)
buffer.writei32(b, 1, report.unknown or 0)
buffer.writei32(b, 5, report.kindMismatch or 0)
buffer.writei32(b, 9, report.widened or 0)
buffer.writei32(b, 13, report.clamped or 0)
buffer.writei32(b, 17, report.duplicate or 0)
buffer.writeu8(b, 21, if report.malformed then 1 else 0)
buffer.writeu8(b, 22, if (report.refused or 0) > 0 then 1 else 0)
buffer.writeinteger(b, 23, -1i)
buffer.writei32(b, 31, 0)
buffer.writei32(b, 35, 0)
if saved == nil then
	buffer.writeinteger(b, 39, -1i)
else
	buffer.writeinteger(b, 39, integer.create(buffer.len(saved)))
	buffer.copy(b, 47, saved, 0, buffer.len(saved))
end
print(hex(b))
`

// loadFixGParse parses the probe's 47-byte header and saved bytes.
func loadFixGParse(t *testing.T, line string) loadFixGAnswer {
	t.Helper()
	raw, err := hex.DecodeString(strings.TrimSpace(line))
	if err != nil || len(raw) < 47 {
		t.Fatalf("the probe's line is not a 47-byte header in hex: %q", line)
	}
	a := loadFixGAnswer{loaded: raw[0] != 0, refused: raw[22] != 0}
	a.counts = loadFixGCounts{
		unknown:      int(int32(binary.LittleEndian.Uint32(raw[1:]))),
		kindMismatch: int(int32(binary.LittleEndian.Uint32(raw[5:]))),
		widened:      int(int32(binary.LittleEndian.Uint32(raw[9:]))),
		clamped:      int(int32(binary.LittleEndian.Uint32(raw[13:]))),
		duplicate:    int(int32(binary.LittleEndian.Uint32(raw[17:]))),
		malformed:    raw[21] != 0,
	}
	n := int64(binary.LittleEndian.Uint64(raw[39:]))
	if n < 0 {
		a.saveFail = true
		return a
	}
	if int64(len(raw)) != 47+n {
		t.Fatalf("the probe's header names %d saved bytes, the line carries %d", n, len(raw)-47)
	}
	a.saved = raw[47:]
	return a
}

// loadFixGStage generates the messagedemo unit, stages its files in a fresh
// directory, and answers the engine's model and the root's struct.
func loadFixGStage(t *testing.T, units map[string][]string) (string, *tabletext.Model, *ir.Struct) {
	t.Helper()
	paths, err := compiler.GatherPaths(prefix(units["messagedemo"]))
	if err != nil {
		t.Fatalf("unit messagedemo: %v", err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("unit messagedemo does not load: %v", err)
	}
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatalf("unit messagedemo: %v", err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatalf("unit messagedemo: %v", err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := tabletext.NewModel(u)
	def := m.Lookup("ToolMessage")
	if def == nil {
		t.Fatal("messagedemo declares no ToolMessage")
	}
	return dir, m, def
}

// loadFixGEngineFor is the differential's engine oracle for one mutant: a
// variable root's region is measured first, and a framing refusal falls back
// to the plain Decode, exactly as the differential does.
func loadFixGEngineFor(m *tabletext.Model, def *ir.Struct, data []byte) (loadFixGAnswer, bool) {
	decode := tablewire.DecodeRegion
	if m.IsVariable(def.Name) {
		if _, _, refused := tablewire.FileRegionMeasure(m.Unit, def, data); refused {
			decode = tablewire.Decode
		}
	}
	inst := m.New(def)
	var rep tabletext.Report
	ok, derr := decode(m, inst, data, &rep)
	var o loadFixGAnswer
	if tablewire.Refused(derr) {
		o.refused = true
		return o, true
	}
	if derr != nil {
		return o, false
	}
	o.loaded = true
	o.counts = loadFixGCounts{
		unknown:      rep.Unknown,
		kindMismatch: rep.KindMismatch,
		widened:      rep.Widened,
		clamped:      rep.Clamped,
		duplicate:    rep.Duplicate,
		malformed:    rep.Malformed || !ok,
	}
	enc, eerr := tablewire.Encode(m, inst)
	if eerr != nil {
		o.saveFail = true
		return o, true
	}
	o.saved = enc
	return o, true
}

// loadFixGVerdict compares the two legs and names the first disagreement, or
// "" when they agree.
func loadFixGVerdict(a, o loadFixGAnswer) string {
	if o.counts.malformed && !o.refused {
		// DAMAGE DISCARDS THE WHOLE BATCH (docs/SPEC-TABLES.md §4, §3.3): the
		// leg owes the refusal, and no counter or saved byte is compared
		if !a.counts.malformed {
			return fmt.Sprintf("the engine discards a damaged batch; Luau says malformed=%t", a.counts.malformed)
		}
		return ""
	}
	if a.loaded != o.loaded {
		return fmt.Sprintf("the load differs: Luau loaded=%t, the engine loaded=%t", a.loaded, o.loaded)
	}
	if a.refused != o.refused {
		return fmt.Sprintf("the verdict differs: Luau refused=%t, the engine refused=%t", a.refused, o.refused)
	}
	if a.counts != o.counts {
		return fmt.Sprintf("the report differs: Luau says {unknown:%d kindMismatch:%d widened:%d clamped:%d duplicate:%d malformed:%t}, the engine says {unknown:%d kindMismatch:%d widened:%d clamped:%d duplicate:%d malformed:%t}",
			a.counts.unknown, a.counts.kindMismatch, a.counts.widened, a.counts.clamped, a.counts.duplicate, a.counts.malformed,
			o.counts.unknown, o.counts.kindMismatch, o.counts.widened, o.counts.clamped, o.counts.duplicate, o.counts.malformed)
	}
	if a.saveFail != o.saveFail {
		if a.saveFail {
			return fmt.Sprintf("the Luau save refused the value it loaded; the engine encoded %d bytes", len(o.saved))
		}
		return fmt.Sprintf("the engine refused to encode the value it decoded; the Luau save wrote %d bytes", len(a.saved))
	}
	if !a.saveFail && !bytes.Equal(a.saved, o.saved) {
		at := 0
		for at < len(a.saved) && at < len(o.saved) && a.saved[at] == o.saved[at] {
			at++
		}
		return fmt.Sprintf("the decoded value differs: Luau saves %d bytes, the engine %d, first difference at byte %d\n    luau   %x\n    engine %x",
			len(a.saved), len(o.saved), at, a.saved, o.saved)
	}
	return ""
}

// TestLuauToolMessageResaveAgreesWithTheEngine pins cause G of the Luau
// wire-fuzz differential: a foreign element kind leaves an optional array
// ABSENT (§2.3) and a union's array arm None (§2.6, §3), and a damaged count
// leaves the arm None too, so the save writes neither the way the engine does.
// The regression rebuilds the mutant of seed 0x74c532fb72d33d6f (unit
// messagedemo, root ToolMessage, case message_trace), loads it with the Luau
// tolerant load under the pinned interpreter and with the Go engine, and
// requires the six counters, the verdict and the re-saved bytes to agree.
func TestLuauToolMessageResaveAgreesWithTheEngine(t *testing.T) {
	t.Parallel()

	// seed 0x74c532fb72d33d6f, unit messagedemo, root ToolMessage, case
	// message_trace: the mutant it rebuilds, pinned
	const seed = uint64(0x74c532fb72d33d6f)
	const pinHex = "0101080c000000020f030d56040c057472616365050e1a0d0117060805000000020f070d0b080c026869090e021e0000000a0e0a080200000000000000000b0e200d021b0c0d070d0803000000000e0d0d0d08040000000f08060000000000010000100e172d0212110c08626f6f742e6c75610d0801000000000100120e041e02110000a828f581a4ac38aa9d32c9c69be74dcd87f56fdcf4bb96ed39264f47f3d7e865e64975696bd0789458868e6cc4dba4d62862915987c671727e409519eff404faf33e6e3e9326e59d0477754d1cfa8a023a4081a65463dc991f25ad45ad975dee07594f69ade84bbf0a5053f01803f0c287f28b628712535fee9aebc4a69ec5de7606d7eb0d2dc503296e72e1a4d477905c01824217faf9781300000000000000"

	var base []byte
	for _, row := range manifestRows(t) {
		if row.kind == "instance" && row.unit == "messagedemo" && row.root == "ToolMessage" && row.caseName == "message_trace" {
			b, err := os.ReadFile(filepath.Join(root, row.wire))
			if err != nil {
				t.Fatal(err)
			}
			base = b
			break
		}
	}
	if base == nil {
		t.Fatal("message_trace instance of messagedemo:ToolMessage not in the manifest")
	}

	mutant := loadFixGMutate(base, seed)
	if hex.EncodeToString(mutant) != pinHex {
		t.Fatalf("seed %#016x rebuilt a different mutant: got %x want %s", seed, mutant, pinHex)
	}

	units, _ := readManifest(t)
	dir, m, def := loadFixGStage(t, units)

	var mods []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if name := entry.Name(); strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)
	var b strings.Builder
	for _, name := range mods {
		fmt.Fprintf(&b, "require(\"./%s\")\n", name)
	}
	b.WriteString(loadFixGProbe)
	if err := os.WriteFile(filepath.Join(dir, "probe.luau"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	line := luaurun.Run(t, dir, "probe.luau", map[string][]byte{"wire": mutant})
	a := loadFixGParse(t, string(line))
	o, ok := loadFixGEngineFor(m, def, mutant)
	if !ok {
		t.Fatalf("seed %#016x unit messagedemo root ToolMessage case message_trace: the engine refused the root itself", seed)
	}
	if v := loadFixGVerdict(a, o); v != "" {
		t.Errorf("seed %#016x unit messagedemo root ToolMessage case message_trace: %s\n    mutant %x", seed, v, mutant)
	}
}
