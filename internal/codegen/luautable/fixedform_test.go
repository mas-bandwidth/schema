package luautable_test

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// fixedCorpusVectors pairs the corpus files with the schema and root that wrote
// them (docs/FIXED-FORM-ALGORITHM.md §7), the same nine the Go leg reads.
type fixedVector struct {
	file, schema, root string
}

var fixedCorpusVectors = []fixedVector{
	{"fx1.bin", "test/tables/FX1.schema", "FxRoot"},
	{"fx2.bin", "test/tables/FX2.schema", "FxRoot"},
	{"p1.bin", "test/tables/P1.schema", "Chain"},
	{"p3.bin", "test/tables/P3.schema", "Chain"},
	{"keyed.bin", "tables/examples/Keyed.schema", "KeyedConfig"},
	{"pack.bin", "tables/examples/Pack.schema", "PackConfig"},
	{"fxw.bin", "test/tables/FXW.schema", "FxWide"},
	{"fu1.bin", "test/tables/FU1.schema", "FuRoot"},
	{"fu2.bin", "test/tables/FU2.schema", "FuRoot"},
}

const hexProbe = `local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
`

// build writes a unit's Luau modules into a fresh directory and returns the
// directory, the module basename and the files.
func build(t *testing.T, schema string) (string, string, map[string][]byte) {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, schema)})
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
	return dir, strings.TrimSuffix(filepath.Base(schema), ".schema") + "Table", files
}

// fixedNonFiniteValue is one corpus-manifest value whose bits are a NaN or an
// infinity: its path, then the float's spelled bits (the dump writes a float as
// its digits and its bits, "nan|0x7F800001").
var fixedNonFiniteValue = regexp.MustCompile(`^([A-Za-z0-9_.\[\]]+)=[^|]*\|0x([0-9A-Fa-f]{16}|[0-9A-Fa-f]{8})$`)

// fixedManifestNonFinite reads, per `file=` line of the corpus manifest, the
// leaf name of the first value the reference wrote as a NaN or an infinity,
// and "" for a file that holds none, as the Go leg's fixedOracleManifestNonFinite
// does. Under the finite rule of 2026-10-27 (docs/SPEC-TABLES.md §3.3) such a
// file is a MUST-REFUSE vector: the reference's writer still emits the pattern
// until the C++ card of the finite-tables plan (#2232) regenerates the corpus
// with finite values, and a leg that refuses it is the leg that keeps the rule.
// The manifest decides, so the identity row comes back by itself the day the
// corpus turns finite.
func fixedManifestNonFinite(t *testing.T, corpus string) map[string]string {
	t.Helper()
	out := map[string]string{}
	data, err := os.ReadFile(filepath.Join(corpus, "manifest.txt"))
	if os.IsNotExist(err) {
		return out
	}
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		head, values, ok := strings.Cut(line, " values=")
		if !ok {
			continue
		}
		file := ""
		for field := range strings.FieldsSeq(head) {
			if v, ok := strings.CutPrefix(field, "file="); ok {
				file = v
			}
		}
		for value := range strings.SplitSeq(values, ",") {
			m := fixedNonFiniteValue.FindStringSubmatch(value)
			if m == nil {
				continue
			}
			bits, err := strconv.ParseUint(m[2], 16, 64)
			if err != nil {
				t.Fatal(err)
			}
			nonFinite := len(m[2]) == 8 && bits&0x7F800000 == 0x7F800000 || len(m[2]) == 16 && bits&0x7FF0000000000000 == 0x7FF0000000000000
			if nonFinite && file != "" && out[file] == "" {
				leaf := m[1]
				if i := strings.LastIndexByte(leaf, '.'); i >= 0 {
					leaf = leaf[i+1:]
				}
				out[file] = leaf
			}
		}
	}
	return out
}

// fixedRefusalProbe loads a corpus file the manifest says holds a non-finite
// float and prints the refusal: its name and the field it names.
const fixedRefusalProbe = `local ok, err = pcall(F.load, T.%sDescriptor, %s, inputs.oracle%s)
if ok then
	print("loaded")
else
	local e: any = err
	print("refused " .. tostring(e.name) .. " " .. tostring(e.field))
end
`

// checkFixedRefusal holds a must-refuse probe's line to non_finite_float naming
// <Root>.<leaf>.
func checkFixedRefusal(t *testing.T, file, root, leaf string, out []byte) {
	t.Helper()
	got := strings.TrimSpace(string(out))
	want := "refused non_finite_float " + root + "." + leaf
	if got != want {
		t.Errorf("%s holds a non-finite %s, a must-refuse vector (docs/SPEC-TABLES.md §3.3): the Luau load answered %q, want %q", file, leaf, got, want)
	}
}

