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
local v = load.LoadTolerant(T, "` + root + `", wire, report)
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
			return a, fmt.Errorf("probe line %q field %d: %w", line, 1+i, err)
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
		return a, fmt.Errorf("probe line %q saved bytes: %w", line, err)
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
	if o.noRoot {
		return "the engine refused the root itself"
	}
	if !a.loaded {
		return fmt.Sprintf("the Lua load returned no root; the engine read %+v (refused=%t)", o.counts, o.refused)
	}
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
		// a field reference spelled 2^64-1 is past the id table, so the body is
		// malformed; a signed bound read it as a field this table cannot name
		{"report_leb_reference_unsigned", "guard_false_no_edge", "01ffffffffffffffffff011102020c0a010307040401000000000044e5f7fadfa6c4f7ffffffffffffffff8db6f6d2c660bd66ea0ce83094fde47c0400000000000000"},
		// a flags field is never clamped to its wire bits (§4)
		{"report_flags_not_bit_clamped", "message_arms", "01010800000000020f030e0a0402fdffffff09000000040e170f020509080300000000000033060f070708048000000000a828f581a4ac38aa9532c9c69be74dcd166856b27ffc7d43fff2d5bb45894233f85775e8906b5ab5cf145a78fee4eab9220160561970af770700000000000000"},
		// a map's keys are scanned before its entries are read, ascending (§2.8)
		{"report_map_key_scan", "blob_shared_string", "01010e140d0208020801610311020008020c806203110200042c09010506736861726564009f76486aa7da01abec805b36194ac93dea0ce83094fde47cffffffffffffffff58fcaffad8e04b700500000000000000"},
		// an enum-keyed body never clamps its count (§3.2)
		{"report_keyed_count_not_clamped", "r1_ping", "01011e02030f042000050d07060a0000c03f00070e041e02020809100804a002040700350000d48aaa77299ba8328025f1eade1ae5c39443f2513bc8c136a90733c50de030bfcc69e4e208beb5ffc6875c803a62dc9ac599f782764e0ad9b318320d7ec916c4dd542dd0294d3faa0900000000000000"},
		// slack after an arm's inner union is skipped by the outer L (§3)
		{"report_arm_inner_union_slack", "message_arms", "0101080c000000020f030e0a0402fdffffff09000000040e170f020509080300000000000000060f070708002900000000a828f581a4ac38aa9532c9c69be74dcd166856b28afc7d43fff2d5bb45894233f85775e8906b5ab5cf145a78fee4eab9220360561970af770700000000000000"},
		// a None enum element keeps its place in the list (§2.9)
		{"resave_enum_none_element", "r1_ping", "01011e02030f042000050d07060a0000c03f00070e041e020008091008040102040700000000d48ac477299ba8328025f1eade1ae5c39443f2513bc8c136a90733c50de030bfcc69e4e29bbeb5ffc6875c803a62dc9ac599f782764e0ad9b318320d7ec916c4dde62dd0294d3fae0900000000000000"},
		// an array of another element kind keeps the field, and an arm holding
		// one reads None (§2.3, §3)
		{"resave_array_kind_mismatch_keeps_field", "message_arms", "0101080c000000020f030e0a8402fdffffff09000000040e170f020509080300510000000080060f070708042900000000a828f581a4ac38aa9532c9c69be74dcd166856b28afc7d43fff2d5bb45894233f85775e8906b5ab5cf145a78fee4eab9220160561970af770700000000000000"},
		{"resave_list_kind_mismatch_keeps_field", "list_mixed", "01010e051e03020304050e12090203000000000000000400000000000000010e1a0f03070d0d080401000000090402800000000a040407000000000b0e0e040300000000320000006400000000c599f782764e0ad9ec2202864cfc63af522602864cfe63af052802864cff63afa82e867084edf24abbf00c9bccfb2d73b145c34435abfe73071702864cf563af541502864cf463afc07fb38abe08637f68b77fae4c0cf6520b00000000000000"},
		// a map entry whose pointer value is null stays an entry (§2.8, §3.1)
		{"resave_map_null_value", "blob_shared_string", "01010e140d0208010c01610311020008020c016203110200040c09010506736861726564009f76486aa7da01abec107f36184ac93dea0ce83094fde47cfffffffffffff7ff58fcaffad8e04b700500000000000000"},
		// a pointer under a false guard is not written and not numbered (§3,
		// §3.1): its target takes no node, so the re-save writes one record
		{"resave_guard_false_drops_guarded_pointer", "guard_true_both_edges", "01010101021102031103040c13020507ff04010000000005070604020000000000c740a70b985d73d744e5f7fadfa6c4f7c15dd12f298f25e3ffffffffffffffff8db6f6d2c61cbd66ea0ce83094fde47c0600000000000000"},
	}
	for _, c := range cases {
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
