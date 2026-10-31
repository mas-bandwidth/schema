package luautable_test

// THE VERSIONING FIXTURES ON THE LUAU LEG (docs/FIXED-FORM-VERSIONING-TESTS.md,
// the procedures in docs/FIXED-FORM-ALGORITHM.md §5). Every row of that page is
// ONE definition change and owes TWO read columns:
//
//	NEW-READS-OLD    the widened reader reads the older writer's file: every
//	                 old value lands exactly, the reader's tail is its declared
//	                 default, and the counters are §5.4's — for an APPEND, all
//	                 of them zero, because an append the reader knows is not an
//	                 event.
//	OLD-REFUSES-NEW  the older reader given the widened writer's file refuses
//	                 BEFORE any record, reporting THE FILE'S HASH and nothing
//	                 else, with no counter moved and `malformed` false (§5.3's
//	                 joint answer). Luau answers `layout_unknown`, Go's rule
//	                 (§5.3: `layout_newer` is reserved, SPEC-TABLES §21.3).
//
// The bytes are the C++ reference's: `make tables-fixedform-corpus` writes
// `build/fixedform-corpus/old_<row>.bin` and `new_<row>.bin`, the rows that are
// not a pair (`old_/mid_/new_floor.bin`, `old_/a_/b_/new_lineage_merge.bin`),
// and manifest.txt, THE VALUE ORACLE (§5.9 #32): every value a NEW-READS-OLD
// read lands is asserted against the manifest line of the file it read, path
// by path, never against the bytes under test.
//
// THE PROBE IS THE COLUMN (§5.9 #18): two generations of one table name have no
// spelling in one Luau module tree, so each row's each column is its own
// generated directory, run with this leg's pinned interpreter.
//
// THE LINEAGE IS DATA THE BUILD HANDS THE BACKEND (§5.2): the test plays the
// lock, handing the newer unit the older units' locked entries through
// luautable.GenerateLineage — the wire hash, the layout bytes verbatim and the
// whole record size. A reader never parses the layout a file carries; it
// matches the header's hash against the lineage and compares the bytes.
//
// THE POISON (§5.7, §5.9 #17, #35): this leg's read writes no caller storage
// at all. Every record is a FRESH value from the descriptor's constructor and
// the values are returned only when the whole file read, so the prefill is the
// constructor and REFUSE writes nothing by construction; the poison rows assert
// the declared defaults on the value surface, field by field, which is the
// conforming target §5.9 #35 names for a leg with no byte view.

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"maps"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- the rows ---------------------------------------------------------------

type luauVersionRow struct {
	row string
	// sameHash: the edit moves no layout byte and no digest byte, so the two
	// hashes are equal and the file reads in BOTH directions (§5.7).
	sameHash bool
	// widens: the row grows a WIDTH, so `widened` moves; every other row is an
	// APPEND and owes every counter at zero (§5.9 #10). widenedExact is the
	// exact count where the page gives one (§5.9 #33: four elements into a
	// grown element width are four widen entries, never a folded run).
	widens       bool
	widenedExact int
	// check is Luau asserting what the manifest cannot: the reader's tail.
	check string
}

