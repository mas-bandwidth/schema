package mootable_test

// THE FIXED FORM ON THE MOO LEG (docs/SPEC-TABLES.md §3.4,
// docs/FIXED-FORM-ALGORITHM.md). The bytes are the C++ reference's: `make
// tables-fixedform-corpus` writes build/fixedform-corpus and its manifest.txt,
// the values the dump wrote into each file. The corpus is a SKIP on a bare
// tree and a FAILURE under SCHEMA_REQUIRE_CORPUS=1, as on every other leg.

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"maps"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/mootable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func fixedCorpus(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(root, "build", "fixedform-corpus")
	if _, err := os.Stat(filepath.Join(dir, "manifest.txt")); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") == "" {
			t.Skip("the C++ reference's fixed-form corpus is absent (make tables-fixedform-corpus); SCHEMA_REQUIRE_CORPUS unset")
		}
		t.Fatalf("the fixed-form corpus is absent: build it with make tables-fixedform-corpus: %v", err)
	}
	return dir
}

func corpusFile(t *testing.T, corpus, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(corpus, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// a manifest line: records= and the values, normalised so a float compares
// by its bits and a fixed-point value by its raw integer.
type manifestLine struct {
	records int
	values  map[string]string
}

var manifestValueSplit = regexp.MustCompile(`,(r\d+\.)`)

func readFixedManifest(t *testing.T, corpus string) map[string]manifestLine {
	t.Helper()
	out := map[string]manifestLine{}
	for line := range strings.SplitSeq(string(corpusFile(t, corpus, "manifest.txt")), "\n") {
		head, values, _ := strings.Cut(line, " values=")
		var file string
		ml := manifestLine{values: map[string]string{}}
		for field := range strings.FieldsSeq(head) {
			k, v, _ := strings.Cut(field, "=")
			switch k {
			case "file":
				file = v
			case "records":
				ml.records, _ = strconv.Atoi(v)
			}
		}
		if file == "" {
			continue
		}
		// a value may hold a comma inside its quotes: split only before the
		// next r<k>. key
		for kv := range strings.SplitSeq(manifestValueSplit.ReplaceAllString(values, "\x00$1"), "\x00") {
			k, v, ok := strings.Cut(kv, "=")
			if ok {
				ml.values[k] = normaliseManifestValue(v)
			}
		}
		out[file] = ml
	}
	return out
}

func normaliseManifestValue(v string) string {
	if _, raw, ok := strings.Cut(v, "|raw="); ok {
		return "raw=" + raw
	}
	if _, bits, ok := strings.Cut(v, "|0x"); ok {
		return "0x" + strings.ToUpper(bits)
	}
	return v
}

// fixedUnit generates a unit for moo with a handed lineage and writes it.
func fixedUnit(t *testing.T, u *ir.Unit, lineage map[string][]mootable.FixedLineageEntry) *generatedUnit {
	t.Helper()
	files, err := moo.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := mootable.GenerateLineage(u, lineage)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	return writeUnit(t, u, files)
}

func unitFromPath(t *testing.T, rel string) *ir.Unit {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, rel)})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// fixedDumper is moo that prints a value in the manifest's own spelling,
// normalised as normaliseManifestValue normalises the manifest.
const fixedDumper = `local St = require("./schema_tables")
local V = require("./table_values")
local lines: { string } = {}
local function emit(k: string, v: string)
	lines[#lines + 1] = k .. "=" .. v
end
local function int(n: number): string
	return string.format("%d", n)
end
local dumpRecord: any
local function dumpValue(f: any, v: any, path: string, length: any)
	local s = f.s
	if s == "bool" then
		emit(path, if v then "1" else "0")
	elseif s == "int" then
		if f.cls == "fx" then
			emit(path, "raw=" .. V.Int64.toString(V.Int64.fromNumberRaw(v)))
		elseif f.fbits ~= nil then
			emit(path, "raw=" .. (if type(v) == "number" then int(v) else tostring(v)))
		elseif type(v) == "number" then
			emit(path, int(v))
		else
			emit(path, tostring(v))
		end
	elseif s == "float" then
		if f.f32 then
			emit(path, string.format("0x%08X", v))
		else
			emit(path, string.format("0x%08X%08X", v[2], v[1]))
		end
	elseif s == "enum" then
		emit(path, int(v))
	elseif s == "str" then
		if f.wide then
			emit(path .. "_length", int(length))
		else
			emit(path, "\"" .. v .. "\"")
			emit(path .. "_length", int(#v))
		end
	elseif s == "bytes" then
		for i = 0, length - 1 do
			emit(path .. "[" .. int(i) .. "]", int(buffer.readu8(v, i)))
		end
		emit(path .. "_length", int(length))
	elseif s == "tab" then
		dumpRecord(St.lookup(f.t), v, path)
	elseif s == "union" then
		emit(path .. ".type", int(v.Type))
		if v.Type > 0 then
			local a = St.lookup(f.t).arms[v.Type]
			if a.s ~= "void" then
				local extra = v[a.slot .. "Length"]
				if a.s == "arr" and a.array == "counted" then
					extra = v[a.slot .. "Count"]
				end
				dumpValue(a, v[a.slot], path .. "." .. a.name, extra)
			end
		end
	elseif s == "arr" then
		local count = f.bound
		if f.array == "counted" then
			count = length
			emit(path .. "_count", int(count))
		end
		for i = 1, count do
			local x = if type(v) == "buffer" then buffer.readu8(v, i - 1) else v[i]
			dumpValue(f.e, x, path .. "[" .. int(i - 1) .. "]", nil)
		end
	end
end
dumpRecord = function(d: any, v: any, path: string)
	for _, f in d.fields do
		local p = path .. "." .. f.name
		if f.present ~= nil then
			emit(p .. "_present", if v[f.present] then "1" else "0")
			if not v[f.present] then
				continue
			end
		end
		local extra = v[f.slot .. "Length"]
		if f.s == "arr" and f.array == "counted" then
			extra = v[f.slot .. "Count"]
		end
		dumpValue(f, v[f.slot], p, extra)
	end
end
`

