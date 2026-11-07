package luautable_test

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
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

// loadFixA is the splitmix64 generator the differential uses, so this card's
// regression rebuilds the same mutant from the same seed.
type loadFixAsplitmix64 struct{ state uint64 }

func (r *loadFixAsplitmix64) next() uint64 {
	r.state += 0x9E3779B97F4A7C15
	z := r.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// loadFixAmutate reproduces the differential's one-to-four-byte mutation of
// base for a seed (docs/PORT-LUAU.md, THE CARD MAP: schema-luau-fuzz-
// differential).
func loadFixAmutate(base []byte, seed uint64) []byte {
	r := &loadFixAsplitmix64{state: seed}
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

// loadFixAcounts is the report's six counters.
type loadFixAcounts struct {
	unknown, kindMismatch, widened, clamped, duplicate int
	malformed                                          bool
}

// loadFixAengine reads the mutant with the compiler's engine (internal/tablewire),
// following the differential's oracle for a file root: a variable root's region
// is measured first, and a wire the framing refuses is read by the plain Decode.
func loadFixAengine(m *tabletext.Model, def *ir.Struct, data []byte) (loadFixAcounts, bool, []byte) {
	var ref bool
	decode := tablewire.DecodeRegion
	if m.IsVariable(def.Name) {
		if _, _, refused := tablewire.FileRegionMeasure(m.Unit, def, data); refused {
			ref = true
			decode = tablewire.Decode
		}
	}
	inst := m.New(def)
	var rep tabletext.Report
	ok, derr := decode(m, inst, data, &rep)
	c := loadFixAcounts{malformed: rep.Malformed || (derr == nil && !ok)}
	if !ref && derr == nil {
		c.unknown = rep.Unknown
		c.kindMismatch = rep.KindMismatch
		c.widened = rep.Widened
		c.clamped = rep.Clamped
		c.duplicate = rep.Duplicate
	}
	enc, eerr := tablewire.Encode(m, inst)
	if eerr != nil {
		return c, ref, nil
	}
	return c, ref, enc
}

// loadFixAstage generates the tabledemo unit, stages its files in a fresh
// directory and runs the probe against one wire, returning the probe's answer
// line.
func loadFixAstage(t *testing.T, wire []byte) string {
	t.Helper()
	units, _ := readManifest(t)
	paths, err := compiler.GatherPaths(prefix(units["tabledemo"]))
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	main := `local inputs = require("./inputs")
local load = require("./schema_load")
local save = require("./schema_save")
local St = require("./schema_tables")
require("./TablesTable")
require("./NestedTable")
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = load.loadTolerant("ProfileConfig", inputs.wire, report)
local out = save.save(St.lookup("ProfileConfig"), v)
local saved = "REFUSED"
if out ~= nil then
	local parts = {}
	for i = 0, buffer.len(out) - 1 do parts[i + 1] = string.format("%02x", buffer.readu8(out, i)) end
	saved = table.concat(parts)
end
print(string.format("%d,%d,%d,%d,%d,%s|%s", report.unknown, report.kindMismatch, report.widened, report.clamped, report.duplicate, tostring(report.malformed), saved))
`
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", map[string][]byte{"wire": wire})))
}

// loadFixAverdict names the first disagreement between the Luau answer and the
// engine's, or "" when they agree.
func loadFixAverdict(line string, c loadFixAcounts, saveFail bool, enc []byte) string {
	parts := strings.SplitN(line, "|", 2)
	if len(parts) != 2 {
		return "the Luau probe did not answer in the expected shape"
	}
	var got loadFixAcounts
	if _, err := fmt.Sscanf(parts[0], "%d,%d,%d,%d,%d,%t", &got.unknown, &got.kindMismatch, &got.widened, &got.clamped, &got.duplicate, &got.malformed); err != nil {
		return "the Luau probe answer did not parse: " + parts[0]
	}
	if c.malformed {
		// DAMAGE DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4): the leg
		// owes the refusal, and no counter or saved byte is compared
		if !got.malformed {
			return "the engine discards a damaged load; the Luau load did not refuse it"
		}
		return ""
	}
	if got != c {
		return fmt.Sprintf("the report differs: Luau says {unknown:%d kindMismatch:%d widened:%d clamped:%d duplicate:%d malformed:%t}, the engine says {unknown:%d kindMismatch:%d widened:%d clamped:%d duplicate:%d malformed:%t}",
			got.unknown, got.kindMismatch, got.widened, got.clamped, got.duplicate, got.malformed,
			c.unknown, c.kindMismatch, c.widened, c.clamped, c.duplicate, c.malformed)
	}
	if parts[1] == "REFUSED" {
		if !saveFail {
			return "the Luau save refused the value it loaded; the engine encoded the value"
		}
		return ""
	}
	saved, err := hex.DecodeString(parts[1])
	if err != nil {
		return "the Luau saved hex did not parse"
	}
	if saveFail {
		return "the engine refused to encode the value it decoded; the Luau save wrote bytes"
	}
	if !bytes.Equal(saved, enc) {
		return fmt.Sprintf("the decoded value differs: Luau saves %d bytes, the engine %d", len(saved), len(enc))
	}
	return ""
}

// TestLuauLoadRootTrailingBytesAgreesWithTheEngine pins cause A of the Luau
// wire-fuzz differential: a root body with bytes after its terminator is
// malformed before anything is read. The engine (internal/tablewire, decode.go,
// decodeWith: "ANY BYTE BETWEEN THE ROOT'S TERMINATOR AND THE TABLE'S FIRST
// ENTRY IS MALFORMED", docs/SPEC-TABLES.md §3) checks the terminator first and
// reads nothing when bytes follow it, so every counter is 0; the Luau tolerant
// load decoded the body first and counted its unknown field. The regression
// rebuilds the mutant of seed 0x4e9588da75e469fe (unit tabledemo, root
// ProfileConfig, case profile_elide) and requires the six counters, the verdict
// and the re-saved bytes to agree between the two legs.
func TestLuauLoadRootTrailingBytesAgreesWithTheEngine(t *testing.T) {
	t.Parallel()

	// seed 0x4e9588da75e469fe, unit tabledemo, root ProfileConfig, case
	// profile_elide: the mutant it rebuilds, pinned
	const seed = uint64(0x4e9588da75e469fe)
	const pinHex = "01010801000000020101030d080401060d0201000100000069b9801f526d8bab3c258e140a5e6fa0a3b513ff75ce595724cc8a11f5f028de0400000000000000"

	var base []byte
	for _, row := range manifestRows(t) {
		if row.kind == "instance" && row.unit == "tabledemo" && row.root == "ProfileConfig" && row.caseName == "profile_elide" {
			b, err := os.ReadFile(filepath.Join(root, row.wire))
			if err != nil {
				t.Fatal(err)
			}
			base = b
			break
		}
	}
	if base == nil {
		t.Fatal("profile_elide instance of tabledemo:ProfileConfig not in the manifest")
	}

	mutant := loadFixAmutate(base, seed)
	if hex.EncodeToString(mutant) != pinHex {
		t.Fatalf("seed %#016x rebuilt a different mutant: got %x want %s", seed, mutant, pinHex)
	}

	units, _ := readManifest(t)
	paths, err := compiler.GatherPaths(prefix(units["tabledemo"]))
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	def := m.Lookup("ProfileConfig")
	if def == nil {
		t.Fatal("tabledemo declares no ProfileConfig")
	}

	line := loadFixAstage(t, mutant)
	c, ref, enc := loadFixAengine(m, def, mutant)
	if v := loadFixAverdict(line, c, enc == nil, enc); v != "" {
		t.Errorf("seed %#016x unit tabledemo root ProfileConfig case profile_elide: %s\n    mutant %x", seed, v, mutant)
	}

	// the cause: the engine reads nothing — malformed, every counter 0, and
	// the wire's framing was refused by the region measure
	if !ref && !c.malformed {
		t.Errorf("seed %#016x: expected the engine to read nothing (malformed, every counter 0), got %+v", seed, c)
	} else if !ref && c != (loadFixAcounts{malformed: true}) {
		t.Errorf("seed %#016x: expected the engine to read nothing (malformed, every counter 0), got %+v", seed, c)
	}
}