var luauVersionRows = []luauVersionRow{
	{row: "array_bounded_grow", check: `
for i = 5, 8 do check(v.Vals[i] == nil or num(v.Vals[i]) == 0, "slot " .. i .. " past the writer's count is not empty") end`},
	{row: "array_elem_widen", widens: true, widenedExact: 4},
	{row: "array_fixed_grow", check: `
for i = 5, 8 do
	check(num(v.Vals[i].X) == 7 and num(v.Vals[i].Y) == 9,
		"slot " .. i .. " past the old bound is not the ELEMENT default: " .. tostring(v.Vals[i].X) .. " " .. tostring(v.Vals[i].Y))
end`},
	{row: "bits_grow", widens: true},
	{row: "bytes_grow"},
	{row: "constant_grow"},
	{row: "enum_append"},
	{row: "enum_width", widens: true},
	{row: "field_append", check: `
check(num(v.W) == 77, "the appended field is not its declared default: " .. tostring(v.W))`},
	{row: "field_deprecate", sameHash: true},
	{row: "field_undeprecate", sameHash: true},
	{row: "fixed_I_grow", widens: true},
	{row: "fixed_I_grow_element", widens: true},
	{row: "flags_append"},
	{row: "float_widen", widens: true},
	{row: "int_widen", widens: true, check: `
check(#values == 3, "the int_widen file carries three records, not " .. #values)`},
	{row: "keyed_array_enum_append", check: `
check(num(v.Slots[4].N) == 7, "the slot the appended key opened is not the element default: " .. tostring(v.Slots[4].N))`},
	{row: "nested_append"},
	{row: "optional_add", check: `
check(v.LinkPresent == true, "a T read into ?T is present")`},
	{row: "range_widen"},
	{row: "rename_without_was", sameHash: true},
	{row: "string_grow"},
	{row: "uint_widen", widens: true},
	{row: "union_append"},
	// union_arm_payload_widen is NOT a widen: it appends a field inside an arm,
	// so it owes widened == 0 like every other append (§5.7)
	{row: "union_arm_payload_widen"},
	{row: "wstring_grow"},
}

// ---- NEW-READS-OLD ----------------------------------------------------------

func TestLuauFixedVersioningNewReadsOld(t *testing.T) {
	t.Parallel()
	corpus := luauFixedCorpus(t)
	nonFinite := fixedManifestNonFinite(t, corpus)
	for _, r := range luauVersionRows {
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			reader := luauVersionUnit(t, "VNEW_"+r.row)
			root := luauVersionRoot(t, reader)
			file := "old_" + r.row + ".bin"
			if leaf := nonFinite[file]; leaf != "" {
				// THE REFERENCE STILL WRITES A NaN HERE (the row's float rung);
				// under the finite rule of 2026-10-27 the newer reader refuses
				// it by name, naming the field, before the bounds pass
				// (docs/SPEC-TABLES.md §3.3): a must-refuse vector until the
				// finite-tables plan's C++ card regenerates the corpus, and an
				// ordinary row after (the manifest decides)
				body := fmt.Sprintf(`local report: any = {}
check(LOAD("file", report) == nil and report.reason == "non_finite_float", "the non-finite float the older writer wrote was not refused by name: " .. why(report))
check(report.field ~= nil and string.sub(report.field, -%d) == %q, "the refusal does not name the field: " .. tostring(report.field))
`, len(leaf)+1, "."+leaf)
				luauVersionProbe(t, "VNEW_"+r.row, []string{"VOLD_" + r.row}, 0, map[string][]byte{"file": luauCorpusFile(t, corpus, file)}, body)
				return
			}
			counters := `check(report.widened == 0, "widened " .. report.widened .. ", and an append is not an event")`
			switch {
			case r.widenedExact > 0:
				counters = fmt.Sprintf(`check(report.widened == %d, "widened " .. report.widened .. ", want exactly %d")`, r.widenedExact, r.widenedExact)
			case r.widens:
				counters = `check(report.widened ~= 0, "a widening row moved no widened counter")`
			}
			body := fmt.Sprintf(`local report: any = {}
local values = LOAD("file", report)
check(values ~= nil, "the newer reader refused the older writer's file: " .. why(report))
if values == nil then return end
check(report.reason == nil and report.malformed == false, "a clean NEW-READS-OLD is not a refusal: " .. why(report))
check(report.unknown == 0 and report.kindMismatch == 0 and report.clamped == 0 and report.duplicate == 0,
	"counters moved on a clean backward read: " .. why(report))
%s
local v = values[1]
%s
%s
`, counters, luauManifestChecks(t, corpus, file, root), r.check)
			luauVersionProbe(t, "VNEW_"+r.row, []string{"VOLD_" + r.row}, 0, map[string][]byte{"file": luauCorpusFile(t, corpus, file)}, body)
		})
	}
}

// ---- OLD-REFUSES-NEW --------------------------------------------------------