// fixedLoadProbe loads inputs.file at root through table_fixed.moo and prints
// "ok <n> <counters>", then the dump, then the re-saved bytes as hex; or
// "refused <reason> <field> <malformed> <layoutHash>".
func fixedLoadProbe(g *generatedUnit, mod, rootName string) string {
	return "--!strict\nlocal inputs = require(\"./inputs\")\nlocal F = require(\"./table_fixed\")\n" + g.requireAll() +
		fmt.Sprintf("local T = require(\"./%s\")\n", mod) + fixedDumper + fmt.Sprintf(`local d = T.%[1]sDescriptor
local fixed = T.%[1]sFixed
local report: any = {}
local values = F.load(d, fixed, inputs.file, report)
if values == nil then
	print("refused " .. tostring(report.reason) .. " " .. tostring(report.field) .. " " .. tostring(report.malformed) .. " " .. (if report.layoutHash ~= nil then V.idHex(report.layoutHash) else "-"))
	return
end
print(string.format("ok %%d u%%d k%%d c%%d w%%d", #values, report.unknown, report.kindMismatch, report.clamped, report.widened))
for i, v in values do
	dumpRecord(d, v, "r" .. int(i - 1))
end
for _, l in lines do
	print(l)
end
local out, why = F.save(d, fixed, values)
if out == nil then
	print("SAVENIL " .. why.name .. " " .. tostring(why.field))
	return
end
local parts = table.create(buffer.len(out))
for i = 0, buffer.len(out) - 1 do
	parts[i + 1] = string.format("%%02x", buffer.readu8(out, i))
end
print("x" .. table.concat(parts))
`, rootName)
}

type fixedRun struct {
	head  string
	dump  map[string]string
	saved []byte
}

