package gotable

// THE VERSIONING FIXTURES ON THE GO LEG (docs/FIXED-FORM-VERSIONING-TESTS.md,
// the procedures in docs/FIXED-FORM-ALGORITHM.md §5). Every row of that page is
// one definition change and owes TWO read columns:
//
//   NEW-READS-OLD    the widened reader reads the older writer's file: every
//                    old value lands exactly, the reader's tail is its declared
//                    default, and the counters are §5.4's — for an APPEND, all
//                    of them zero, because an append the reader knows is not an
//                    event.
//   OLD-REFUSES-NEW  the older reader given the widened writer's file refuses
//                    `layout_newer` BEFORE any record, reporting THE FILE'S
//                    HASH and nothing else, with no counter moved and
//                    `Malformed` false (§5.3's joint answer).
//
// The bytes are the C++ reference's: `make tables-fixedform-corpus` writes
// `build/fixedform-corpus/old_<row>.bin` and `new_<row>.bin`. The two schemas
// of a row are `test/tables/VOLD_<row>.schema` and `VNEW_<row>.schema`; they
// carry ONE table name in two packages, because the two layouts are ONE
// LINEAGE.
//
// THE LINEAGE IS DATA THE BUILD HANDS THE BACKEND (§5.2, COMPILE(lock, T)):
// here the test plays the lock, handing the newer unit the older unit's locked
// entry — the wire hash, the layout bytes verbatim and the record size. A
// reader NEVER parses the layout a file carries; it matches the header's hash
// against the lineage and compares the bytes it already holds.

import (
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- the rows ---------------------------------------------------------------

type versionRow struct {
	row string
	// sameHash is a row whose edit moves no layout byte and no digest byte
	// (`rename_without_was`, `field_deprecate`, `field_undeprecate`): the two
	// hashes are equal, both sides take the identity plan and the file READS in
	// BOTH directions — which is the test that the hash, and only the hash, is
	// the version (§5.7).
	sameHash bool
	// widens is a row that grows a WIDTH, so `widened` moves at least once;
	// every other row is an APPEND and owes all six counters at zero.
	widens bool
	// check is Go source asserting the landed values, over `back`.
	check string
}

var versionRows = []versionRow{
	{row: "array_bounded_grow"},
	{row: "array_elem_widen", widens: true},
	{row: "array_fixed_grow"},
	{row: "bits_grow", widens: true},
	{row: "bytes_grow"},
	{row: "constant_grow"},
	{row: "enum_append"},
	{row: "enum_width", widens: true},
	{row: "field_append", check: `
	if back[0].X != 11 || back[0].Y != 22 || back[0].Z != 33 {
		t.Fatalf("the old writer's values did not land: %+v", back[0])
	}
	if back[0].W != 77 {
		t.Fatalf("the appended field is not its declared default: %+v", back[0])
	}`},
	{row: "field_deprecate", sameHash: true},
	{row: "field_undeprecate", sameHash: true},
	{row: "fixed_I_grow", widens: true},
	{row: "flags_append"},
	{row: "float_widen", widens: true},
	{row: "int_widen", widens: true, check: `
	if n != 3 {
		t.Fatalf("the int_widen file carries three records, not %d", n)
	}
	want := []int32{-1, -32768, 32767}
	for k := range want {
		if int32(back[k].V) != want[k] {
			t.Fatalf("record %d widened wrong: %+v", k, back[k])
		}
		if back[k].Lead != 0xAAAAAAAA || back[k].Trail != 0xBBBBBBBB {
			t.Fatalf("record %d moved a neighbour: %+v", k, back[k])
		}
	}`},
	{row: "keyed_array_enum_append"},
	{row: "nested_append"},
	{row: "optional_add"},
	{row: "range_widen"},
	{row: "rename_without_was", sameHash: true},
	{row: "string_grow"},
	{row: "uint_widen", widens: true},
	{row: "union_append"},
	{row: "union_arm_payload_widen"},
	{row: "wstring_grow"},
}

// ---- NEW-READS-OLD ----------------------------------------------------------

func TestFixedVersioningNewReadsOld(t *testing.T) {
	corpus := fixedCorpus(t)
	for _, r := range versionRows {
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			newer := readSchema(t, "VNEW_"+r.row)
			older := readSchema(t, "VOLD_"+r.row)
			table := fixedRootName(t, older)
			counters := "if r.Widened != 0 { t.Fatalf(\"widened %d, and an append is not an event\", r.Widened) }"
			if r.widens {
				counters = "if r.Widened == 0 { t.Fatalf(\"a widening row moved no widened counter\") }"
			}
			src := fmt.Sprintf(`package probe

import ("os"; "testing")

func TestNewReadsOld(t *testing.T) {
	data, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	back := make([]%[2]s, 8)
	var r TableReport
	plan := make([]TableFixedEntry, 4096)
	n := %[2]sFixedLoad(back, data, plan, &r)
	if n < 1 {
		t.Fatalf("the newer reader refused the older writer's file: n=%%d %%+v", n, r)
	}
	if r.Reason != "" || r.Malformed || r.Verdict == TableOpenRefused {
		t.Fatalf("a clean NEW-READS-OLD is not a refusal: %%+v", r)
	}
	if r.Unknown != 0 || r.KindMismatch != 0 || r.Clamped != 0 || r.Duplicate != 0 {
		t.Fatalf("counters moved on a clean backward read: %%+v", r)
	}
	%[3]s
	%[4]s
}
`, filepath.Join(corpus, "old_"+r.row+".bin"), table, counters, r.check)
			// The newer unit is handed the OLDER unit's locked lineage entry.
			out, err := runVersionProbe(t, newer, []string{older}, src)
			if err != nil {
				t.Fatalf("NEW-READS-OLD %s: %v\n%s", r.row, err, out)
			}
		})
	}
}