func TestLuauFixedVersioningOldRefusesNew(t *testing.T) {
	t.Parallel()
	corpus := luauFixedCorpus(t)
	for _, r := range luauVersionRows {
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			data := luauCorpusFile(t, corpus, "new_"+r.row+".bin")
			var body string
			if r.sameHash {
				// no second column: the hashes are equal and the file reads
				// in both directions (§5.7)
				body = `local report: any = {}
local values = LOAD("file", report)
check(values ~= nil and #values >= 1, "a row that moved no layout byte must READ both ways: " .. why(report))
check(report.reason == nil and report.malformed == false, "an equal hash is not a version: " .. why(report))
`
			} else {
				body = fmt.Sprintf(`local report: any = {}
local values = LOAD("file", report)
check(values == nil, "the older reader did not refuse the newer writer's file")
check(report.reason == "layout_unknown", "a hash in no lineage entry owes layout_unknown, not " .. tostring(report.reason))
check(report.layoutHash == %s, "layout_unknown reports THE FILE's hash: " .. tostring(report.layoutHash))
check(report.malformed == false, "a refusal by name never sets malformed too (the joint answer)")
check(report.widened == 0 and report.unknown == 0 and report.kindMismatch == 0 and report.clamped == 0 and report.duplicate == 0,
	"REFUSE is total: no counter moves: " .. why(report))
`, luau.IntegerLit(new(bigInt).SetUint64(binary.LittleEndian.Uint64(data[8:16]))))
			}
			luauVersionProbe(t, "VOLD_"+r.row, nil, 0, map[string][]byte{"file": data}, body)
		})
	}
}

// ---- the floor --------------------------------------------------------------
//
// The floor is ONE NUMBER and the lineage is ONE ARRAY, so "retired" is an
// index cut: below the floor is `layout_unsupported` (upgrade the client),
// outside the lineage `layout_unknown` (§5.2, §5.7's three floor rows).

func TestLuauFixedVersioningFloor(t *testing.T) {
	t.Parallel()
	corpus := luauFixedCorpus(t)
	inputs := map[string][]byte{
		"old": luauCorpusFile(t, corpus, "old_floor.bin"),
		"mid": luauCorpusFile(t, corpus, "mid_floor.bin"),
	}
	older := []string{"VOLD_floor", "VMID_floor"}
	t.Run("floor_at", func(t *testing.T) {
		t.Parallel()
		luauVersionProbe(t, "VNEW_floor", older, 0, inputs, `local report: any = {}
local values = LOAD("old", report)
check(values ~= nil and report.reason == nil and not report.malformed, "a file AT the floor must read: " .. why(report))
if values ~= nil then check(num(values[1].A) == 11, "the oldest file's a did not land") end
`)
	})
	t.Run("floor_below", func(t *testing.T) {
		t.Parallel()
		hash := luau.IntegerLit(new(bigInt).SetUint64(binary.LittleEndian.Uint64(inputs["old"][8:16])))
		luauVersionProbe(t, "VNEW_floor", older, 1, inputs, fmt.Sprintf(`local report: any = {}
local values = LOAD("old", report)
check(values == nil and report.reason == "layout_unsupported", "a file below the floor owes layout_unsupported: " .. why(report))
check(report.layoutHash == %s, "layout_unsupported reports the file's hash too")
check(not report.malformed and report.widened == 0 and report.unknown == 0 and report.clamped == 0, "REFUSE is total: " .. why(report))
local mid: any = {}
check(LOAD("mid", mid) ~= nil, "the file AT the raised floor must still read: " .. why(mid))
`, hash))
	})
	t.Run("floor_raise_live", func(t *testing.T) {
		t.Parallel()
		luauVersionProbe(t, "VNEW_floor", older, 2, inputs, `local report: any = {}
local values = LOAD("mid", report)
check(values == nil and report.reason == "layout_unsupported", "the floor raised by one: yesterday's file must refuse: " .. why(report))
`)
	})
}

// ---- the hash ---------------------------------------------------------------

