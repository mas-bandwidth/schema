package luautable_test

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

// loadFixCProbe writes the Luau leg's answer for one wire into main.luau: the
// same 47-byte header and saved bytes the wire-fuzz driver prints, so the
// answer parses into loadFixCAnswer and compares against loadFixCEngine.
func loadFixCProbe(g *generatedUnit, rootName string) string {
	var b strings.Builder
	var mods []string
	runtime := map[string]bool{"load": true, "save": true, "fixed": true, "message": true, "schema_tables": true, "schema_runtime": true}
	for name := range g.files {
		if strings.HasSuffix(name, ".luau") && !runtime[strings.TrimSuffix(name, ".luau")] {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)
	for _, m := range mods {
		fmt.Fprintf(&b, "require(\"./%s\")\n", m)
	}
	b.WriteString("local S = require(\"./schema_tables\")\n")
	b.WriteString("local load = require(\"./schema_load\")\n")
	b.WriteString("local save = require(\"./schema_save\")\n")
	b.WriteString("local inputs = require(\"./inputs\")\n")
	fmt.Fprintf(&b, "local ROOT = %q\n", rootName)
	b.WriteString(`local function hex(b)
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end

local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = load.loadTolerant(ROOT, inputs.wire, report)
local desc = S.lookup(ROOT)
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
`)
	return b.String()
}

// loadFixCUnit generates one manifest unit, writes its files into a temp dir,
// and answers the engine's model and the root's struct.
func loadFixCUnit(t *testing.T, unitKey, root string) (*generatedUnit, *tabletext.Model, *ir.Struct) {
	t.Helper()
	units, _ := readManifest(t)
	paths, err := compiler.GatherPaths(prefix(units[unitKey]))
	if err != nil {
		t.Fatalf("unit %s: %v", unitKey, err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("unit %s does not load: %v", unitKey, err)
	}
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatalf("unit %s: %v", unitKey, err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatalf("unit %s: %v", unitKey, err)
	}
	maps.Copy(files, tables)
	g := &generatedUnit{dir: t.TempDir(), files: files, loaded: true}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(g.dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := tabletext.NewModel(u)
	def := m.Lookup(root)
	if def == nil {
		t.Fatalf("unit %s declares no table %s", unitKey, root)
	}
	return g, m, def
}

type loadFixCCounts struct {
	unknown, kindMismatch, widened, clamped, duplicate int
	malformed                                          bool
}

type loadFixCAnswer struct {
	raised   string
	loaded   bool
	counts   loadFixCCounts
	refused  bool
	saved    []byte
	saveFail bool
}

func loadFixCParseLine(line string) (loadFixCAnswer, error) {
	var a loadFixCAnswer
	line = strings.TrimSpace(line)
	if line == "ERR" {
		a.raised = "the Luau load or save raised"
		return a, nil
	}
	raw, err := hex.DecodeString(line)
	if err != nil {
		return a, fmt.Errorf("reply line is not lowercase hex: %w", err)
	}
	if len(raw) < 47 {
		return a, fmt.Errorf("reply has %d bytes, want at least the 47-byte header", len(raw))
	}
	a.loaded = raw[0] != 0
	a.counts = loadFixCCounts{
		unknown:      int(int32(binary.LittleEndian.Uint32(raw[1:]))),
		kindMismatch: int(int32(binary.LittleEndian.Uint32(raw[5:]))),
		widened:      int(int32(binary.LittleEndian.Uint32(raw[9:]))),
		clamped:      int(int32(binary.LittleEndian.Uint32(raw[13:]))),
		duplicate:    int(int32(binary.LittleEndian.Uint32(raw[17:]))),
		malformed:    raw[21] != 0,
	}
	a.refused = raw[22] != 0
	n := int64(binary.LittleEndian.Uint64(raw[39:]))
	if n < 0 {
		a.saveFail = true
		return a, nil
	}
	if int64(len(raw)) != 47+n {
		return a, fmt.Errorf("reply has %d bytes, the header names %d saved", len(raw), n)
	}
	a.saved = raw[47:]
	return a, nil
}

type loadFixCOracle struct {
	variable       bool
	framingRefused bool
	noRoot         bool
	readNothing    bool
	counts         loadFixCCounts
	refused        bool
	encoded        []byte
	encFail        bool
}

func loadFixCEngine(m *tabletext.Model, def *ir.Struct, data []byte) loadFixCOracle {
	o := loadFixCOracle{variable: m.IsVariable(def.Name)}
	decode := tablewire.DecodeRegion
	if o.variable {
		if _, _, refused := tablewire.FileRegionMeasure(m.Unit, def, data); refused {
			o.framingRefused = true
			decode = tablewire.Decode
		}
	}
	inst := m.New(def)
	var rep tabletext.Report
	ok, derr := decode(m, inst, data, &rep)
	switch {
	case tablewire.Refused(derr):
		o.refused = true
	case derr != nil:
		o.noRoot = true
		return o
	default:
		o.counts = loadFixCCounts{
			unknown:      rep.Unknown,
			kindMismatch: rep.KindMismatch,
			widened:      rep.Widened,
			clamped:      rep.Clamped,
			duplicate:    rep.Duplicate,
			malformed:    rep.Malformed || !ok,
		}
	}
	enc, eerr := tablewire.Encode(m, inst)
	if eerr != nil {
		o.encFail = true
		return o
	}
	o.encoded = enc
	if o.counts == (loadFixCCounts{malformed: true}) {
		fresh, ferr := tablewire.Encode(m, m.New(def))
		o.readNothing = ferr == nil && bytes.Equal(fresh, enc)
	}
	return o
}

func loadFixCVerdict(a loadFixCAnswer, o loadFixCOracle) string {
	if a.raised != "" {
		return "the Luau leg RAISED: " + a.raised
	}
	if o.noRoot {
		return "the engine refused the root itself"
	}
	if o.counts.malformed && !o.refused {
		// DAMAGE DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4): the leg
		// owes the refusal, and no counter or saved byte is compared
		if a.loaded && !a.counts.malformed {
			return fmt.Sprintf("the engine discards a damaged load: Luau says %+v, the engine says %+v", a.counts, o.counts)
		}
		return ""
	}
	if !a.loaded {
		if !o.variable {
			return "the Luau load returned no root for a FIXED root, which always loads"
		}
		if a.refused != o.refused {
			return fmt.Sprintf("the Luau load returned no root and the verdict differs: Luau refused=%t, the engine refused=%t", a.refused, o.refused)
		}
		if o.refused || o.framingRefused || o.counts.malformed || o.readNothing {
			return ""
		}
		return fmt.Sprintf("the Luau load returned no root; the engine read a value (%+v)", o.counts)
	}
	if a.refused != o.refused {
		return fmt.Sprintf("the verdict differs: Luau refused=%t, the engine refused=%t", a.refused, o.refused)
	}
	if a.counts != o.counts {
		return fmt.Sprintf("the report differs: Luau says %+v, the engine says %+v", a.counts, o.counts)
	}
	if a.saveFail != o.encFail {
		if a.saveFail {
			return fmt.Sprintf("the Luau save refused the value it loaded; the engine encoded %d bytes", len(o.encoded))
		}
		return fmt.Sprintf("the engine refused to encode the value it decoded; the Luau save wrote %d bytes", len(a.saved))
	}
	if !a.saveFail && !bytes.Equal(a.saved, o.encoded) {
		return fmt.Sprintf("the decoded value differs: Luau saves %d bytes, the engine %d", len(a.saved), len(o.encoded))
	}
	return ""
}

// TestLuauLoadUnionFramingAgreesWithTheEngine is the regression for the
// wire-fuzz differential's cause C (union framing) on the seed the card
// names: a table arm whose sub-body ends before its frame is still stored as
// the union's value, where the engine reads that union as None; and union
// header framing damage must stop the enclosing body. The Luau tolerant load
// and the Go engine must agree on the six counters, the verdict and the
// re-saved bytes.
func TestLuauLoadUnionFramingAgreesWithTheEngine(t *testing.T) {
	t.Parallel()
	// The mutant of seed 0x21d319f0902d7814, unit tblm1, root Msg, case
	// m1_open. The root body's second field is the Body union (arm open, a
	// table); the arm's L=8 frames a table body whose string field claims an
	// L past the frame, so the sub-reader ends before the frame's end.
	mutant := []byte{
		0x01, 0x01, 0x08, 0x01, 0x00, 0x00, 0x00, 0x02, 0x0f, 0x03, 0x0d, 0x08, 0x04, 0x0c, 0xc2, 0x61,
		0x2e, 0x6d, 0x64, 0x00, 0x00, 0x3c, 0x13, 0xe2, 0x5c, 0x19, 0x8a, 0x3b, 0x82, 0x95, 0x32, 0xc9,
		0xc6, 0x9b, 0xe7, 0x4d, 0xcd, 0xe9, 0x70, 0x36, 0x63, 0xb4, 0x97, 0x4f, 0xf8, 0x76, 0x06, 0xd7,
		0xeb, 0x1c, 0x2d, 0xc5, 0x03, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}

	g, m, def := loadFixCUnit(t, "tblm1", "Msg")
	main := loadFixCProbe(g, "Msg")
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"wire": mutant})))
	ans, err := loadFixCParseLine(got)
	if err != nil {
		t.Fatalf("parse the Luau reply: %v\n%s", err, got)
	}
	o := loadFixCEngine(m, def, mutant)
	if v := loadFixCVerdict(ans, o); v != "" {
		t.Fatalf("seed 0x21d319f0902d7814 unit tblm1 root Msg case m1_open: %s\n    luau   %s\n    engine %x", v, got, o.encoded)
	}
}