func runFixedLoad(t *testing.T, g *generatedUnit, mod, rootName string, file []byte) fixedRun {
	t.Helper()
	out := g.run(t, fixedLoadProbe(g, mod, rootName), map[string][]byte{"file": file})
	lines := strings.Split(out, "\n")
	r := fixedRun{head: lines[0], dump: map[string]string{}}
	for _, l := range lines[1:] {
		if strings.HasPrefix(l, "x") {
			var err error
			if r.saved, err = hexDecode(strings.TrimPrefix(l, "x")); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if strings.HasPrefix(l, "SAVENIL") {
			t.Fatalf("the save refused what the load landed: %s", l)
		}
		k, v, _ := strings.Cut(l, "=")
		r.dump[k] = v
	}
	return r
}

func hexDecode(s string) ([]byte, error) {
	out := make([]byte, len(s)/2)
	for i := range out {
		v, err := strconv.ParseUint(s[2*i:2*i+2], 16, 8)
		if err != nil {
			return nil, err
		}
		out[i] = byte(v)
	}
	return out, nil
}

// compareManifest holds every manifest value the reader names to the value it
// landed; a key the reader does not name (a field it does not declare, a wide
// string's units) is not compared. It answers how many compared.
func compareManifest(t *testing.T, what string, ml manifestLine, dump map[string]string) int {
	t.Helper()
	compared := 0
	for k, want := range ml.values {
		got, ok := dump[k]
		if !ok || strings.HasPrefix(want, "u\"") || strings.Contains(want, "\\") {
			continue
		}
		// the reference spells an element of a fixed-point array by its raw
		// integer alone, and a field as digits|raw=R
		if raw, ok := strings.CutPrefix(got, "raw="); ok && !strings.HasPrefix(want, "raw=") {
			got = raw
		}
		if widened, ok := widenedFloat32(want, got); ok {
			want = widened
		}
		if got != want {
			t.Errorf("%s: %s landed %s, the reference wrote %s", what, k, got, want)
		}
		compared++
	}
	return compared
}

// widenedFloat32 is the float64 pattern a float32 pattern widens to, when the
// reference wrote a float32 (0x and eight digits) and the read landed a float64
// (0x and sixteen): the float rung is the ordinary conversion, exact for every
// finite float32 (SPEC-TABLES §3.3, §4).
func widenedFloat32(want, got string) (string, bool) {
	if len(want) != 10 || len(got) != 18 || !strings.HasPrefix(want, "0x") || !strings.HasPrefix(got, "0x") {
		return "", false
	}
	bits, err := strconv.ParseUint(want[2:], 16, 32)
	if err != nil {
		return "", false
	}
	return fmt.Sprintf("0x%016X", math.Float64bits(float64(math.Float32frombits(uint32(bits))))), true
}

var fixedOracleVectors = []struct{ file, schema, root string }{
	{"fx1.bin", "test/tables/FX1.schema", "FxRoot"},
	{"fx2.bin", "test/tables/FX2.schema", "FxRoot"},
	{"p1.bin", "test/tables/P1.schema", "Chain"},
	{"p3.bin", "test/tables/P3.schema", "Chain"},
	{"keyed.bin", "tables/examples/Keyed.schema", "KeyedConfig"},
	{"pack.bin", "tables/examples/Pack.schema", "PackConfig"},
	{"fu1.bin", "test/tables/FU1.schema", "FuRoot"},
	{"fu2.bin", "test/tables/FU2.schema", "FuRoot"},
}

// TestMooFixedFormAgreesWithTheByteOracle is §7 row 1 over the reference's
// nine corpus files: each is read through the emitted reader, every value the
// manifest records lands, the record count is the manifest's, no counter
// moves, and the value saved back is the reference's file byte for byte.
// Every float in the corpus is finite (SPEC-TABLES §3.3): fu1 and fu2 carry a
// subnormal and a -0 in `heat`, the values a wrong widening would move.
func TestMooFixedFormAgreesWithTheByteOracle(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	corpus := fixedCorpus(t)
	manifest := readFixedManifest(t, corpus)
	for _, v := range fixedOracleVectors {
		t.Run(strings.TrimSuffix(v.file, ".bin"), func(t *testing.T) {
			t.Parallel()
			u := unitFromPath(t, v.schema)
			g := fixedUnit(t, u, nil)
			mod := strings.TrimSuffix(filepath.Base(v.schema), ".schema") + "Table"
			oracle := corpusFile(t, corpus, v.file)
			ml := manifest[v.file]
			r := runFixedLoad(t, g, mod, v.root, oracle)
			if want := fmt.Sprintf("ok %d u0 k0 c0 w0", ml.records); r.head != want {
				t.Fatalf("the read: %s, want %s", r.head, want)
			}
			if n := compareManifest(t, v.file, ml, r.dump); n == 0 {
				t.Fatalf("no manifest value compared")
			}
			if string(r.saved) != string(oracle) {
				t.Fatalf("the save is not the reference's bytes: %d saved, %d in the file; %s", len(r.saved), len(oracle), firstDiff(r.saved, oracle))
			}
		})
	}
}

// TestMooFixedFormByteOracleNegativeControl plants one write into the emitted
// table_fixed.moo — an integer's store skipped — and requires fx1's byte
// comparison to go red.
func TestMooFixedFormByteOracleNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	corpus := fixedCorpus(t)
	g := fixedUnit(t, unitFromPath(t, "test/tables/FX1.schema"), nil)
	path := filepath.Join(g.dir, "table_fixed.moo")
	src := string(g.files["table_fixed.moo"])
	const anchor = "\t\tV.writeRaw(out, off, n.size, signed, x)\n"
	if strings.Count(src, anchor) != 1 {
		t.Fatal("the integer store anchor is not in table_fixed.moo exactly once")
	}
	if err := os.WriteFile(path, []byte(strings.Replace(src, anchor, "\t\tlocal _ = x\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	oracle := corpusFile(t, corpus, "fx1.bin")
	r := runFixedLoad(t, g, "FX1Table", "FxRoot", oracle)
	if string(r.saved) == string(oracle) {
		t.Fatal("the planted write still saves the reference's bytes: the gate proves nothing")
	}
}

// the versioning rows the corpus carries an old_ and a new_ file for.
type versionRow struct {
	row      string
	sameHash bool
	widened  int // -1: at least one
	unknown  int
	noNewer  bool // the corpus carries no new_ file (the row's second column is its own test)
}

var mooVersionRows = []versionRow{
	{row: "array_bounded_grow"},
	{row: "array_elem_widen", widened: 4},
	{row: "array_fixed_grow"},
	{row: "bits_grow"},
	{row: "bytes_grow"},
	{row: "cfloat_range_widen"},
	{row: "cfloat_res_refine"},
	{row: "constant_grow"},
	{row: "enum_append"},
	{row: "enum_width", widened: -1},
	{row: "field_append"},
	{row: "field_deprecate", sameHash: true},
	{row: "field_undeprecate", sameHash: true},
	{row: "fixed_I_grow", widened: -1},
	{row: "fixed_I_grow_element", widened: -1},
	{row: "flags_append"},
	{row: "float_widen", widened: -1},
	{row: "int_widen", widened: -1},
	{row: "keyed_array_enum_append"},
	{row: "nested_append"},
	{row: "optional_add"},
	{row: "range_widen"},
	{row: "rename_without_was", sameHash: true},
	{row: "string_grow"},
	{row: "uint_widen", widened: -1},
	{row: "union_append"},
	{row: "union_arm_payload_widen"},
	{row: "unknown_census", unknown: 1, noNewer: true},
}

func readTestSchema(t *testing.T, base string) *ir.Unit {
	t.Helper()
	return unitFromPath(t, filepath.Join("test", "tables", base+".schema"))
}

// rootOf is §5.9 #9's rule: the file's root is the fixed root no other fixed
// table names by value.
func rootOf(t *testing.T, u *ir.Unit) string {
	t.Helper()
	roots := ir.TableFixedFormRoots(u)
	named := map[string]bool{}
	for _, st := range roots {
		for _, f := range st.Fields {
			named[f.Type.Name] = true
		}
	}
	for _, st := range roots {
		if !named[st.Name] {
			return st.Name
		}
	}
	t.Fatal("no fixed root")
	return ""
}

func lineageOf(t *testing.T, older []*ir.Unit, retire int) map[string][]mootable.FixedLineageEntry {
	t.Helper()
	out := map[string][]mootable.FixedLineageEntry{}
	for i, o := range older {
		for _, st := range ir.TableFixedFormRoots(o) {
			e, ok := mootable.FixedLineageOf(o, st.Name)
			if !ok {
				t.Fatalf("no lineage entry for %s", st.Name)
			}
			e.Retired = i < retire
			out[st.Name] = append(out[st.Name], e)
		}
	}
	return out
}

// TestMooFixedVersioningNewReadsOld is every versioning row's NEW-READS-OLD
// column (docs/FIXED-FORM-ALGORITHM.md §5.7): the newer reader, handed the
// older unit's locked entry as its lineage, reads the reference's old_ file —
// every manifest value it names lands, no refusal, the counters §5.4 owes
// (an append is not an event; a widening row moves widened, array_elem_widen
// exactly four per record; a dropped field is one `unknown` per peer).
func TestMooFixedVersioningNewReadsOld(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	corpus := fixedCorpus(t)
	manifest := readFixedManifest(t, corpus)
	for _, r := range mooVersionRows {
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			older := readTestSchema(t, "VOLD_"+r.row)
			newer := readTestSchema(t, "VNEW_"+r.row)
			rootName := rootOf(t, older)
			g := fixedUnit(t, newer, lineageOf(t, []*ir.Unit{older}, 0))
			file := "old_" + r.row + ".bin"
			ml := manifest[file]
			got := runFixedLoad(t, g, "VNEW_"+r.row+"Table", rootName, corpusFile(t, corpus, file))
			if !strings.HasPrefix(got.head, fmt.Sprintf("ok %d u%d k0 c0 w", ml.records, r.unknown)) {
				t.Fatalf("NEW-READS-OLD %s: %s", r.row, got.head)
			}
			w, _ := strconv.Atoi(got.head[strings.LastIndex(got.head, "w")+1:])
			switch {
			case r.widened > 0 && w != r.widened*ml.records:
				t.Errorf("widened %d, the row owes exactly %d per record", w, r.widened)
			case r.widened < 0 && w == 0:
				t.Errorf("a widening row moved no widened counter")
			case r.widened == 0 && w != 0:
				t.Errorf("widened %d, and an append is not an event", w)
			}
			if n := compareManifest(t, file, ml, got.dump); n == 0 {
				t.Errorf("no manifest value compared")
			}
		})
	}
}

// TestMooFixedFormRefusesTheForgedNaN is the finite rule's must-refuse vector
// on the fixed form (SPEC-TABLES §3.3), forged as the reference's own test
// forges it (test/tables/fixedform_main.cpp, fu_refusal_case): the float32 the
// reference wrote into old_float_widen.bin, -1234.5625, is overwritten with a
// signalling NaN, and the newer reader refuses the file as non_finite_float,
// naming the field, before it widens anything.
func TestMooFixedFormRefusesTheForgedNaN(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	corpus := fixedCorpus(t)
	older := readTestSchema(t, "VOLD_float_widen")
	newer := readTestSchema(t, "VNEW_float_widen")
	rootName := rootOf(t, older)
	g := fixedUnit(t, newer, lineageOf(t, []*ir.Unit{older}, 0))
	file := append([]byte(nil), corpusFile(t, corpus, "old_float_widen.bin")...)
	written := binary.LittleEndian.AppendUint32(nil, 0xC49A5200)
	at := strings.Index(string(file), string(written))
	if at < 0 {
		t.Fatal("old_float_widen.bin does not hold the reference's float32 -1234.5625")
	}
	binary.LittleEndian.PutUint32(file[at:], 0x7F8ABCDE)
	got := runFixedLoad(t, g, "VNEW_float_widenTable", rootName, file)
	if want := fmt.Sprintf("refused non_finite_float %s.v false -", rootName); got.head != want {
		t.Fatalf("the forged NaN: %s, want %s", got.head, want)
	}
}

// TestMooFixedVersioningOldRefusesNew is every row's OLD-REFUSES-NEW column:
// the older reader refuses the reference's new_ file layout_unknown BEFORE any
// record, reporting the file's hash, with malformed false — except a row
// whose edit moved no layout byte and no digest byte, which READS in both
// directions (§5.7).
func TestMooFixedVersioningOldRefusesNew(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	corpus := fixedCorpus(t)
	for _, r := range mooVersionRows {
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			if r.noNewer {
				t.Skipf("the corpus has no new_%s.bin", r.row)
			}
			older := readTestSchema(t, "VOLD_"+r.row)
			rootName := rootOf(t, older)
			g := fixedUnit(t, older, nil)
			data := corpusFile(t, corpus, "new_"+r.row+".bin")
			got := runFixedLoad(t, g, "VOLD_"+r.row+"Table", rootName, data)
			if r.sameHash {
				if !strings.HasPrefix(got.head, "ok ") {
					t.Fatalf("a row that moved no layout byte must read in both directions: %s", got.head)
				}
				return
			}
			want := fmt.Sprintf("refused layout_unknown nil false #%016x", binary.LittleEndian.Uint64(data[8:16]))
			if got.head != want {
				t.Fatalf("OLD-REFUSES-NEW %s: %s, want %s", r.row, got.head, want)
			}
		})
	}
}