func TestLuauFixedVersioningHash(t *testing.T) {
	t.Parallel()
	corpus := luauFixedCorpus(t)
	old := luauCorpusFile(t, corpus, "old_field_append.bin")
	fresh := luauCorpusFile(t, corpus, "new_field_append.bin")
	unknown := append([]byte(nil), old...)
	binary.LittleEndian.PutUint64(unknown[8:16], 0x0EADBEEFCAFEF00D)
	differ := append([]byte(nil), old...)
	differ[20+4] ^= 0xFF
	inputs := map[string][]byte{"old": old, "fresh": fresh, "unknown": unknown, "differ": differ}
	older := []string{"VOLD_field_append"}
	// hash_unknown: a hash in no lineage refuses layout_unknown, reporting the
	// FILE's hash and nothing else
	t.Run("hash_unknown", func(t *testing.T) {
		t.Parallel()
		luauVersionProbe(t, "VNEW_field_append", older, 0, inputs, `local report: any = {}
check(LOAD("unknown", report) == nil and report.reason == "layout_unknown", "hash_unknown: " .. why(report))
check(report.layoutHash == 0x0EADBEEFCAFEF00Di, "hash_unknown reports the file's hash")
check(not report.malformed, "hash_unknown is a refusal by name, not malformed")
`)
	})
	// hash_known_bytes_differ: a KNOWN hash over layout bytes that differ from
	// the lock's is layout_malformed, never a runtime walk (§5.3)
	t.Run("hash_known_bytes_differ", func(t *testing.T) {
		t.Parallel()
		luauVersionProbe(t, "VNEW_field_append", older, 0, inputs, `local report: any = {}
check(LOAD("differ", report) == nil and report.reason == "layout_malformed", "hash_known_bytes_differ: " .. why(report))
check(not report.malformed, "a refusal by name never sets malformed too")
`)
	})
	// hash_identity: the reader's own hash selects the identity plan
	t.Run("hash_identity", func(t *testing.T) {
		t.Parallel()
		luauVersionProbe(t, "VNEW_field_append", older, 0, inputs, `local report: any = {}
local values = LOAD("fresh", report)
check(values ~= nil and report.reason == nil and not report.malformed, "hash_identity: " .. why(report))
if values ~= nil then check(num(values[1].W) == 777, "the identity plan lost a value: " .. tostring(values[1].W)) end
`)
	})
}

// ---- lineage_merge ----------------------------------------------------------
//
// Two branches append different fields; after the merge BOTH pre-merge files
// read on the merged build (the name-subset rule, bill §8a.1).

func TestLuauFixedVersioningLineageMerge(t *testing.T) {
	t.Parallel()
	corpus := luauFixedCorpus(t)
	reader := luauVersionUnit(t, "VNEW_lineage_merge")
	root := luauVersionRoot(t, reader)
	inputs := map[string][]byte{}
	var body strings.Builder
	for _, side := range []string{"old", "a", "b"} {
		file := side + "_lineage_merge.bin"
		inputs[side] = luauCorpusFile(t, corpus, file)
		fmt.Fprintf(&body, `do
	local report: any = {}
	local values = LOAD(%q, report)
	check(values ~= nil and report.reason == nil and not report.malformed, %q .. ": every pre-merge file reads on the merged build: " .. why(report))
	if values ~= nil then
		local v = values[1]
%s
	end
end
`, side, file, luauManifestChecks(t, corpus, file, root))
	}
	luauVersionProbe(t, "VNEW_lineage_merge", []string{"VOLD_lineage_merge", "VBRA_lineage_merge", "VBRB_lineage_merge"}, 0, inputs, body.String())
}

// ---- A TAG NO ARM LANDS (§5.2 EMIT kind 15, §5.9 #42) -----------------------
//
// The corpus's own old_union_append.bin (one record, tag 1, the alpha arm
// live) with a SECOND record appended that is the first with its tag forged to
// 9, past the writer's two arms. The newer reader holds three arms, so this is
// the COMPILED plan: record 1 keeps the writer's arm, record 2 lands None and
// counts once — never the previous record's tag.