func fixedVars(root string) string {
	return fmt.Sprintf(`local fixed = { hash = T.%[1]sFixedHash, recordBytes = T.%[1]sFixedRecordBytes, layout = T.%[1]sFixedLayout }`, root)
}

// TestLuauFixedFormAgreesWithTheByteOracle writes every fixed corpus root from
// the value its own reader lands on the oracle's bytes, and requires the bytes
// back: the writer agrees with the C++ reference and the reader lands the
// oracle's values (docs/FIXED-FORM-ALGORITHM.md §7 row 1). A reader one field
// older and one newer is exercised on the FU pair: the newer reader wide-reads
// the older file, and the older reader refuses the newer file by name (§5.3).
func TestLuauFixedFormAgreesWithTheByteOracle(t *testing.T) {
	t.Parallel()
	corpus := filepath.Join(root, "build", "fixedform-corpus")
	nonFinite := fixedManifestNonFinite(t, corpus)
	var compared atomic.Int32
	for _, v := range fixedCorpusVectors {
		t.Run(strings.TrimSuffix(v.file, ".bin"), func(t *testing.T) {
			t.Parallel()
			oracle, err := os.ReadFile(filepath.Join(corpus, v.file))
			if err != nil {
				if os.Getenv("SCHEMA_REQUIRE_CORPUS") == "" {
					t.Skip("the oracle is absent; SCHEMA_REQUIRE_CORPUS unset")
				}
				t.Fatalf("the oracle file %s is absent: build it with make tables-fixedform-corpus: %v", v.file, err)
			}
			dir, mod, _ := build(t, v.schema)
			if leaf := nonFinite[v.file]; leaf != "" {
				probe := fmt.Sprintf("--!strict\nlocal T = require(\"./%s\")\nlocal F = require(\"./schema_fixed\")\nlocal inputs = require(\"./inputs\")\n", mod) +
					fixedVars(v.root) + "\n" + fmt.Sprintf(fixedRefusalProbe, v.root, "fixed", "")
				if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(probe), 0o644); err != nil {
					t.Fatal(err)
				}
				checkFixedRefusal(t, v.file, v.root, leaf, luaurun.Run(t, dir, "main.luau", map[string][]byte{"oracle": oracle}))
				compared.Add(1)
				return
			}
			main := "--!strict\nlocal T = require(\"./%s\")\nlocal F = require(\"./schema_fixed\")\nlocal inputs = require(\"./inputs\")\n" +
				fixedVars(v.root) + "\nlocal values = F.load(T.%sDescriptor, fixed, inputs.oracle)\nlocal out = F.save(T.%sDescriptor, fixed, values)\nprint(hex(out))\n"
			main = fmt.Sprintf(main, mod, v.root, v.root)
			main = hexProbe + main
			if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
				t.Fatal(err)
			}
			out := luaurun.Run(t, dir, "main.luau", map[string][]byte{"oracle": oracle})
			got, err := hex.DecodeString(strings.TrimSpace(string(out)))
			if err != nil {
				t.Fatalf("the probe printed %q: %v", out, err)
			}
			if string(got) != string(oracle) {
				at := 0
				for at < len(got) || at < len(oracle) {
					var a, b byte
					if at < len(got) {
						a = got[at]
					}
					if at < len(oracle) {
						b = oracle[at]
					}
					if a != b {
						break
					}
					at++
				}
				lo := max(at-8, 0)
				hi := min(at+16, len(got))
				ohi := min(at+16, len(oracle))
				t.Errorf("%s: differs at byte %d: got %x oracle %x (%d bytes)", v.file, at, got[lo:hi], oracle[lo:ohi], len(oracle))
			}
			compared.Add(1)
		})
	}
	t.Cleanup(func() { t.Logf("compared %d fixed roots against the byte oracle", compared.Load()) })
}