// ---- OLD-REFUSES-NEW --------------------------------------------------------

func TestFixedVersioningOldRefusesNew(t *testing.T) {
	corpus := fixedCorpus(t)
	for _, r := range versionRows {
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			older := readSchema(t, "VOLD_"+r.row)
			table := fixedRootName(t, older)
			file := filepath.Join(corpus, "new_"+r.row+".bin")
			var body string
			if r.sameHash {
				// A row whose edit moves no layout byte and no digest byte has
				// NO second column: the hashes are equal and the file reads in
				// both directions (§5.7).
				body = `	if n < 1 {
		t.Fatalf("a row that moved no layout byte must READ in both directions: n=%d %+v", n, r)
	}
	if r.Reason != "" || r.Malformed {
		t.Fatalf("an equal hash is not a version: %+v", r)
	}`
			} else {
				body = `	if n != -1 {
		t.Fatalf("the older reader did not refuse the newer writer's file: n=%d %+v", n, r)
	}
	if r.Reason != "layout_newer" {
		t.Fatalf("a hash in no lineage entry owes layout_newer, not %q", r.Reason)
	}
	if r.LayoutHash != want {
		t.Fatalf("layout_newer reports THE FILE'S hash: 0x%016x, not 0x%016x", want, r.LayoutHash)
	}
	if r.Malformed {
		t.Fatal("a refusal by name never sets malformed too (§5.3, the joint answer)")
	}
	if r.Widened != 0 || r.Unknown != 0 || r.KindMismatch != 0 || r.Clamped != 0 || r.Duplicate != 0 {
		t.Fatalf("REFUSE is total: no counter moves: %+v", r)
	}
	if back[0] != fresh {
		t.Fatalf("REFUSE wrote destination bytes: %+v", back[0])
	}`
			}
			src := fmt.Sprintf(`package probe

import ("encoding/binary"; "os"; "testing")

func TestOldRefusesNew(t *testing.T) {
	data, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	want := binary.LittleEndian.Uint64(data[8:16])
	_ = want
	var fresh %[2]s
	%[2]sReset(&fresh)
	back := make([]%[2]s, 8)
	for k := range back {
		%[2]sReset(&back[k])
	}
	var r TableReport
	plan := make([]TableFixedEntry, 4096)
	n := %[2]sFixedLoad(back, data, plan, &r)
%[3]s
}
`, file, table, body)
			out, err := runVersionProbe(t, older, nil, src)
			if err != nil {
				t.Fatalf("OLD-REFUSES-NEW %s: %v\n%s", r.row, err, out)
			}
		})
	}
}

// ---- the floor --------------------------------------------------------------
//
// The floor is ONE NUMBER and the lineage is ONE ARRAY, so "retired" is an
// index cut and the operator's two answers stay distinct: below the floor is
// `layout_unsupported` (upgrade the client), outside the lineage is
// `layout_newer` (ship the reader). §5.2, §5.7's three floor rows.