func TestLuauFixedCompiledPlanTagPastArmSet(t *testing.T) {
	t.Parallel()
	corpus := luauFixedCorpus(t)
	data := luauCorpusFile(t, corpus, "old_union_append.bin")
	at0 := 20 + int(binary.LittleEndian.Uint32(data[16:20]))
	record := data[at0:]
	forged := append(append([]byte(nil), data...), record...)
	forged[len(data)+8] = 9
	luauVersionProbe(t, "VNEW_union_append", []string{"VOLD_union_append"}, 0, map[string][]byte{"file": forged}, `local report: any = {}
local values = LOADT("file", report)
check(values ~= nil and #values == 2, "the two-record file did not read: " .. why(report))
if values == nil then return end
check(report.reason == nil and not report.malformed, "a forged TAG is not a refusal and not malformed: " .. why(report))
check(values[1].Pick.Type == 1 and num(values[1].Pick.Alpha.M) == 7, "record 1 lost the arm the writer named")
check(values[2].Pick.Type == 0, "a tag past the arm set reported an arm: " .. tostring(values[2].Pick.Type))
check(report.clamped == 1, "a tag past the writer's arm set is counted exactly once: " .. why(report))
local strict: any = {}
check(LOAD("file", strict) == nil and strict.reason == "clamped", "the plain read is STRICT and refuses the clamp by name (§6.5): " .. why(strict))
`)
}

// ---- the harness ------------------------------------------------------------

type bigInt = big.Int

func luauFixedCorpus(t *testing.T) string {
	t.Helper()
	slowtest.Gate(t, "the pinned Luau interpreter and the fixed-form corpus")
	dir := filepath.Join(root, "build", "fixedform-corpus")
	if _, err := os.Stat(filepath.Join(dir, "manifest.txt")); err != nil {
		// THE SKIP IS A FAILURE WHEN SOMETHING PROMISED THE CORPUS:
		// `make tables-luau-versioning` builds it first and sets
		// SCHEMA_REQUIRE_CORPUS=1
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and the reference's byte oracle is not in %s: %v", dir, err)
		}
		t.Skip("the reference's byte oracle is not built: make tables-fixedform-corpus")
	}
	return dir
}

func luauCorpusFile(t *testing.T, corpus, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(corpus, name))
	if err != nil {
		t.Fatalf("the corpus file %s: %v", name, err)
	}
	return data
}

func luauVersionUnit(t *testing.T, base string) *ir.Unit {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, "test", "tables", base+".schema")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("load %s: %v", base, err)
	}
	return u
}

// luauVersionRoot is the fixed table a corpus file was written FROM: the
// OUTER one, which no other fixed table of the unit names by value (§5.9 #9).
func luauVersionRoot(t *testing.T, u *ir.Unit) *ir.Struct {
	t.Helper()
	roots := ir.TableFixedFormRoots(u)
	if len(roots) == 0 {
		t.Fatal("a versioning row declares a fixed root")
	}
	named := map[string]bool{}
	for _, st := range roots {
		for _, f := range st.Fields {
			named[f.Type.Name] = true
		}
	}
	for _, st := range roots {
		if !named[st.Name] {
			return st
		}
	}
	return roots[len(roots)-1]
}

const luauVersionPrelude = `--!strict
local T = require("./%[1]s")
local F = require("./schema_fixed")
local inputs = require("./inputs")
local fails = 0
local function check(ok: boolean, what: string)
	if not ok then
		fails += 1
		print("FAIL: " .. what)
	end
end
local function why(r: any): string
	return string.format("reason=%%s malformed=%%s unknown=%%s kind=%%s clamped=%%s widened=%%s duplicate=%%s layoutHash=%%s",
		tostring(r.reason), tostring(r.malformed), tostring(r.unknown), tostring(r.kindMismatch),
		tostring(r.clamped), tostring(r.widened), tostring(r.duplicate), tostring(r.layoutHash))
end
-- a value as a plain number: an integer, a number, or a bool as 0 / 1
local function num(x: any): number
	if typeof(x) == "integer" then return integer.tonumber(x) end
	if typeof(x) == "boolean" then return if x then 1 else 0 end
	return x
end
local bits = buffer.create(8)
local function f32bits(x: number): number
	buffer.writef32(bits, 0, x)
	return buffer.readu32(bits, 0)
end
local function f64bits(x: number): (number, number)
	buffer.writef64(bits, 0, x)
	return buffer.readu32(bits, 0), buffer.readu32(bits, 4)
end
local fixed = { hash = T.%[2]sFixedHash, recordBytes = T.%[2]sFixedRecordBytes, layout = T.%[2]sFixedLayout,
	lineage = T.%[2]sFixedLineage, floor = T.%[2]sFixedFloor }
-- LOAD is the plain read, STRICT; LOADT the tolerant one by its name (§6.5)
local function LOAD(name: string, report: any): { any }?
	return F.read(T.%[2]sDescriptor, fixed, inputs[name], report)
end
local function LOADT(name: string, report: any): { any }?
	return F.loadTolerant(T.%[2]sDescriptor, fixed, inputs[name], report)
end
local _ = num; local _ = f32bits; local _ = f64bits; local _ = LOADT
local function body()
%[3]s
end
body()
if fails == 0 then print("ok") end
`