// TestMooFixedVersioningFloor: the floor is one number and the lineage one
// array (§5.2): a file below it refuses layout_unsupported with its hash, one
// at or above it reads.
func TestMooFixedVersioningFloor(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	corpus := fixedCorpus(t)
	older, mid, newer := readTestSchema(t, "VOLD_floor"), readTestSchema(t, "VMID_floor"), readTestSchema(t, "VNEW_floor")
	g := fixedUnit(t, newer, lineageOf(t, []*ir.Unit{older, mid}, 1))
	old := corpusFile(t, corpus, "old_floor.bin")
	if got := runFixedLoad(t, g, "VNEW_floorTable", "Floored", old).head; got != fmt.Sprintf("refused layout_unsupported nil false #%016x", binary.LittleEndian.Uint64(old[8:16])) {
		t.Errorf("below the floor: %s", got)
	}
	if got := runFixedLoad(t, g, "VNEW_floorTable", "Floored", corpusFile(t, corpus, "mid_floor.bin")).head; !strings.HasPrefix(got, "ok 1 ") {
		t.Errorf("at the floor: %s", got)
	}
	if got := runFixedLoad(t, g, "VNEW_floorTable", "Floored", corpusFile(t, corpus, "new_floor.bin")).head; !strings.HasPrefix(got, "ok 1 ") {
		t.Errorf("the reader's own file: %s", got)
	}
	// the control: with no floor the old file reads
	g = fixedUnit(t, newer, lineageOf(t, []*ir.Unit{older, mid}, 0))
	if got := runFixedLoad(t, g, "VNEW_floorTable", "Floored", old).head; !strings.HasPrefix(got, "ok 1 ") {
		t.Errorf("the control, no floor: %s", got)
	}
}