// layoutLiteral renders the oracle header's layout as Luau static data for a
// foreign (older) writer's Fixed value.
func layoutLiteral(oracle []byte) string {
	count := binary.LittleEndian.Uint32(oracle[20:24])
	var b strings.Builder
	b.WriteString("{\n")
	p := 24
	for i := 0; i < int(count); i++ {
		id := binary.LittleEndian.Uint64(oracle[p : p+8])
		kind := oracle[p+8]
		size := binary.LittleEndian.Uint32(oracle[p+9 : p+13])
		children := binary.LittleEndian.Uint32(oracle[p+13 : p+17])
		fmt.Fprintf(&b, "\t{ id = %s, kind = %d, size = %d, children = %d },\n",
			luau.IntegerLit(new(big.Int).SetUint64(id)), kind, size, children)
		p += 17
	}
	b.WriteString("}")
	return b.String()
}

// knownLiteral is a lineage entry (docs/FIXED-FORM-ALGORITHM.md §5.2) for the
// layout an oracle file carries, as Luau static data: the test plays the lock,
// stating the older writer's locked entry the way the build hands it down.
func knownLiteral(oracle []byte) string {
	hash := binary.LittleEndian.Uint64(oracle[8:16])
	length := binary.LittleEndian.Uint32(oracle[16:20])
	body := binary.LittleEndian.Uint32(oracle[20+4+9 : 20+4+13])
	return fmt.Sprintf("{ hash = %s, layout = %s, layoutBytes = %d, recordBytes = %d }",
		luau.IntegerLit(new(big.Int).SetUint64(hash)), layoutLiteral(oracle), length, 8+body)
}

// TestLuauFixedFormVersioning pins the §3.4 invariant through the lineage
// (§5.2, §5.3): a reader one appended field newer, handed the older writer's
// locked entry, reads the older writer's record, widening int16->int32 and
// f32->f64, and writes the older writer's bytes back under that entry's layout;
// the older reader, whose lineage holds no newer entry, refuses the newer file
// as layout_unknown, reporting the file's hash.
func TestLuauFixedFormVersioning(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter and the fixed-form corpus")
	corpus := filepath.Join(root, "build", "fixedform-corpus")
	oldOracle, err := os.ReadFile(filepath.Join(corpus, "fu1.bin"))
	if err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") == "" {
			t.Skip("the FU1 oracle is absent; SCHEMA_REQUIRE_CORPUS unset")
		}
		t.Fatalf("the FU1 oracle is absent: make tables-fixedform-corpus: %v", err)
	}
	newOracle, err := os.ReadFile(filepath.Join(corpus, "fu2.bin"))
	if err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") == "" {
			t.Skip("the FU2 oracle is absent; SCHEMA_REQUIRE_CORPUS unset")
		}
		t.Fatalf("the FU2 oracle is absent: make tables-fixedform-corpus: %v", err)
	}

	dir, mod, _ := build(t, "test/tables/FU2.schema")
	leaf := fixedManifestNonFinite(t, corpus)["fu1.bin"]
	if leaf != "" {
		// the older file holds a non-finite float (until #2232 regenerates the
		// corpus): the newer reader refuses it, before any widening runs
		probe := fmt.Sprintf("--!strict\nlocal T = require(\"./%s\")\nlocal F = require(\"./schema_fixed\")\nlocal inputs = require(\"./inputs\")\nlocal older = %s\n", mod, knownLiteral(oldOracle)) +
			"local fixed = { hash = T.FuRootFixedHash, recordBytes = T.FuRootFixedRecordBytes, layout = T.FuRootFixedLayout, lineage = { older, T.FuRootFixedLineage[#T.FuRootFixedLineage] }, floor = 0 }\n" +
			fmt.Sprintf(fixedRefusalProbe, "FuRoot", "fixed", "")
		if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
		checkFixedRefusal(t, "fu1.bin", "FuRoot", leaf, luaurun.Run(t, dir, "main.luau", map[string][]byte{"oracle": oldOracle}))
	}
	if leaf == "" {
		main := hexProbe + fmt.Sprintf(`--!strict
local T = require("./%s")
local F = require("./schema_fixed")
local inputs = require("./inputs")
local older = %s
local fixed = { hash = T.FuRootFixedHash, recordBytes = T.FuRootFixedRecordBytes, layout = T.FuRootFixedLayout,
	lineage = { older, T.FuRootFixedLineage[#T.FuRootFixedLineage] }, floor = 0 }
local report: any = {}
local values = F.load(T.FuRootDescriptor, fixed, inputs.old, report)
if report.widened == 0 then error("a widening lineage moved no widened counter") end
local out = F.save(T.FuRootDescriptor, { hash = older.hash, recordBytes = older.recordBytes - 8, layout = older.layout }, values)
print(hex(out))
`, mod, knownLiteral(oldOracle))
		if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
			t.Fatal(err)
		}
		out := luaurun.Run(t, dir, "main.luau", map[string][]byte{"old": oldOracle})
		got, err := hex.DecodeString(strings.TrimSpace(string(out)))
		if err != nil {
			t.Fatalf("the newer probe printed %q: %v", out, err)
		}
		if string(got) != string(oldOracle) {
			at := 0
			for at < len(got) || at < len(oldOracle) {
				var a, b byte
				if at < len(got) {
					a = got[at]
				}
				if at < len(oldOracle) {
					b = oldOracle[at]
				}
				if a != b {
					break
				}
				at++
			}
			t.Errorf("the newer reader did not read the older record back to its bytes (first diff %d)", at)
		}

	}

	dir, mod, _ = build(t, "test/tables/FU1.schema")
	main := fmt.Sprintf(`--!strict
local T = require("./%s")
local F = require("./schema_fixed")
local inputs = require("./inputs")
local fixed = { hash = T.FuRootFixedHash, recordBytes = T.FuRootFixedRecordBytes, layout = T.FuRootFixedLayout,
	lineage = T.FuRootFixedLineage, floor = T.FuRootFixedFloor }
local report: any = {}
local values = F.read(T.FuRootDescriptor, fixed, inputs.new, report)
if values ~= nil then print("read") else print(report.reason .. "|" .. tostring(report.layoutHash)) end
`, mod)
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luaurun.Run(t, dir, "main.luau", map[string][]byte{"new": newOracle})
	want := fmt.Sprintf("layout_unknown|%d", int64(binary.LittleEndian.Uint64(newOracle[8:16])))
	if strings.TrimSpace(string(out)) != want {
		t.Errorf("the older reader did not refuse the newer file as layout_unknown with its hash: %q, want %q", out, want)
	}
}