// luauVersionProbe generates the READER's Luau with the OLDER units' locked
// entries handed to it as its lineage (oldest first, the reader's own last),
// the first `retire` of them retired (which moves the floor), writes ONE
// generated probe beside it and runs it under the pinned interpreter. A
// probe is generated, never hand-written (§5.9 #18).
func luauVersionProbe(t *testing.T, reader string, older []string, retire int, inputs map[string][]byte, body string) {
	t.Helper()
	u := luauVersionUnit(t, reader)
	lineage := map[string][]luautable.FixedLineageEntry{}
	for i, base := range older {
		o := luauVersionUnit(t, base)
		for _, st := range ir.TableFixedFormRoots(o) {
			e, ok := luautable.FixedLineageOf(o, st.Name)
			if !ok {
				t.Fatalf("no lineage entry for %s", st.Name)
			}
			e.Retired = i < retire
			if e.Retired {
				e.Reason = "retired by the test's lock"
			}
			lineage[st.Name] = append(lineage[st.Name], e)
		}
	}
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatalf("generate %s: %v", reader, err)
	}
	tables, err := luautable.GenerateLineage(u, lineage)
	if err != nil {
		t.Fatalf("generate %s: %v", reader, err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	st := luauVersionRoot(t, u)
	var indented strings.Builder
	for line := range strings.SplitSeq(strings.TrimRight(body, "\n"), "\n") {
		if line != "" {
			indented.WriteString("\t" + line)
		}
		indented.WriteString("\n")
	}
	probe := fmt.Sprintf(luauVersionPrelude, reader+"Table", st.Name, strings.TrimRight(indented.String(), "\n"))
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(luaurun.Run(t, dir, "main.luau", inputs)))
	if !strings.HasSuffix(out, "ok") || strings.Contains(out, "FAIL:") {
		t.Fatalf("%s probe:\n%s", reader, out)
	}
}

// ---- the value oracle -------------------------------------------------------

// luauManifestValues is ONE FILE's `values=` out of the corpus manifest, in
// order, as path/value pairs (§5.9 #32). A manifest that is there and does not
// carry the file is a failure by name.
func luauManifestValues(t *testing.T, corpus, file string) [][2]string {
	t.Helper()
	f, err := os.Open(filepath.Join(corpus, "manifest.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "file="+file+" ") {
			continue
		}
		_, values, ok := strings.Cut(line, " values=")
		if !ok {
			t.Fatalf("the manifest line for %s carries no values=", file)
		}
		var out [][2]string
		for _, pair := range luauSplitValues(values) {
			k, v, ok := strings.Cut(pair, "=")
			if !ok {
				t.Fatalf("the manifest pair %q is not path=value", pair)
			}
			out = append(out, [2]string{k, v})
		}
		return out
	}
	t.Fatalf("the manifest carries no line for %s: the oracle and the bytes under test have come apart", file)
	return nil
}