// TestMooFixedLineageLockBugFailsTheBuild (§5.9 #8, #26): a handed entry whose
// layout does not frame, or whose record size is not its layout's, fails the
// build naming the table; the control, a lawful entry, builds.
func TestMooFixedLineageLockBugFailsTheBuild(t *testing.T) {
	t.Parallel()
	u := readTestSchema(t, "VNEW_field_append")
	older := readTestSchema(t, "VOLD_field_append")
	good := lineageOf(t, []*ir.Unit{older}, 0)
	if _, err := mootable.GenerateLineage(u, good); err != nil {
		t.Fatalf("the control, a lawful entry: %v", err)
	}
	for name, mutate := range map[string]func(e *mootable.FixedLineageEntry){
		"layout": func(e *mootable.FixedLineageEntry) { e.Layout = e.Layout[:len(e.Layout)-1] },
		"record": func(e *mootable.FixedLineageEntry) { e.Record++ },
	} {
		bad := map[string][]mootable.FixedLineageEntry{}
		for k, v := range good {
			cp := append([]mootable.FixedLineageEntry(nil), v...)
			cp[0].Layout = append([]byte(nil), cp[0].Layout...)
			mutate(&cp[0])
			bad[k] = cp
		}
		if _, err := mootable.GenerateLineage(u, bad); err == nil || !strings.Contains(err.Error(), "LOCK") {
			t.Errorf("a lock bug in the %s did not fail the build: %v", name, err)
		}
	}
}

