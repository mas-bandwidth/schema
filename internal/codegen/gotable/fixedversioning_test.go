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

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
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
	{row: "fixed_I_grow_element", widens: true, check: `
	if n != 1 {
		t.Fatalf("the fixed_I_grow_element file carries one record, not %d", n)
	}
	want := []int32{-1, -128, 0, 112}
	for k := range want {
		if int32(back[0].Vals[k]) != want[k] {
			t.Fatalf("slot %d did not land its raw scaled value: %+v", k, back[0])
		}
	}
	if back[0].Lead != 0xAAAAAAAA || back[0].Trail != 0xBBBBBBBB {
		t.Fatalf("the element row moved a neighbour: %+v", back[0])
	}`},
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

// ---- writer_bound_count (§5.8 row 4) ----------------------------------------
//
// The OLD writer's file declares [..4]int32 and the NEW reader is [..8]int32; it
// reads through the lineage, so the plan is a COMPILED one and the count's bound
// is the WRITER's carried by the plan — never the reader's own 8 and never the
// forged 7. `Clamped` is asserted == 1 and never >= 1: the bounds pass counts
// once per entry per record, so a leg that also counts in the `count` op lands 2
// and is wrong.

func TestFixedVersioningWriterBoundCount(t *testing.T) {
	corpus := fixedCorpus(t)
	newer := readSchema(t, "VNEW_array_bounded_grow")
	older := readSchema(t, "VOLD_array_bounded_grow")
	table := fixedRootName(t, older)
	src := fmt.Sprintf(`package probe

import ("bytes"; "encoding/binary"; "os"; "testing")

func TestWriterBoundCount(t *testing.T) {
	data, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	needle := []byte{0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00}
	at := bytes.Index(data, needle)
	if at < 0 {
		t.Fatal("writer_bound_count: the lead 0xAAAAAAAA followed by vals_count 4 is not in the file")
	}
	if bytes.Index(data[at+1:], needle) >= 0 {
		t.Fatal("writer_bound_count: the forged-count needle occurs more than once, so the search is not a locator")
	}
	binary.LittleEndian.PutUint32(data[at+4:], 7)
	back := make([]%[2]s, 8)
	var r TableReport
	plan := make([]TableFixedEntry, 4096)
	n := %[2]sFixedLoad(back, data, plan, &r)
	if n != 1 {
		t.Fatalf("writer_bound_count: the forged old file reads one record, n=%%d %%+v", n, r)
	}
	if back[0].ValsCount != 4 {
		t.Fatalf("writer_bound_count: the count landed %%d, not the WRITER's bound 4 (never the reader's 8, never the forged 7): %%+v", back[0].ValsCount, back[0])
	}
	for k, want := range []int32{1000, 1001, 1002, 1003} {
		if back[0].Vals[k] != want {
			t.Fatalf("writer_bound_count: Vals[%%d] landed %%d, want %%d: %%+v", k, back[0].Vals[k], want, back[0])
		}
	}
	for k := 4; k < 8; k++ {
		if back[0].Vals[k] != 0 {
			t.Fatalf("writer_bound_count: the reader's slot %%d is not its declared default 0: %%+v", k, back[0])
		}
	}
	if back[0].Lead != 0xAAAAAAAA || back[0].Trail != 0xBBBBBBBB {
		t.Fatalf("writer_bound_count: a mislaid size moved a neighbour: %%+v", back[0])
	}
	if r.Clamped != 1 {
		t.Fatalf("writer_bound_count: the bounds pass counts once per entry per record, Clamped=%%d, want 1: %%+v", r.Clamped, r)
	}
	if r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Duplicate != 0 {
		t.Fatalf("writer_bound_count: a counter other than Clamped moved: %%+v", r)
	}
	if r.Malformed || r.Verdict == TableOpenRefused || r.Reason != "" {
		t.Fatalf("writer_bound_count: the forged count reads clean, not a refusal: %%+v", r)
	}
}
`, filepath.Join(corpus, "old_array_bounded_grow.bin"), table)
	out, err := runVersionProbe(t, newer, []string{older}, src)
	if err != nil {
		t.Fatalf("writer_bound_count: %v\n%s", err, out)
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

// ---- the form header, before the file's length ------------------------------
//
// §5.3 STEP 1 READS THE FORM BYTE BEFORE THE FILE'S LENGTH, and each byte earns
// its own name (docs/SPEC-TABLES.md §3.4): a committed form-1 file is TEN bytes
// and a form-2 batch THREE, so a reader that measured first would answer
// `malformed` for every real file of the two forms it is supposed to name. The
// seven reserved bytes of §5.3 step 2 are REFUSED and not ignored. Form byte 0
// is assigned by no form and is named by where the registry sits.
//
// THE FIVE CASES ARE THE MODEL'S (test/elixir-fixedform/main.exs): the empty
// file, the two real short lengths, form 0 with a fixture's own tail, and a
// valid form-3 fixture with reserved byte 3 set nonzero. THE SHORT CASES ARE
// THE EXACT TEN- AND THREE-BYTE INPUTS, never a full-length buffer.
//
// EACH PROBE ASSERTS THE JOINT REPORT (§5.3's two answers — a named refusal sets
// `refused` and its name and never `malformed`; a malformed read sets `malformed`
// and names nothing) AND THAT REFUSE WROTE NOT ONE DESTINATION BYTE: the record
// is pre-poisoned and compared unchanged.
func TestFixedVersioningFormHeader(t *testing.T) {
	corpus := fixedCorpus(t)
	newer := readSchema(t, "VNEW_field_append")
	older := readSchema(t, "VOLD_field_append")
	table := fixedRootName(t, older)
	fixture := filepath.Join(corpus, "new_field_append.bin")
	src := fmt.Sprintf(`package probe

import ("os"; "testing"; "unsafe")

func poison(v *%[1]s) {
	b := unsafe.Slice((*byte)(unsafe.Pointer(v)), unsafe.Sizeof(*v))
	for i := range b {
		b[i] = 0xAB
	}
}

func unchanged(v *%[1]s) bool {
	b := unsafe.Slice((*byte)(unsafe.Pointer(v)), unsafe.Sizeof(*v))
	for i := range b {
		if b[i] != 0xAB {
			return false
		}
	}
	return true
}

func TestFormHeader(t *testing.T) {
	valid, err := os.ReadFile(%[2]q)
	if err != nil {
		t.Fatal(err)
	}
	if len(valid) < 20 || valid[0] != 3 {
		t.Fatalf("the fixture is not a form-3 file: len=%%d byte0=%%d", len(valid), valid[0])
	}
	form0 := append([]byte(nil), valid...)
	form0[0] = 0
	reserved3 := append([]byte(nil), valid...)
	reserved3[3] = 1
	cases := []struct {
		name string
		in   []byte
		want TableReport
	}{
		{"empty-file", []byte{}, TableReport{Malformed: true}},
		{"short-form1", []byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0}, TableReport{Verdict: TableOpenRefused, Reason: "previous_form"}},
		{"short-form2", []byte{2, 1, 0}, TableReport{Verdict: TableOpenRefused, Reason: "message_form_as_file"}},
		{"unassigned-form0", form0, TableReport{Verdict: TableOpenRefused, Reason: "newer_form"}},
		{"reserved-byte3", reserved3, TableReport{Malformed: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			back := make([]%[1]s, 1)
			poison(&back[0])
			var r TableReport
			plan := make([]TableFixedEntry, 4096)
			n := %[1]sFixedLoad(back, c.in, plan, &r)
			if n != -1 {
				t.Fatalf("%%s: n=%%d, want -1 (%%+v)", c.name, n, r)
			}
			if r != c.want {
				t.Fatalf("%%s: report %%+v, want %%+v", c.name, r, c.want)
			}
			if !unchanged(&back[0]) {
				t.Fatalf("%%s: REFUSE wrote destination bytes", c.name)
			}
		})
	}
}
`, table, fixture)
	out, err := runVersionProbe(t, newer, []string{older}, src)
	if err != nil {
		t.Fatalf("form header: %v\n%s", err, out)
	}
}

// ---- forged_ordinal_both_plans ----------------------------------------------
//
// §5.8 row 12: the SAME forged bytes read by BOTH plans. r0.tier is set to 4,
// past the WRITER's three variants and a name only the NEW reader has; both
// plans carry the WRITER's count, so 4 lands None on both and never Platinum.
// Clamped is == 1 and never >= 1: the BOUNDS pass's count, once per field
// (§5.4), and the two plans' equal 1 is the row's whole proof.
func TestFixedVersioningForgedOrdinalBothPlans(t *testing.T) {
	corpus := fixedCorpus(t)
	newer := readSchema(t, "VNEW_enum_append")
	older := readSchema(t, "VOLD_enum_append")
	table := fixedRootName(t, older)
	src := fmt.Sprintf(`package probe

import ("bytes"; "os"; "testing")

func TestForgedOrdinal(t *testing.T) {
	data, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	needle := []byte{0x03, 0x09, 0x00, 0x00, 0x00}
	at := bytes.Index(data, needle)
	if at < 0 {
		t.Fatalf("the record body (tier=Gold, seq=9) is not in the file: tail=%% x", data[len(data)-5:])
	}
	if bytes.Index(data[at+1:], needle) >= 0 {
		t.Fatalf("the record body occurs more than once in the file")
	}
	if at != len(data)-5 {
		t.Fatalf("the record body is not the tail of the file: at=%%d len=%%d", at, len(data))
	}
	data[at] = 4 // the forge

	back := make([]%[2]s, 8)
	var r TableReport
	plan := make([]TableFixedEntry, 4096)
	n := %[2]sFixedLoad(back, data, plan, &r)
	if n != 1 {
		t.Fatalf("the forged file reads one record, not %%d (report %%+v)", n, r)
	}
	if r.Verdict == TableOpenRefused || r.Reason != "" || r.Malformed {
		t.Fatalf("the forged read is not a refusal: %%+v", r)
	}
	if back[0].Tier != TierNone {
		t.Fatalf("the forged ordinal lands None, not %%v (record %%+v)", back[0].Tier, back[0])
	}
	if back[0].Seq != 9 {
		t.Fatalf("the scalar after the enum must stand at 9, not %%v (record %%+v)", back[0].Seq, back[0])
	}
	if r.Clamped != 1 {
		t.Fatalf("Clamped is the BOUNDS pass's count, once per field: %%d (report %%+v)", r.Clamped, r)
	}
	if r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Duplicate != 0 {
		t.Fatalf("only Clamped may move on a forged ordinal: %%+v", r)
	}
}
`, filepath.Join(corpus, "old_enum_append.bin"), table)
	out, err := runVersionProbe(t, newer, []string{older}, src)
	if err != nil {
		t.Fatalf("forged_ordinal_both_plans, the compiled plan: %v\n%s", err, out)
	}
	out, err = runVersionProbe(t, older, nil, src)
	if err != nil {
		t.Fatalf("forged_ordinal_both_plans, the identity plan: %v\n%s", err, out)
	}
}

// §5.8 row 9, refuse_writes_nothing — the row that asks the refusal itself to have
// written nothing. The header's hash is left alone so the reader selects the OLD
// lineage entry and compiles its plan (a nonempty fill list for the appended nested
// field) before step 11 compares the record's own hash; that record hash is inverted
// so the comparison refuses `no_layout`. The destination is poisoned with 0x5A, not
// zero, so a prefill that ran (Vec.w = 88, a nonzero default) is told apart from one
// that did not: zero would hide the prefill, 0x5A exposes every byte the load touched.
func TestFixedVersioningRefuseWritesNothing(t *testing.T) {
	corpus := fixedCorpus(t)
	newer := readSchema(t, "VNEW_nested_append")
	older := readSchema(t, "VOLD_nested_append")
	table := fixedRootName(t, older)
	src := fmt.Sprintf(`package probe

import ("encoding/binary"; "os"; "testing"; "unsafe")

func TestRefuseWritesNothing(t *testing.T) {
	data, err := os.ReadFile(%[2]q)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte{1, 0, 0, 0, 2, 0, 0, 0, 3, 0, 0, 0, 24, 0, 0, 0}
	at := -1
	for i := 0; i+len(body) <= len(data); i++ {
		eq := true
		for j := range body {
			if data[i+j] != body[j] {
				eq = false
				break
			}
		}
		if eq {
			if at != -1 {
				t.Fatalf("the record body occurs more than once in the file")
			}
			at = i
		}
	}
	if at == -1 {
		t.Fatalf("the record body does not occur in the file")
	}
	if at != len(data)-len(body) {
		t.Fatalf("the record body is not the tail of the file: at=%%d want %%d", at, len(data)-len(body))
	}
	headerHash := binary.LittleEndian.Uint64(data[8:16])
	// the forge
	for i := at - 8; i < at; i++ {
		data[i] = ^data[i]
	}
	if binary.LittleEndian.Uint64(data[8:16]) != headerHash {
		t.Fatalf("the forge touched the header's hash: 0x%%016x -> 0x%%016x", headerHash, binary.LittleEndian.Uint64(data[8:16]))
	}
	back := make([]%[1]s, 8)
	poisoned := unsafe.Slice((*byte)(unsafe.Pointer(&back[0])), int(unsafe.Sizeof(back[0]))*len(back))
	for i := range poisoned {
		poisoned[i] = 0x5A
	}
	before := append([]byte(nil), poisoned...)
	var r TableReport
	plan := make([]TableFixedEntry, 4096)
	n := %[1]sFixedLoad(back, data, plan, &r)
	if n != -1 {
		t.Fatalf("a record whose hash names no layout must refuse: n=%%d %%+v", n, r)
	}
	if r.Verdict != TableOpenRefused || r.Reason != "no_layout" {
		t.Fatalf("the record hash refusal owes no_layout by name: %%+v", r)
	}
	if r.Malformed {
		t.Fatal("a refusal by name never sets malformed too (§5.3, the joint answer)")
	}
	if r.LayoutHash != 0 {
		t.Fatalf("no_layout must not publish a hash: 0x%%016x", r.LayoutHash)
	}
	if r.Widened != 0 || r.Unknown != 0 || r.KindMismatch != 0 || r.Clamped != 0 || r.Duplicate != 0 {
		t.Fatalf("REFUSE is total: no counter moves: %%+v", r)
	}
	for i := range poisoned {
		if poisoned[i] != before[i] {
			t.Fatalf("REFUSE wrote destination byte %%d: 0x%%02x, want 0x5A", i, poisoned[i])
		}
	}
}
`, table, filepath.Join(corpus, "old_nested_append.bin"))
	out, err := runVersionProbe(t, newer, []string{older}, src)
	if err != nil {
		t.Fatalf("refuse_writes_nothing: %v\n%s", err, out)
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
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

var _ = binary.LittleEndian