// luauSplitValues splits a values= list at its commas outside a quoted string.
func luauSplitValues(s string) []string {
	var out []string
	quoted, start := false, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			quoted = !quoted
		case ',':
			if !quoted {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// luauManifestChecks is the Luau asserting every value the manifest says the
// WRITER of `file` wrote landed on the reader's value surface, path by path:
// a field by its name or its `was` name, a union's tag and arm, an optional's
// presence, a count, a length, a bytes(N) element, text by its bytes or code
// units, a float by its bits, a fixed-point value by its raw scaled integer.
func luauManifestChecks(t *testing.T, corpus, file string, root *ir.Struct) string {
	t.Helper()
	var b strings.Builder
	for _, kv := range luauManifestValues(t, corpus, file) {
		path, value := kv[0], kv[1]
		rec, rest, ok := strings.Cut(path, ".")
		if !ok || !strings.HasPrefix(rec, "r") {
			t.Fatalf("%s: the manifest path %q names no record", file, path)
		}
		n, err := strconv.Atoi(rec[1:])
		if err != nil {
			t.Fatalf("%s: the manifest path %q names no record", file, path)
		}
		expr, f, kind := luauResolvePath(t, root, fmt.Sprintf("values[%d]", n+1), rest)
		cond := luauValueCheck(t, expr, f, kind, value)
		fmt.Fprintf(&b, "check(%s, %q)\n", cond, file+": "+path+" did not land "+value)
	}
	return b.String()
}

// luauResolvePath walks a manifest path over the reader's IR to the Luau
// expression that holds it.
func luauResolvePath(t *testing.T, root *ir.Struct, expr, path string) (string, *ir.Field, string) {
	t.Helper()
	st := root
	var un *ir.Union
	segs := strings.Split(path, ".")
	for i, seg := range segs {
		last := i == len(segs)-1
		name, idx := seg, -1
		if before, after, ok := strings.Cut(seg, "["); ok {
			name = before
			k, err := strconv.Atoi(strings.TrimSuffix(after, "]"))
			if err != nil {
				t.Fatalf("the manifest path %q has a bad index", path)
			}
			idx = k
		}
		if un != nil {
			if name == "type" && last {
				return expr + ".Type", nil, "int"
			}
			var arm *ir.UnionVariant
			for k := range un.Variants {
				if un.Variants[k].Name == name {
					arm = &un.Variants[k]
				}
			}
			if arm == nil || arm.Ref == nil {
				t.Fatalf("the manifest path %q names no table arm %s", path, name)
			}
			expr += "." + ir.GoExportName(arm.Name)
			st, un = arm.Ref, nil
			continue
		}
		var f *ir.Field
		for _, c := range st.Fields {
			if c.Name == name || (c.WasName != "" && c.WasName == name) {
				f = c
			}
		}
		if f == nil {
			for _, suffix := range []string{"_count", "_length", "_present"} {
				base, ok := strings.CutSuffix(name, suffix)
				if !ok {
					continue
				}
				for _, c := range st.Fields {
					if c.Name == base && last {
						return expr + "." + ir.GoExportName(base+suffix), c, "int"
					}
				}
			}
			t.Fatalf("the manifest path %q names no field %s of %s", path, name, st.Name)
		}
		expr += "." + ir.GoExportName(f.Name)
		if idx >= 0 {
			if f.Type.Kind == ir.TBytes {
				if !last {
					t.Fatalf("the manifest path %q runs past a bytes(N) element", path)
				}
				return fmt.Sprintf("buffer.readu8(%s, %d)", expr, idx), f, "int"
			}
			expr += fmt.Sprintf("[%d]", idx+1)
		}
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			if f.Type.Kind == ir.TNamed && !last {
				st = r
				continue
			}
		case *ir.Union:
			if !last {
				un = r
				continue
			}
		}
		if !last {
			t.Fatalf("the manifest path %q runs past the leaf %s", path, name)
		}
		switch f.Type.Kind {
		case ir.TString:
			return expr, f, "string"
		case ir.TWString:
			return expr, f, "wstring"
		case ir.TFloat32:
			return expr, f, "f32"
		case ir.TFloat64:
			return expr, f, "f64"
		case ir.TFixed:
			return expr, f, "fixed"
		}
		return expr, f, "int"
	}
	t.Fatalf("the manifest path %q ends on a table", path)
	return "", nil, ""
}

// luauValueCheck is the Luau condition one manifest value owes.
func luauValueCheck(t *testing.T, expr string, f *ir.Field, kind, value string) string {
	t.Helper()
	switch kind {
	case "string":
		s, err := strconv.Unquote(value)
		if err != nil {
			t.Fatalf("the manifest text %s does not unquote: %v", value, err)
		}
		return fmt.Sprintf("buffer.readstring(%s, 0, %d) == %s", expr, len(s), luauQuote(s))
	case "wstring":
		if !strings.HasPrefix(value, "u\"") || !strings.HasSuffix(value, "\"") {
			t.Fatalf("the manifest wide text %s is not u\"...\"", value)
		}
		units := luauWideUnits(value)
		var parts []string
		for i, u := range units {
			parts = append(parts, fmt.Sprintf("buffer.readu16(%s, %d) == %d", expr, 2*i, u))
		}
		return strings.Join(parts, " and ")
	case "f32", "f64":
		_, hexBits, ok := strings.Cut(value, "|0x")
		if !ok {
			t.Fatalf("the manifest float %s carries no bits", value)
		}
		raw, err := strconv.ParseUint(hexBits, 16, 64)
		if err != nil {
			t.Fatalf("the manifest float bits %s: %v", value, err)
		}
		if kind == "f32" {
			return fmt.Sprintf("f32bits(%s) == 0x%08x", expr, raw)
		}
		if len(hexBits) <= 8 {
			// an f32 the writer wrote, read by an f64 reader: the widened
			// double's bits, the NaN payload included (§4.5 widenf)
			raw = luauWidenF32(uint32(raw))
		}
		return fmt.Sprintf("(function() local lo, hi = f64bits(%s); return lo == 0x%08x and hi == 0x%08x end)()", expr, uint32(raw), uint32(raw>>32))
	case "fixed":
		// a scalar's line spells the value and the raw scaled integer
		// (`-0.0625|raw=-1`); an element's spells the raw integer alone
		rawText := value
		if _, r, ok := strings.Cut(value, "|raw="); ok {
			rawText = r
		}
		if _, err := strconv.ParseInt(rawText, 10, 64); err != nil {
			t.Fatalf("the manifest fixed-point %s carries no raw value", value)
		}
		return fmt.Sprintf("num(%s) == %s", expr, rawText)
	}
	if _, err := strconv.ParseInt(value, 10, 64); err != nil {
		if _, err := strconv.ParseUint(value, 10, 64); err != nil {
			t.Fatalf("the manifest value %s for %s is not an integer", value, expr)
		}
	}
	_ = f
	return fmt.Sprintf("num(%s) == %s", expr, value)
}

// luauWidenF32 is an f32's bits as the f64 the widen lands: the sign, the
// exponent re-biased (all ones stays all ones), the fraction shifted up 29.
func luauWidenF32(b uint32) uint64 {
	sign := uint64(b>>31) << 63
	exp := (b >> 23) & 0xff
	frac := uint64(b&0x7fffff) << 29
	if exp == 0xff {
		return sign | 0x7ff<<52 | frac
	}
	return math.Float64bits(float64(math.Float32frombits(b)))
}

// luauWideUnits is the UTF-16 code units of the manifest's wide text: its
// \uXXXX escapes are units as they are, lone surrogates included.
func luauWideUnits(raw string) []uint16 {
	body := strings.TrimSuffix(strings.TrimPrefix(raw, "u\""), "\"")
	var out []uint16
	for i := 0; i < len(body); {
		if strings.HasPrefix(body[i:], "\\u") && i+6 <= len(body) {
			v, err := strconv.ParseUint(body[i+2:i+6], 16, 16)
			if err == nil {
				out = append(out, uint16(v))
				i += 6
				continue
			}
		}
		r := []rune(body[i:])[0]
		out = append(out, utf16.Encode([]rune{r})...)
		i += len(string(r))
	}
	return out
}

// luauQuote is s as a Luau string literal, every byte past printable ASCII
// as a decimal escape.
func luauQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x20 && c < 0x7f && c != '"' && c != '\\' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "\\%d", c)
		}
	}
	b.WriteByte('"')
	return b.String()
}