// fx1Offsets is where fx1.bin's first record and its fields sit.
func fx1Offsets(oracle []byte) (layoutLen, rec int) {
	layoutLen = int(binary.LittleEndian.Uint32(oracle[16:20]))
	return layoutLen, 20 + layoutLen
}

// TestMooFixedFormRefusesByName is §5.3's refusal table over images cut from
// the reference's fx1.bin: each refused by its own name, malformed exactly
// where §5.3 sets it, the layout refusals reporting the file's hash, and
// every refusal total (nothing lands). The control, the uncut file, reads.
func TestMooFixedFormRefusesByName(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	corpus := fixedCorpus(t)
	oracle := corpusFile(t, corpus, "fx1.bin")
	g := fixedUnit(t, unitFromPath(t, "test/tables/FX1.schema"), nil)
	_, rec := fx1Offsets(oracle)
	body := rec + 8 // the first record's body
	cut := func(f func(b []byte) []byte) []byte { return f(append([]byte(nil), oracle...)) }
	// FxRoot: keep u32 @0, narrow u16 @4, renamed @6, gone @10, nested @14
	// (8), label @22 (u32 length + 8), marks @34 (u32 count + 4 x i32)
	cases := []struct {
		name string
		file []byte
		want string
	}{
		{"empty", []byte{}, "refused truncated nil true -"},
		{"form 1", []byte{1}, "refused previous_form nil false -"},
		{"form 2", []byte{2, 0, 0}, "refused message_form_as_file nil false -"},
		{"form 9", []byte{9}, "refused newer_form nil false -"},
		{"short header", oracle[:19], "refused truncated nil true -"},
		{"reserved byte", cut(func(b []byte) []byte { b[3] = 1; return b }), "refused reserved_not_zero nil true -"},
		{"layout past the file", cut(func(b []byte) []byte { binary.LittleEndian.PutUint32(b[16:], uint32(len(b))); return b }), "refused layout_malformed nil false -"},
		{"unknown hash", cut(func(b []byte) []byte { b[8] ^= 1; return b }), fmt.Sprintf("refused layout_unknown nil false #%016x", binary.LittleEndian.Uint64(oracle[8:16])^1)},
		{"layout bytes differ", cut(func(b []byte) []byte { b[20+4+8+1] ^= 1; return b }), "refused layout_malformed nil false -"},
		{"ragged tail", oracle[:len(oracle)-1], "refused truncated nil true -"},
		{"record hash", cut(func(b []byte) []byte { b[rec+(len(b)-rec)/2] ^= 1; return b }), "refused no_layout nil false -"},
		{"count past the bound", cut(func(b []byte) []byte { binary.LittleEndian.PutUint32(b[body+34:], 5); return b }), "refused count_over_bound nil false -"},
		{"length past the bound", cut(func(b []byte) []byte { binary.LittleEndian.PutUint32(b[body+22:], 9); return b }), "refused length_over_bound nil false -"},
		{"ill-formed text", cut(func(b []byte) []byte { b[body+26] = 0xFF; return b }), "refused text_ill_formed nil false -"},
	}
	for _, c := range cases {
		got := runFixedLoad(t, g, "FX1Table", "FxRoot", c.file).head
		if got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	if got := runFixedLoad(t, g, "FX1Table", "FxRoot", oracle).head; !strings.HasPrefix(got, "ok 2 ") {
		t.Errorf("the control, the reference's file: %s", got)
	}
}

// TestMooFixedFormIntegrity: a root that declares integrity = "crc32c" writes
// the CRC-32C trailer (hash/crc32's Castagnoli table is the independent sum)
// and reads it back; one flipped byte refuses integrity_mismatch.
func TestMooFixedFormIntegrity(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	u := unitFromSource(t, "package probe\nfixed table Sealed | integrity = \"crc32c\"\n{\n    id uint32 = 7\n    name string(8)\n}\n")
	g := fixedUnit(t, u, nil)
	main := "--!strict\nlocal F = require(\"./table_fixed\")\nlocal T = require(\"./ProbeTable\")\n" + `local v = T.NewSealed()
v.Name = "seal"
local out = F.save(T.SealedDescriptor, T.SealedFixed, { v }) :: buffer
local parts = table.create(buffer.len(out))
for i = 0, buffer.len(out) - 1 do
	parts[i + 1] = string.format("%02x", buffer.readu8(out, i))
end
print(table.concat(parts))
`
	saved, err := hexDecode(g.run(t, main, nil))
	if err != nil {
		t.Fatal(err)
	}
	n := len(saved) - 4
	if binary.LittleEndian.Uint32(saved[n:]) != crc32.Checksum(saved[:n], crc32.MakeTable(crc32.Castagnoli)) {
		t.Fatalf("the trailer is not the CRC-32C of the bytes before it: %x", saved)
	}
	if got := runFixedLoad(t, g, "ProbeTable", "Sealed", saved); !strings.HasPrefix(got.head, "ok 1 ") || string(got.saved) != string(saved) {
		t.Fatalf("the sealed file does not round-trip: %s", got.head)
	}
	bad := append([]byte(nil), saved...)
	bad[n-1] ^= 1
	if got := runFixedLoad(t, g, "ProbeTable", "Sealed", bad).head; got != "refused integrity_mismatch nil false -" {
		t.Fatalf("a flipped byte: %s", got)
	}
}

// TestMooFixedFormRefusesNonFiniteOnWrite: a NaN or an infinity in a float
// field refuses the save as non_finite_float, naming the field (§3.3); the
// control, a finite value, saves.
func TestMooFixedFormRefusesNonFiniteOnWrite(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	u := unitFromSource(t, "package probe\nfixed table Heat\n{\n    a float32\n    b float64\n}\n")
	g := fixedUnit(t, u, nil)
	main := "--!strict\nlocal F = require(\"./table_fixed\")\nlocal T = require(\"./ProbeTable\")\nlocal serialize = require(\"./serialize\")\n" + `local function try(a: number, blo: number, bhi: number): string
	local v = T.NewHeat()
	v.A = a
	v.B = serialize.Uint64.new(blo, bhi)
	local out, why = F.save(T.HeatDescriptor, T.HeatFixed, { v })
	if out ~= nil then return "saved" end
	return why.name .. " " .. tostring(why.field)
end
print(try(0x7FC00000, 0, 0))
print(try(0x3F800000, 0, 0x7FF00000))
print(try(0x3F800000, 0, 0x3FF00000))
`
	if got, want := g.run(t, main, nil), "non_finite_float Heat.a\nnon_finite_float Heat.b\nsaved"; got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

// TestMooReadStrictDispatchesTheForm (§6.5, §3.4): table_refusal.moo answers
// a fixed root's form 3 through its fixed reader and refuses forms 1, 2 and
// any other byte by direction; a variable root reads form 1 and refuses form
// 3 as newer_form.
func TestMooReadStrictDispatchesTheForm(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table runtime)")
	t.Parallel()
	u := unitFromSource(t, "package probe\nfixed table Fx\n{\n    a uint32 = 3\n}\ntable Vx\n{\n    a uint32 = 3\n}\n")
	g := fixedUnit(t, u, nil)
	main := "--!strict\nlocal R = require(\"./table_refusal\")\nlocal F = require(\"./table_fixed\")\nlocal T = require(\"./ProbeTable\")\n" + `local v = T.NewFx()
v.A = 9
local fx = F.save(T.FxDescriptor, T.FxFixed, { v }) :: buffer
local function read(root: string, b: buffer): string
	local rep: any = {}
	local got = R.ReadStrict(T, root, b, rep)
	if got == nil then return "refused " .. tostring(rep.reason) end
	return "read " .. tostring(got.A)
end
print(read("Fx", fx))
print(read("Fx", buffer.fromstring("\x01\x00\x00\x00\x00\x00\x00\x00\x00")))
print(read("Fx", buffer.fromstring("\x02\x00\x00")))
print(read("Fx", buffer.fromstring("\x07")))
print(read("Vx", fx))
print(read("Vx", buffer.fromstring("\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00")))
`
	want := "read 9\nrefused previous_form\nrefused message_form_as_file\nrefused newer_form\nrefused newer_form\nread 3"
	if got := g.run(t, main, nil); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

// TestMooFixedVersioningNegativeControl plants one edit into the emitted
// table_fixed.moo — the plan's id match broken, so no writer field resolves to
// a field of this reader's — and requires a NEW-READS-OLD row the gate holds
// green to go red: the plan's matching is what the versioning gate measures.
func TestMooFixedVersioningNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	corpus := fixedCorpus(t)
	older, newer := readTestSchema(t, "VOLD_field_append"), readTestSchema(t, "VNEW_field_append")
	g := fixedUnit(t, newer, lineageOf(t, []*ir.Unit{older}, 0))
	file := corpusFile(t, corpus, "old_field_append.bin")
	if got := runFixedLoad(t, g, "VNEW_field_appendTable", "Lineage", file).head; got != "ok 1 u0 k0 c0 w0" {
		t.Fatalf("the control, unpatched: %s", got)
	}
	const anchor = "\t\tlocal f = byId[c.id]\n"
	src := string(g.files["table_fixed.moo"])
	if strings.Count(src, anchor) != 1 {
		t.Fatal("the id-match anchor is not in table_fixed.moo exactly once")
	}
	if err := os.WriteFile(filepath.Join(g.dir, "table_fixed.moo"), []byte(strings.Replace(src, anchor, "\t\tlocal f = byId[c.id .. \"!\"]\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := runFixedLoad(t, g, "VNEW_field_appendTable", "Lineage", file).head; got == "ok 1 u0 k0 c0 w0" {
		t.Fatalf("the broken match still reads clean: %s", got)
	}
}