func TestFixedVersioningFloor(t *testing.T) {
	corpus := fixedCorpus(t)
	older := readSchema(t, "VOLD_floor")
	mid := readSchema(t, "VMID_floor")
	newer := readSchema(t, "VNEW_floor")
	table := fixedRootName(t, older)

	probe := func(t *testing.T, retire int, body string) {
		t.Helper()
		src := fmt.Sprintf(`package probe

import ("os"; "testing")

func TestFloor(t *testing.T) {
	oldFile, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	midFile, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = oldFile, midFile
	back := make([]%[3]s, 8)
	plan := make([]TableFixedEntry, 4096)
	var r TableReport
	_, _, _ = back, plan, r
%[4]s
}
`, filepath.Join(corpus, "old_floor.bin"), filepath.Join(corpus, "mid_floor.bin"), table, body)
		out, err := runVersionProbeRetired(t, newer, []string{older, mid}, retire, src)
		if err != nil {
			t.Fatalf("floor: %v\n%s", err, out)
		}
	}

	t.Run("floor_at", func(t *testing.T) {
		t.Parallel()
		// Nothing retired: the OLDEST file is at the floor and it reads.
		probe(t, 0, `	n := `+table+`FixedLoad(back, oldFile, plan, &r)
	if n < 1 || r.Reason != "" || r.Malformed {
		t.Fatalf("a file AT the floor must read: n=%d %+v", n, r)
	}`)
	})
	t.Run("floor_below", func(t *testing.T) {
		t.Parallel()
		// Entry 0 retired: the floor is 1, and the file one below it refuses
		// layout_unsupported — nothing decoded, no counter moved.
		probe(t, 1, `	n := `+table+`FixedLoad(back, oldFile, plan, &r)
	if n != -1 || r.Reason != "layout_unsupported" {
		t.Fatalf("a file below the floor owes layout_unsupported: n=%d %+v", n, r)
	}
	if r.Malformed || r.Widened != 0 || r.Unknown != 0 || r.Clamped != 0 {
		t.Fatalf("REFUSE is total: %+v", r)
	}
	m := `+table+`FixedLoad(back, midFile, plan, &r)
	if m < 1 {
		t.Fatalf("the file AT the raised floor must still read: %d %+v", m, r)
	}`)
	})
	t.Run("floor_raise_live", func(t *testing.T) {
		t.Parallel()
		// The floor raised by one: the file that read yesterday refuses today.
		probe(t, 2, `	n := `+table+`FixedLoad(back, midFile, plan, &r)
	if n != -1 || r.Reason != "layout_unsupported" {
		t.Fatalf("the floor raised by one: yesterday's file must refuse: n=%d %+v", n, r)
	}`)
	})
}

// ---- the hash ---------------------------------------------------------------

func TestFixedVersioningHash(t *testing.T) {
	corpus := fixedCorpus(t)
	newer := readSchema(t, "VNEW_field_append")
	older := readSchema(t, "VOLD_field_append")
	table := fixedRootName(t, older)
	old := filepath.Join(corpus, "old_field_append.bin")
	fresh := filepath.Join(corpus, "new_field_append.bin")

	src := fmt.Sprintf(`package probe

import ("encoding/binary"; "os"; "testing")

// hash_unknown: a hash in no lineage refuses layout_newer.
func TestHashUnknown(t *testing.T) {
	data, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint64(data[8:16], 0xDEADBEEFCAFEF00D)
	back := make([]%[3]s, 8)
	plan := make([]TableFixedEntry, 4096)
	var r TableReport
	n := %[3]sFixedLoad(back, data, plan, &r)
	if n != -1 || r.Reason != "layout_newer" || r.LayoutHash != 0xDEADBEEFCAFEF00D || r.Malformed {
		t.Fatalf("hash_unknown: n=%%d %%+v", n, r)
	}
}

// hash_known_bytes_differ: a KNOWN hash whose layout bytes differ from the
// lock's is ONE name, layout_malformed — "a lie about a known version". The
// seven §1.1 malformations under a known hash all land here, never in a
// runtime walk (§5.3).
func TestHashKnownBytesDiffer(t *testing.T) {
	data, err := os.ReadFile(%[1]q)
	if err != nil {
		t.Fatal(err)
	}
	data[TableFixedHeaderBytes+4] ^= 0xFF
	back := make([]%[3]s, 8)
	plan := make([]TableFixedEntry, 4096)
	var r TableReport
	n := %[3]sFixedLoad(back, data, plan, &r)
	if n != -1 || r.Reason != "layout_malformed" || r.Malformed {
		t.Fatalf("hash_known_bytes_differ: n=%%d %%+v", n, r)
	}
}

// hash_identity: the reader's own hash selects the identity plan.
func TestHashIdentity(t *testing.T) {
	data, err := os.ReadFile(%[2]q)
	if err != nil {
		t.Fatal(err)
	}
	back := make([]%[3]s, 8)
	plan := make([]TableFixedEntry, 4096)
	var r TableReport
	n := %[3]sFixedLoad(back, data, plan, &r)
	if n < 1 || r.Reason != "" || r.Malformed {
		t.Fatalf("hash_identity: n=%%d %%+v", n, r)
	}
	if back[0].W != 777 {
		t.Fatalf("the identity plan lost a value: %%+v", back[0])
	}
}
`, old, fresh, table)
	out, err := runVersionProbe(t, newer, []string{older}, src)
	if err != nil {
		t.Fatalf("hash cases: %v\n%s", err, out)
	}
}