// forgedFile is a form-3 file with no records whose header carries `hash` and
// the layout `entries` verbatim (docs/SPEC-TABLES.md §3.4 THE FRAMING).
func forgedFile(hash uint64, entries [][4]uint64) []byte {
	b := make([]byte, 20, 24+17*len(entries))
	b[0] = 3
	binary.LittleEndian.PutUint64(b[8:], hash)
	binary.LittleEndian.PutUint32(b[16:], uint32(4+17*len(entries)))
	b = binary.LittleEndian.AppendUint32(b, uint32(len(entries)))
	for _, e := range entries {
		b = binary.LittleEndian.AppendUint64(b, e[0])
		b = append(b, byte(e[1]))
		b = binary.LittleEndian.AppendUint32(b, uint32(e[2]))
		b = binary.LittleEndian.AppendUint32(b, uint32(e[3]))
	}
	return b
}

// TestLuauFixedFormRefusals reaches each named refusal of a lineage entry that
// will not build — a LANE CARRYING ITS NAME (docs/FIXED-FORM-ALGORITHM.md §5.9
// #36, §5.3's table) — with a file whose header carries that entry's hash and
// layout byte for byte, so the comparison of step 7 passes and the entry's own
// plan is what refuses; and the form byte's name before any of it (§1.1, §4.2;
// docs/SPEC-TABLES.md §3).
func TestLuauFixedFormRefusals(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter and the fixed-form corpus")
	corpus := filepath.Join(root, "build", "fixedform-corpus")
	oracle, err := os.ReadFile(filepath.Join(corpus, "fx1.bin"))
	if err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") == "" {
			t.Skip("the FX1 oracle is absent; SCHEMA_REQUIRE_CORPUS unset")
		}
		t.Fatalf("the FX1 oracle is absent: make tables-fixedform-corpus: %v", err)
	}
	const forgedHash = 0x0123456789abcdef
	tooLarge := [][4]uint64{{0, 13, 70000, 0}}
	var tooMany [][4]uint64
	tooMany = append(tooMany, [4]uint64{0, 13, 0, 0})
	for range 4097 {
		tooMany = append(tooMany, [4]uint64{0, 4, 4, 0})
	}
	forged := map[string][][4]uint64{
		"layout_malformed":        {},
		"layout_record_too_large": tooLarge,
		"plan_too_large":          tooMany,
	}
	for name, entries := range forged {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir, mod, _ := build(t, "test/tables/FX1.schema")
			var lit strings.Builder
			lit.WriteString("{")
			for _, e := range entries {
				fmt.Fprintf(&lit, " { id = %di, kind = %d, size = %d, children = %d },", e[0], e[1], e[2], e[3])
			}
			lit.WriteString(" }")
			body := uint64(0)
			if len(entries) > 0 {
				body = entries[0][2]
			}
			main := fmt.Sprintf(`--!strict
local T = require("./%s")
local F = require("./schema_fixed")
local inputs = require("./inputs")
local forged = { hash = %di, layout = %s, layoutBytes = %d, recordBytes = %d }
local fixed = { hash = T.FxRootFixedHash, recordBytes = T.FxRootFixedRecordBytes, layout = T.FxRootFixedLayout,
	lineage = { forged, T.FxRootFixedLineage[#T.FxRootFixedLineage] }, floor = 0 }
local report: any = {}
if F.read(T.FxRootDescriptor, fixed, inputs.forged, report) ~= nil then print("read") else print(report.reason) end
`, mod, int64(forgedHash), lit.String(), 4+17*len(entries), 8+body)
			if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
				t.Fatal(err)
			}
			out := luaurun.Run(t, dir, "main.luau", map[string][]byte{"forged": forgedFile(forgedHash, entries)})
			if got := strings.TrimSpace(string(out)); got != name {
				t.Errorf("the forged lineage entry reached %q, want %q", got, name)
			}
		})
	}
	t.Run("previous_form", func(t *testing.T) {
		t.Parallel()
		dir, mod, _ := build(t, "test/tables/FX1.schema")
		main := fmt.Sprintf(`--!strict
local T = require("./%s")
local F = require("./schema_fixed")
local inputs = require("./inputs")
local fixed = { hash = T.FxRootFixedHash, recordBytes = T.FxRootFixedRecordBytes, layout = T.FxRootFixedLayout }
local report: any = {}
if F.read(T.FxRootDescriptor, fixed, inputs.oracle, report) ~= nil then print("read") else print(report.reason) end
`, mod)
		if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
			t.Fatal(err)
		}
		b := append([]byte(nil), oracle...)
		b[0] = 1
		out := luaurun.Run(t, dir, "main.luau", map[string][]byte{"oracle": b})
		if got := strings.TrimSpace(string(out)); got != "previous_form" {
			t.Errorf("a form-1 byte reached %q, want previous_form", got)
		}
	})
}

