package luatable

import (
	"bytes"
	"encoding/hex"
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

// agCts is the report's six counters, a comparable tuple.
type agCts struct {
	unknown, kindMismatch, widened, clamped, duplicate int
	malformed                                          bool
}

// agAnswer is the Lua leg's answer for one wire.
type agAnswer struct {
	loaded   bool
	counts   agCts
	refused  bool
	saved    []byte
	saveFail bool
}

// agOracle is the engine's answer for one wire.
type agOracle struct {
	variable       bool
	framingRefused bool
	noRoot         bool
	counts         agCts
	refused        bool
	encoded        []byte
	encFail        bool
}

// agProbe is the Lua main for one wire: it loads with the generated Load,
// saves with Save, and prints one line, loaded|unknown|kindMismatch|widened|
// clamped|duplicate|malformed|refused|savedhex, savedhex "none" when the save
// refused or nothing was loaded.
func agProbe(module, root string) string {
	return `local T = require("` + module + `")
local load = require("load")
local save = require("save")
local inputs = require("inputs")
local wire = inputs.wire
local function hex(s)
  return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0,
                 clamped = 0, widened = 0, duplicate = 0 }
local v = load.Load(T, "` + root + `", wire, report)
local savedHex = "none"
local loaded = 0
if v ~= nil then
  loaded = 1
  local b = save.Save(T, "` + root + `", v)
  if b ~= nil then savedHex = hex(b) end
end
print(table.concat({ loaded, report.unknown, report.kindMismatch,
  report.widened, report.clamped, report.duplicate,
  tostring(report.malformed), report.refused, savedHex }, "|"))
`
}

// agParseLine parses one probe line.
func agParseLine(line string) (agAnswer, error) {
	var a agAnswer
	f := strings.Split(strings.TrimSpace(line), "|")
	if len(f) != 9 {
		return a, fmt.Errorf("probe line has %d fields, want 9: %q", len(f), line)
	}
	a.loaded = f[0] == "1"
	n := []*int{&a.counts.unknown, &a.counts.kindMismatch, &a.counts.widened, &a.counts.clamped, &a.counts.duplicate}
	for i, p := range n {
		if _, err := fmt.Sscanf(f[1+i], "%d", p); err != nil {
			return a, fmt.Errorf("probe line %q field %d: %v", line, 1+i, err)
		}
	}
	a.counts.malformed = f[6] == "true"
	a.refused = f[7] != "0"
	if f[8] == "none" {
		a.saveFail = true
		return a, nil
	}
	s, err := hex.DecodeString(f[8])
	if err != nil {
		return a, fmt.Errorf("probe line %q saved bytes: %v", line, err)
	}
	a.saved = s
	return a, nil
}

// agEngine reads one wire with the compiler's engine, mirroring the harness's
// luaFuzzEngine (fuzz_test.go).
func agEngine(m *tabletext.Model, def *ir.Struct, data []byte) agOracle {
	o := agOracle{variable: m.IsVariable(def.Name)}
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
		o.counts = agCts{
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
	return o
}

// agVerdict compares the Lua answer with the engine's and names the first
// disagreement, or "" when there is none.
func agVerdict(a agAnswer, o agOracle) string {
	if a.refused != o.refused {
		return fmt.Sprintf("the verdict differs: Lua refused=%t, the engine refused=%t", a.refused, o.refused)
	}
	if a.counts != o.counts {
		return fmt.Sprintf("the report differs: Lua says %+v, the engine says %+v", a.counts, o.counts)
	}
	if a.saveFail != o.encFail {
		if a.saveFail {
			return fmt.Sprintf("the Lua save refused the value it loaded; the engine encoded %d bytes", len(o.encoded))
		}
		return fmt.Sprintf("the engine refused to encode the value it decoded; the Lua save wrote %d bytes", len(a.saved))
	}
	if !a.saveFail && !bytes.Equal(a.saved, o.encoded) {
		at := 0
		for at < len(a.saved) && at < len(o.encoded) && a.saved[at] == o.encoded[at] {
			at++
		}
		return fmt.Sprintf("the decoded value differs: Lua saves %d bytes, the engine %d, first difference at byte %d\n    lua    %x\n    engine %x",
			len(a.saved), len(o.encoded), at, a.saved, o.encoded)
	}
	return ""
}

// TestLuaTableDamagedReportsAndResavesAgree pins one fixed divergence group per
// subtest: each loads the named wire with the generated Load, saves it with
// Save, and requires the counters, the verdict and the saved bytes to equal
// internal/tablewire's decode and encode of the same bytes. A red prints both
// sides.
func TestLuaTableDamagedReportsAndResavesAgree(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	units := manifestUnits(t, manifest)
	rows := map[string]manifestRow{}
	for _, r := range manifestRows(t, manifest, "instance") {
		rows[r.caseName] = r
	}
	cases := []struct {
		name, caseName string
		wire           string
	}{
		{"report_guard_false_no_edge", "guard_false_no_edge", "01011102020c0a010307040401000000000044e5f7fadfa6c4f7ffffffffffffffff8db6f6d2c660bd66ea0ce83094fde47c0400000000000000"},
		{"resave_blob_empty", "blob_empty", "01010c046261726502114a031103040c05020500060000861b638ebaadbcc403b2f40f72193b61dd7c58d1bafbf83bffffffffffffffffe44f1c4f47c0852f58fcaffad8e04b700600000000000000"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			row, ok := rows[c.caseName]
			if !ok {
				t.Fatalf("no instance row %s", c.caseName)
			}
			var paths []string
			for _, p := range units[row.unit] {
				paths = append(paths, filepath.Join(repo, p))
			}
			u := luarun.LoadUnit(t, paths...)
			files, err := Generate(u)
			if err != nil {
				t.Fatalf("unit %s: %v", row.unit, err)
			}
			dir := t.TempDir()
			luarun.WriteFiles(t, dir, files)
			probe := agProbe(ModuleName(u), row.root)
			if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
				t.Fatal(err)
			}
			data, err := hex.DecodeString(c.wire)
			if err != nil {
				t.Fatal(err)
			}
			out := strings.TrimRight(string(luarun.Run(t, dir, "main.lua", map[string][]byte{"wire": data})), "\n")
			ans, err := agParseLine(out)
			if err != nil {
				t.Fatalf("case %s: %v (probe line %q)", c.name, err, out)
			}
			m := tabletext.NewModel(u)
			def := m.Lookup(row.root)
			if def == nil {
				t.Fatalf("unit %s declares no table %s", row.unit, row.root)
			}
			o := agEngine(m, def, data)
			if v := agVerdict(ans, o); v != "" {
				t.Errorf("case %s: %s\n    lua    %s", c.name, v, out)
			}
		})
	}
}