// ---- lineage_merge ----------------------------------------------------------
//
// Two branches append different fields; after the merge BOTH pre-merge files
// read on the merged build (the name-subset rule, bill §8a.1).

func TestFixedVersioningLineageMerge(t *testing.T) {
	corpus := fixedCorpus(t)
	merged := readSchema(t, "VNEW_lineage_merge")
	a := readSchema(t, "VBRA_lineage_merge")
	b := readSchema(t, "VBRB_lineage_merge")
	base := readSchema(t, "VOLD_lineage_merge")
	table := fixedRootName(t, base)
	src := fmt.Sprintf(`package probe

import ("os"; "testing")

func TestLineageMerge(t *testing.T) {
	for _, name := range []string{%q, %q} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		back := make([]%[3]s, 8)
		plan := make([]TableFixedEntry, 4096)
		var r TableReport
		n := %[3]sFixedLoad(back, data, plan, &r)
		if n < 1 || r.Reason != "" || r.Malformed {
			t.Fatalf("%%s: both pre-merge files read on the merged build: n=%%d %%+v", name, n, r)
		}
	}
}
`, filepath.Join(corpus, "a_lineage_merge.bin"), filepath.Join(corpus, "b_lineage_merge.bin"), table)
	out, err := runVersionProbe(t, merged, []string{base, a, b}, src)
	if err != nil {
		t.Fatalf("lineage_merge: %v\n%s", err, out)
	}
}

// ---- the harness ------------------------------------------------------------

func fixedCorpus(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("../../../build/fixedform-corpus")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "old_field_append.bin")); err != nil {
		// THE SKIP IS A FAILURE WHEN SOMETHING PROMISED THE CORPUS. A bare
		// `go test ./...` on a tree that never built the oracle has nothing to
		// read and says so; but `make tables-go-versioning` builds the corpus
		// first and sets SCHEMA_REQUIRE_CORPUS=1, so under that target a
		// missing file means the build did not do what the target says it did
		// — and a suite that skips itself there would report green over §5
		// having never run, which is the whole reason this gate exists.
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and the reference's byte oracle is not in %s: %v", dir, err)
		}
		t.Skip("the reference's byte oracle is not built: make tables-fixedform-corpus")
	}
	return dir
}

func readSchema(t *testing.T, base string) string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "test", "tables", base+".schema")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func unitOf(t *testing.T, src string) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

func fixedRootName(t *testing.T, src string) string {
	t.Helper()
	u := unitOf(t, src)
	roots := ir.TableFixedRoots(u)
	if len(roots) == 0 {
		t.Fatal("a versioning row declares a fixed root")
	}
	// A row whose change is NESTED declares two fixed tables — the nested type
	// and the root that reaches it. The file's root is the one no other fixed
	// table names by value.
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
	return roots[len(roots)-1].Name
}

func runVersionProbe(t *testing.T, reader string, older []string, testSource string) ([]byte, error) {
	t.Helper()
	return runVersionProbeRetired(t, reader, older, 0, testSource)
}

// runVersionProbeRetired generates the READER's unit with the OLDER units'
// locked entries handed to it as its lineage — oldest first, the reader's own
// layout last — and marks the first `retire` entries retired, which is what
// moves the floor.
func runVersionProbeRetired(t *testing.T, reader string, older []string, retire int, testSource string) ([]byte, error) {
	t.Helper()
	u := unitOf(t, reader)
	lineage := map[string][]FixedLineageEntry{}
	for i, src := range older {
		o := unitOf(t, src)
		for _, st := range ir.TableFixedRoots(o) {
			e, ok := FixedLineageOf(o, st.Name)
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
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	runtime, err := filepath.Abs("../../../../serialize.go")
	if err != nil {
		t.Fatal(err)
	}
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime))
	// The probe lives IN the generated package, whose name is the schema's own.
	files["version_test.go"] = []byte(strings.Replace(testSource, "package probe", "package "+u.Package, 1))
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

var _ = binary.LittleEndian