// TestLuauFixedFormNegativeControl patches the emitted runtime in memory at one
// anchor, matched exactly once, so the writer skips each record's last field;
// the oracle comparison must then go red.
func TestLuauFixedFormNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter and the fixed-form corpus")
	corpus := filepath.Join(root, "build", "fixedform-corpus")
	oracle, err := os.ReadFile(filepath.Join(corpus, "fx1.bin"))
	if err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") == "" {
			t.Skip("the FX1 oracle is absent; SCHEMA_REQUIRE_CORPUS unset")
		}
		t.Fatalf("the FX1 oracle is absent: make tables-fixedform-corpus: %v", err)
	}
	dir, mod, files := build(t, "test/tables/FX1.schema")
	anchor := "-- FIXED-WRITE-ANCHOR\n\tfor _, f in d.fields do\n\t\tlocal c = childById(n, f.id)"
	runtime := string(files["schema_fixed.luau"])
	if strings.Count(runtime, anchor) != 1 {
		t.Fatalf("the negative control's anchor appears %d times, want exactly once", strings.Count(runtime, anchor))
	}
	patched := strings.Replace(runtime, anchor,
		"-- FIXED-WRITE-ANCHOR\n\tfor i, f in d.fields do\n\t\tif i == #d.fields then continue end\n\t\tlocal c = childById(n, f.id)", 1)
	if err := os.WriteFile(filepath.Join(dir, "schema_fixed.luau"), []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
	main := hexProbe + fmt.Sprintf(`--!strict
local T = require("./%s")
local F = require("./schema_fixed")
local inputs = require("./inputs")
local fixed = %s
local values = F.load(T.FxRootDescriptor, fixed, inputs.oracle)
local out = F.save(T.FxRootDescriptor, fixed, values)
print(hex(out))
`, mod, "{ hash = T.FxRootFixedHash, recordBytes = T.FxRootFixedRecordBytes, layout = T.FxRootFixedLayout }")
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luaurun.Run(t, dir, "main.luau", map[string][]byte{"oracle": oracle})
	got, err := hex.DecodeString(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("the patched probe printed %q: %v", out, err)
	}
	if string(got) == string(oracle) {
		t.Fatal("the negative control stayed green: skipping the last field must move the oracle bytes")
	}
}
