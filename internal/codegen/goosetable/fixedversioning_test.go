package goosetable

// THE VERSIONING GATE ON THE GOOSE LEG (docs/FIXED-FORM-ALGORITHM.md §5.7, the
// rows of docs/FIXED-FORM-VERSIONING-TESTS.md; make tables-goose-versioning).
// Every row of that page is ONE definition change and owes TWO read columns:
//
//	NEW-READS-OLD    the widened reader reads the older writer's file: every
//	                 old value lands exactly, the reader's tail is its
//	                 declared default, and the counters are §5.4's.
//	OLD-REFUSES-NEW  the older reader given the widened writer's file refuses
//	                 `layout_unknown` (§5.3, the rule Go takes) BEFORE any
//	                 record, reporting THE FILE'S hash, no counter moved,
//	                 `malformed` false, and the destination still every field
//	                 of a fresh value.
//
// THE VALUES ARE THE MANIFEST'S (§5.9 #32, #37): build/fixedform-corpus/
// manifest.txt, the line the C++ dump wrote for that file, and never a number
// restated here. A NEW-READS-OLD probe builds the value the reader OWES — a
// fresh NEW value (the reader's declared defaults) with every manifest value of
// the OLD file assigned onto the field of the same WIRE ID (§5.9 #41, so a
// `was =` rename compares) — and the gate compares the two RECORD IMAGES, the
// plan's own byte view (§5.9 #35: Goose derives no struct equality, so the
// image is the form this leg lays). The tail's one exception is the `present`
// companion a `T` -> `?T` row owes as `1` (§5.9 #40).
//
// THE PROBE UNITS LIVE BESIDE THE GENERATOR (§5.9 #18): two generations of one
// table name have no spelling inside one Goose namespace — every module of a
// unit declares into its package, and both generations' runtimes are
// schema_rt — so the column is the unit, one generated program per row per
// column, run under the pinned compiler.
//
// THE LINEAGE IS DATA THE BUILD HANDS THE BACKEND (§5.2): the test plays the
// lock, handing the newer unit each older unit's entry (FixedEvolutionOf: the
// wire hash, the layout bytes verbatim, the record size) through
// GenerateLineage. The reader never walks the layout a file carries.

import (
	"bufio"
	"fmt"
	"maps"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- the rows ---------------------------------------------------------------

type gooseVersionRow struct {
	row string
	// sameHash: the edit moves no layout byte and no digest byte, so both
	// sides take the identity plan and the file READS both ways (§5.7).
	sameHash bool
	// widens: the row grows a WIDTH, so `widened` moves; every other row is
	// an APPEND and owes every counter at zero (§5.9 #10).
	widens bool
	// poison: the destination is laid 0x5A through the record image before
	// the load, because with a zero default the row passes whether the
	// prefill ran or not (§5.7, §5.9 #17).
	poison bool
}

var gooseVersionRows = []gooseVersionRow{
	{row: "array_bounded_grow"},
	{row: "array_elem_widen", widens: true},
	{row: "array_fixed_grow", poison: true},
	{row: "bits_grow", widens: true},
	{row: "bytes_grow"},
	{row: "cfloat_range_widen"},
	{row: "cfloat_res_refine"},
	{row: "constant_grow"},
	{row: "enum_append"},
	{row: "enum_width", widens: true},
	{row: "field_append"},
	{row: "field_deprecate", sameHash: true},
	{row: "field_undeprecate", sameHash: true},
	{row: "fixed_I_grow", widens: true},
	{row: "fixed_I_grow_element", widens: true},
	{row: "flags_append"},
	{row: "float_widen", widens: true},
	{row: "int_widen", widens: true},
	{row: "keyed_array_enum_append", poison: true},
	{row: "nested_append"},
	{row: "optional_add"},
	{row: "range_widen"},
	{row: "rename_without_was", sameHash: true},
	{row: "string_grow"},
	{row: "uint_widen", widens: true},
	{row: "union_append"},
	// NOT A WIDEN, whatever its name: it appends a field inside an arm (§5.7)
	{row: "union_arm_payload_widen"},
	{row: "wstring_grow"},
}

// ---- NEW-READS-OLD ----------------------------------------------------------

func TestGooseFixedVersioningNewReadsOld(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	manifest := gooseVersionManifest(t, corpus)
	for _, r := range gooseVersionRows {
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			file := "old_" + r.row + ".bin"
			line := manifest.line(t, file)
			older := evolutionLoad(t, "test/tables/VOLD_"+r.row+".schema")
			newer := evolutionLoad(t, "test/tables/VNEW_"+r.row+".schema")
			got := gooseVersionProbe(t, gooseVersionCase{
				reader: newer, older: []*ir.Unit{older}, root: line.root, capacity: line.records,
				poison: r.poison, data: gooseCorpusFile(t, corpus, file), want: line,
			})
			got.reads(t, line.records)
			if r.widens {
				if got.widened == 0 {
					t.Fatalf("%s is a widening row and moved no widened counter: %s", r.row, got.raw)
				}
			} else if got.widened != 0 {
				t.Fatalf("%s: widened %d, and an append is not an event: %s", r.row, got.widened, got.raw)
			}
			if got.clamped != 0 || got.unknown != 0 || got.kindMismatch != 0 {
				t.Fatalf("%s: counters moved on a clean backward read: %s", r.row, got.raw)
			}
			got.owes(t)
		})
	}
}

// ---- OLD-REFUSES-NEW --------------------------------------------------------

func TestGooseFixedVersioningOldRefusesNew(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	manifest := gooseVersionManifest(t, corpus)
	for _, r := range gooseVersionRows {
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			file := "new_" + r.row + ".bin"
			line := manifest.line(t, file)
			older := evolutionLoad(t, "test/tables/VOLD_"+r.row+".schema")
			data := gooseCorpusFile(t, corpus, file)
			if r.sameHash {
				// NO second column: the hashes are equal, so the file reads
				// both ways — the test that the hash, and only the hash, is the
				// version (§5.7). The OLD reader owes the NEW file's values.
				got := gooseVersionProbe(t, gooseVersionCase{
					reader: older, root: line.root, capacity: line.records, data: data, want: line,
				})
				got.reads(t, line.records)
				got.owes(t)
				return
			}
			got := gooseVersionProbe(t, gooseVersionCase{
				reader: older, root: line.root, capacity: line.records, poison: false, data: data, fresh: true,
			})
			got.refuses(t, "layout_unknown", true, gooseFileHash(data))
		})
	}
}

// ---- the floor (§5.7's three) -----------------------------------------------

func TestGooseFixedVersioningFloor(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	manifest := gooseVersionManifest(t, corpus)
	older := evolutionLoad(t, "test/tables/VOLD_floor.schema")
	mid := evolutionLoad(t, "test/tables/VMID_floor.schema")
	newer := evolutionLoad(t, "test/tables/VNEW_floor.schema")
	oldData := gooseCorpusFile(t, corpus, "old_floor.bin")
	midData := gooseCorpusFile(t, corpus, "mid_floor.bin")

	t.Run("floor_at", func(t *testing.T) {
		t.Parallel()
		line := manifest.line(t, "old_floor.bin")
		got := gooseVersionProbe(t, gooseVersionCase{reader: newer, older: []*ir.Unit{older, mid}, root: line.root,
			capacity: line.records, data: oldData, want: line})
		got.reads(t, line.records)
		got.owes(t)
	})
	t.Run("floor_below", func(t *testing.T) {
		t.Parallel()
		got := gooseVersionProbe(t, gooseVersionCase{reader: newer, older: []*ir.Unit{older, mid}, retire: 1, root: "Floored",
			capacity: 1, data: oldData, fresh: true})
		got.refuses(t, "layout_unsupported", true, gooseFileHash(oldData))
		line := manifest.line(t, "mid_floor.bin")
		at := gooseVersionProbe(t, gooseVersionCase{reader: newer, older: []*ir.Unit{older, mid}, retire: 1, root: line.root,
			capacity: line.records, data: midData, want: line})
		at.reads(t, line.records)
		at.owes(t)
	})
	t.Run("floor_raise_live", func(t *testing.T) {
		t.Parallel()
		got := gooseVersionProbe(t, gooseVersionCase{reader: newer, older: []*ir.Unit{older, mid}, retire: 2, root: "Floored",
			capacity: 1, data: midData, fresh: true})
		got.refuses(t, "layout_unsupported", true, gooseFileHash(midData))
	})
}

// ---- the hash ---------------------------------------------------------------

func TestGooseFixedVersioningHash(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	manifest := gooseVersionManifest(t, corpus)
	older := evolutionLoad(t, "test/tables/VOLD_field_append.schema")
	newer := evolutionLoad(t, "test/tables/VNEW_field_append.schema")
	old := gooseCorpusFile(t, corpus, "old_field_append.bin")

	t.Run("hash_unknown", func(t *testing.T) {
		t.Parallel()
		data := append([]byte(nil), old...)
		forged := uint64(0x0EADBEEFCAFEF00D)
		for i := range 8 {
			data[8+i] = byte(forged >> (8 * i))
		}
		got := gooseVersionProbe(t, gooseVersionCase{reader: newer, older: []*ir.Unit{older}, root: "Lineage", capacity: 1, data: data, fresh: true})
		got.refuses(t, "layout_unknown", true, forged)
	})
	t.Run("hash_known_bytes_differ", func(t *testing.T) {
		t.Parallel()
		data := append([]byte(nil), old...)
		data[20+4] ^= 0xFF
		got := gooseVersionProbe(t, gooseVersionCase{reader: newer, older: []*ir.Unit{older}, root: "Lineage", capacity: 1, data: data, fresh: true})
		got.refuses(t, "layout_malformed", false, 0)
	})
	t.Run("hash_identity", func(t *testing.T) {
		t.Parallel()
		line := manifest.line(t, "new_field_append.bin")
		got := gooseVersionProbe(t, gooseVersionCase{reader: newer, older: []*ir.Unit{older}, root: line.root,
			capacity: line.records, data: gooseCorpusFile(t, corpus, "new_field_append.bin"), want: line})
		got.reads(t, line.records)
		got.owes(t)
	})
}

// ---- lineage_merge ----------------------------------------------------------

// Two branches append different fields; after the merge both pre-merge files,
// and the oldest, read on the merged build (bill §8a.1).
func TestGooseFixedVersioningLineageMerge(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	manifest := gooseVersionManifest(t, corpus)
	older := []*ir.Unit{
		evolutionLoad(t, "test/tables/VOLD_lineage_merge.schema"),
		evolutionLoad(t, "test/tables/VBRA_lineage_merge.schema"),
		evolutionLoad(t, "test/tables/VBRB_lineage_merge.schema"),
	}
	newer := evolutionLoad(t, "test/tables/VNEW_lineage_merge.schema")
	for _, file := range []string{"old_lineage_merge.bin", "a_lineage_merge.bin", "b_lineage_merge.bin", "new_lineage_merge.bin"} {
		t.Run(strings.TrimSuffix(file, ".bin"), func(t *testing.T) {
			t.Parallel()
			line := manifest.line(t, file)
			got := gooseVersionProbe(t, gooseVersionCase{reader: newer, older: older, root: line.root,
				capacity: line.records, data: gooseCorpusFile(t, corpus, file), want: line})
			got.reads(t, line.records)
			got.owes(t)
		})
	}
}

// ---- §5.8's probes ----------------------------------------------------------

// refuse_writes_nothing (§5.8 row 9, step 8): record 0's per-record hash is
// forged under an untouched header hash, so the OLD entry is selected and its
// compiled plan has a nonempty fill (Vec.w = 88). The hash check runs BEFORE
// the prefill, so the refusal is total: no_layout, malformed false, every
// counter 0, and the poisoned destination still the poison.
func TestGooseFixedVersioningRefuseWritesNothing(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	older := evolutionLoad(t, "test/tables/VOLD_nested_append.schema")
	newer := evolutionLoad(t, "test/tables/VNEW_nested_append.schema")
	data := gooseCorpusFile(t, corpus, "old_nested_append.bin")
	lb := int(uint32(data[16]) | uint32(data[17])<<8 | uint32(data[18])<<16 | uint32(data[19])<<24)
	off := 20 + lb
	for i := range 8 {
		data[off+i] = ^data[off+i]
	}
	got := gooseVersionProbe(t, gooseVersionCase{reader: newer, older: []*ir.Unit{older}, root: "Lineage", capacity: 1,
		poison: true, data: data, fresh: true})
	got.refuses(t, "no_layout", false, 0)
}

// unknown_census (§5.8 row 11, step 9): the deliberately UNLAWFUL pair, its
// entry handed in, over an array of FOUR tables with one dropped field:
// unknown == 1 exactly, once per FIELD per peer — and the three-record file
// tells "once per peer" from "once per record", on a second read as well.
func TestGooseFixedVersioningUnknownCensus(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	manifest := gooseVersionManifest(t, corpus)
	older := evolutionLoad(t, "test/tables/VOLD_unknown_census.schema")
	newer := evolutionLoad(t, "test/tables/VNEW_unknown_census.schema")
	for _, file := range []string{"old_unknown_census.bin", "many_unknown_census.bin"} {
		t.Run(strings.TrimSuffix(file, ".bin"), func(t *testing.T) {
			t.Parallel()
			data := gooseCorpusFile(t, corpus, file)
			records := gooseFixedRecords(t, data, older, "Census")
			c := gooseVersionCase{reader: newer, older: []*ir.Unit{older}, root: "Census", capacity: records, data: data, twice: true}
			if line, ok := manifest.lines[file]; ok {
				c.want = line
			}
			got := gooseVersionProbe(t, c)
			got.reads(t, records)
			if got.unknown != 1 || got.secondUnknown != 1 {
				t.Fatalf("%s: unknown is %d (second read %d); §5.4 owes exactly 1, once per field per peer: %s",
					file, got.unknown, got.secondUnknown, got.raw)
			}
			if got.clamped != 0 || got.widened != 0 || got.kindMismatch != 0 {
				t.Fatalf("%s: a counter beside the census moved: %s", file, got.raw)
			}
			if c.want.file != "" {
				got.owes(t)
			}
		})
	}
}

// forged_ordinal_both_plans (§5.8 row 12, step 10): the enum ordinal forged
// past the OLD writer's variants, read by the NEW build's COMPILED plan and by
// the OLD build's IDENTITY plan: None and clamped == 1 on both, the same number.
func TestGooseFixedVersioningForgedOrdinalBothPlans(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	older := evolutionLoad(t, "test/tables/VOLD_enum_append.schema")
	newer := evolutionLoad(t, "test/tables/VNEW_enum_append.schema")
	data := gooseCorpusFile(t, corpus, "old_enum_append.bin")
	// the record body is tier (u8) then seq (i32): forge the tier past the
	// writer's three variants
	body := len(data) - 5
	if data[body] != 3 {
		t.Fatalf("forged_ordinal: the record's tier is %d, not the manifest's Gold (3)", data[body])
	}
	data[body] = 4
	for _, plan := range []struct {
		name   string
		reader *ir.Unit
		older  []*ir.Unit
	}{{"compiled", newer, []*ir.Unit{older}}, {"identity", older, nil}} {
		t.Run(plan.name, func(t *testing.T) {
			t.Parallel()
			got := gooseVersionProbe(t, gooseVersionCase{reader: plan.reader, older: plan.older, root: "Lineage", capacity: 1,
				data: data, print: []string{"values[0].Tier as u64", "values[0].Seq"}})
			got.reads(t, 1)
			if got.clamped != 1 {
				t.Fatalf("forged_ordinal on the %s plan: clamped %d, want exactly 1: %s", plan.name, got.clamped, got.raw)
			}
			if len(got.printed) != 2 || got.printed[0] != "0" || got.printed[1] != "9" {
				t.Fatalf("forged_ordinal on the %s plan: tier/seq %v, want None (0) and 9", plan.name, got.printed)
			}
		})
	}
}

// union_tag_both_plans (§5.8 row 12's union lane): the union tag forged past
// the OLD writer's two arms selects no arm on the NEW build's compiled plan and
// on the OLD build's identity plan alike: None, clamped == 1 on both.
func TestGooseFixedVersioningUnionTagBothPlans(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	older := evolutionLoad(t, "test/tables/VOLD_union_append.schema")
	newer := evolutionLoad(t, "test/tables/VNEW_union_append.schema")
	data := gooseCorpusFile(t, corpus, "old_union_append.bin")
	// the record body is pick (a u8 tag, then the widest arm's four bytes)
	// then seq (i32)
	tag := len(data) - 9
	if data[tag] != 1 {
		t.Fatalf("union_tag: the record's tag is %d, not the manifest's alpha (1)", data[tag])
	}
	data[tag] = 3
	for _, plan := range []struct {
		name   string
		reader *ir.Unit
		older  []*ir.Unit
	}{{"compiled", newer, []*ir.Unit{older}}, {"identity", older, nil}} {
		t.Run(plan.name, func(t *testing.T) {
			t.Parallel()
			got := gooseVersionProbe(t, gooseVersionCase{reader: plan.reader, older: plan.older, root: "Lineage", capacity: 1,
				data: data, print: []string{"values[0].Pick.Type as u64", "values[0].Seq"}})
			got.reads(t, 1)
			if got.clamped != 1 {
				t.Fatalf("union_tag on the %s plan: clamped %d, want exactly 1: %s", plan.name, got.clamped, got.raw)
			}
			if len(got.printed) != 2 || got.printed[0] != "0" || got.printed[1] != "15" {
				t.Fatalf("union_tag on the %s plan: tag/seq %v, want None (0) and 15", plan.name, got.printed)
			}
		})
	}
}

// writer_bound_count (§5.8 row 4, step 7): the forged count 7 under the
// WRITER's [..4] (the reader's is [..8]) refuses count_over_bound before any
// destination byte; both endpoints of the writer's bound read.
func TestGooseFixedVersioningWriterBoundCount(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	older := evolutionLoad(t, "test/tables/VOLD_array_bounded_grow.schema")
	newer := evolutionLoad(t, "test/tables/VNEW_array_bounded_grow.schema")
	base := gooseCorpusFile(t, corpus, "old_array_bounded_grow.bin")
	lb := int(uint32(base[16]) | uint32(base[17])<<8 | uint32(base[18])<<16 | uint32(base[19])<<24)
	count := 20 + lb + 8 + 4 // the header, the record hash, `lead`
	if base[count] != 4 {
		t.Fatalf("writer_bound_count: the count word is %d, not the manifest's 4", base[count])
	}
	for _, c := range []struct {
		name   string
		count  byte
		refuse bool
	}{{"forged_7", 7, true}, {"minus_1", 0xFF, true}, {"at_0", 0, false}, {"at_4", 4, false}} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			data := append([]byte(nil), base...)
			data[count] = c.count
			if c.count == 0xFF {
				data[count+1], data[count+2], data[count+3] = 0xFF, 0xFF, 0xFF
			}
			got := gooseVersionProbe(t, gooseVersionCase{reader: newer, older: []*ir.Unit{older}, root: "ArrayBoundedGrow",
				capacity: 1, poison: c.refuse, data: data, fresh: c.refuse, print: []string{"values[0].ValsCount"}})
			if c.refuse {
				got.refuses(t, "count_over_bound", false, 0)
				return
			}
			got.reads(t, 1)
			if len(got.printed) != 1 || got.printed[0] != strconv.Itoa(int(c.count)) {
				t.Fatalf("writer_bound_count %s: the count landed %v, want %d", c.name, got.printed, c.count)
			}
		})
	}
}

// ---- the probe --------------------------------------------------------------

// gooseVersionCase is one generated program: the reader unit, the older units
// whose entries the build is handed oldest first (the reader's own entry
// last), how many of them are retired (the floor), and what it prints.
type gooseVersionCase struct {
	reader   *ir.Unit
	older    []*ir.Unit
	retire   int
	root     string
	capacity int
	poison   bool
	data     []byte
	// want, when it names a file: the manifest line whose values the reader
	// owes, compared as record images
	want gooseManifestLine
	// fresh: print the destination's record image beside a fresh value's and
	// a poisoned one's, for a refusal's "wrote nothing"
	fresh bool
	// twice: read the file a second time into the same storage
	twice bool
	// print: Goose expressions printed one per line after the report
	print []string
}

type gooseVersionResult struct {
	raw                                     string
	n                                       int
	reason                                  string
	clamped, widened, unknown, kindMismatch int64
	malformed                               bool
	hash                                    uint64
	secondUnknown                           int64
	got, want                               []string // record images, one per record
	dest, fresh, poison                     string
	printed                                 []string
	file                                    string
}

func (r gooseVersionResult) reads(t *testing.T, records int) {
	t.Helper()
	if r.n != records || r.reason != "ok" || r.malformed {
		t.Fatalf("%s: the reader did not read %d records cleanly: %s", r.file, records, r.raw)
	}
}

func (r gooseVersionResult) owes(t *testing.T) {
	t.Helper()
	if len(r.got) == 0 || len(r.got) != len(r.want) {
		t.Fatalf("%s: %d record images against %d owed: %s", r.file, len(r.got), len(r.want), r.raw)
	}
	for k := range r.got {
		if r.got[k] != r.want[k] {
			t.Fatalf("%s: record %d is not the value the manifest owes\n got  %s\n want %s", r.file, k, r.got[k], r.want[k])
		}
	}
}

// refuses holds a refusal to §5.3's joint answer: the name, -1, no counter
// moved, malformed false, the file's hash where check says one is owed, and
// the destination still what it was.
func (r gooseVersionResult) refuses(t *testing.T, name string, check bool, hash uint64) {
	t.Helper()
	if r.n != -1 || r.reason != name {
		t.Fatalf("%s: owes %s, answered n=%d %s: %s", r.file, name, r.n, r.reason, r.raw)
	}
	if r.malformed {
		t.Fatalf("%s: a refusal by name never sets malformed too: %s", r.file, r.raw)
	}
	if r.clamped != 0 || r.widened != 0 || r.unknown != 0 || r.kindMismatch != 0 {
		t.Fatalf("%s: REFUSE is total and no counter moves: %s", r.file, r.raw)
	}
	if check && r.hash != hash {
		t.Fatalf("%s: %s reports THE FILE'S hash 0x%016x, not 0x%016x", r.file, name, hash, r.hash)
	}
	if r.dest == "" {
		t.Fatalf("%s: the probe printed no destination image", r.file)
	}
	if r.dest != r.before() {
		t.Fatalf("%s: REFUSE wrote the destination\n got  %s\n was  %s", r.file, r.dest, r.before())
	}
}

func (r gooseVersionResult) before() string {
	if r.poison != "" {
		return r.poison
	}
	return r.fresh
}

func gooseFileHash(data []byte) uint64 {
	var h uint64
	for i := range 8 {
		h |= uint64(data[8+i]) << (8 * i)
	}
	return h
}

func gooseCorpusFile(t *testing.T, corpus, file string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(corpus, file))
	if err != nil {
		t.Fatalf("the reference's %s is not in the corpus: %v", file, err)
	}
	return data
}

// gooseFixedRecords is a file's record count under the unit's own entry.
func gooseFixedRecords(t *testing.T, data []byte, u *ir.Unit, root string) int {
	t.Helper()
	e, ok := FixedEvolutionOf(u, root)
	if !ok {
		t.Fatalf("no fixed root %s", root)
	}
	lb := int64(uint32(data[16]) | uint32(data[17])<<8 | uint32(data[18])<<16 | uint32(data[19])<<24)
	return int((int64(len(data)) - 20 - lb) / e.Record)
}

func gooseVersionProbe(t *testing.T, c gooseVersionCase) gooseVersionResult {
	t.Helper()
	if c.capacity < 1 {
		c.capacity = 1
	}
	var lineage map[string][]FixedEvolutionEntry
	if len(c.older) > 0 {
		var entries []FixedEvolutionEntry
		for i, o := range c.older {
			e, ok := FixedEvolutionOf(o, c.root)
			if !ok {
				t.Fatalf("an older unit has no fixed root %s", c.root)
			}
			e.Retired = i < c.retire
			if e.Retired {
				e.Reason = "the test retires it"
			}
			entries = append(entries, e)
		}
		lineage = map[string][]FixedEvolutionEntry{c.root: entries}
	} else if c.retire > 0 {
		t.Fatal("a floor needs a lineage")
	}
	files, err := goose.Generate(c.reader)
	if err != nil {
		t.Fatalf("goose type surface: %v", err)
	}
	tables, err := GenerateLineage(c.reader, lineage)
	if err != nil {
		t.Fatalf("goose table surface: %v", err)
	}
	maps.Copy(files, tables)
	var mods []string
	for name := range files {
		if before, ok := strings.CutSuffix(name, ".goose"); ok {
			mods = append(mods, before)
		}
	}
	sort.Strings(mods)
	ns := goose.Namespace(c.reader)
	root := ns + "::" + c.root
	var b strings.Builder
	b.WriteString("import os;\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s;\n", m)
	}
	b.WriteString("\n" + gooserun.HexProbe + "\n")
	fmt.Fprintf(&b, "fn image(v: const %s&) {\n    var img: u8[>..] = [];\n    %s::_%sFixedWrite(img, v);\n    hex(img);\n}\n\n", root, ns, c.root)
	b.WriteString("fn main() {\n")
	b.WriteString("    var data: u8[>..] = [];\n    if !read_file(\"in.bin\", data) { print(\"read-failed\"); return; }\n")
	fmt.Fprintf(&b, "    var values: %s[%d] = [%s { .. }; %d];\n", root, c.capacity, root, c.capacity)
	if c.poison {
		// THE POISON IS LAID THROUGH THE UNIT'S OWN VALUE SURFACE, field by
		// field (§5.7's third conforming form, §5.9 #35): every leaf 0x5A,
		// every count and length at its bound, every optional present.
		st := fixedRootStruct(t, c.reader, c.root)
		for k := 0; k < c.capacity; k++ {
			for _, a := range goosePoison(st, fmt.Sprintf("values[%d]", k)) {
				fmt.Fprintf(&b, "    %s;\n", a)
			}
		}
	}
	if c.fresh {
		fmt.Fprintf(&b, "    let fresh = %s { .. };\n    print(\"fresh\");\n    image(fresh);\n", root)
		if c.poison {
			b.WriteString("    print(\"poison\");\n    image(values[0]);\n")
		}
	}
	b.WriteString("    var report = schema_rt::FixedEvolveReport { .. };\n")
	fmt.Fprintf(&b, "    let n = %s::%sFixedEvolveLoad(values[0..%d], data, report);\n", ns, c.root, c.capacity)
	b.WriteString("    print(\"report \", n, \" \", schema_rt::fixed_evolve_refusal_name(report.reason), \" \", report.clamped, \" \", report.widened, \" \", report.unknown, \" \", report.kind_mismatch, \" \", (if report.malformed { 1 } else { 0 }), \" \", report.hash);\n")
	if c.twice {
		b.WriteString("    var again = schema_rt::FixedEvolveReport { .. };\n")
		fmt.Fprintf(&b, "    let n2 = %s::%sFixedEvolveLoad(values[0..%d], data, again);\n", ns, c.root, c.capacity)
		b.WriteString("    print(\"again \", n2, \" \", again.unknown);\n")
	}
	if c.fresh {
		b.WriteString("    print(\"dest\");\n    image(values[0]);\n")
	}
	for _, p := range c.print {
		fmt.Fprintf(&b, "    print(\"value \", %s);\n", p)
	}
	if c.want.file != "" {
		owed := gooseOwed(t, c.reader, c.root, c.want)
		for k := 0; k < c.capacity; k++ {
			fmt.Fprintf(&b, "    print(\"got\");\n    image(values[%d]);\n", k)
			fmt.Fprintf(&b, "    var w%d = %s { .. };\n", k, root)
			for _, a := range owed[k] {
				fmt.Fprintf(&b, "    w%d%s;\n", k, a)
			}
			fmt.Fprintf(&b, "    print(\"want\");\n    image(w%d);\n", k)
		}
	}
	b.WriteString("}\n")
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(gooserun.Run(t, dir, "main.goose", map[string][]byte{"in.bin": c.data}))
	return gooseVersionParse(t, out, c.want.file)
}

func gooseVersionParse(t *testing.T, out, file string) gooseVersionResult {
	t.Helper()
	r := gooseVersionResult{raw: strings.TrimSpace(out), file: file}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	next := func(i int) string {
		if i+1 >= len(lines) {
			t.Fatalf("the probe ended after %q:\n%s", lines[i], out)
		}
		return lines[i+1]
	}
	sawReport := false
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		switch {
		case strings.HasPrefix(l, "report "):
			f := strings.Fields(l)
			if len(f) != 9 {
				t.Fatalf("an unreadable report line %q", l)
			}
			n, _ := strconv.Atoi(f[1])
			r.n, r.reason = n, f[2]
			r.clamped, _ = strconv.ParseInt(f[3], 10, 64)
			r.widened, _ = strconv.ParseInt(f[4], 10, 64)
			r.unknown, _ = strconv.ParseInt(f[5], 10, 64)
			r.kindMismatch, _ = strconv.ParseInt(f[6], 10, 64)
			r.malformed = f[7] == "1"
			r.hash, _ = strconv.ParseUint(f[8], 10, 64)
			sawReport = true
		case strings.HasPrefix(l, "again "):
			f := strings.Fields(l)
			r.secondUnknown, _ = strconv.ParseInt(f[2], 10, 64)
			if f[1] != strconv.Itoa(r.n) {
				t.Fatalf("the second read returned %s, the first %d", f[1], r.n)
			}
		case strings.HasPrefix(l, "value "):
			r.printed = append(r.printed, strings.TrimPrefix(l, "value "))
		case l == "got":
			r.got = append(r.got, next(i))
			i++
		case l == "want":
			r.want = append(r.want, next(i))
			i++
		case l == "dest":
			r.dest = next(i)
			i++
		case l == "fresh":
			r.fresh = next(i)
			i++
		case l == "poison":
			r.poison = next(i)
			i++
		}
	}
	if !sawReport {
		t.Fatalf("the probe printed no report:\n%s", out)
	}
	return r
}

// ---- the manifest -----------------------------------------------------------

type gooseManifestValue struct{ path, value string }

type gooseManifestLine struct {
	file, row, root string
	records         int
	values          []gooseManifestValue
}

type gooseManifest struct{ lines map[string]gooseManifestLine }

func (m gooseManifest) line(t *testing.T, file string) gooseManifestLine {
	t.Helper()
	l, ok := m.lines[file]
	if !ok {
		t.Fatalf("the corpus manifest carries no row for %s — the oracle and the bytes under test have come apart", file)
	}
	return l
}

// gooseVersionManifest reads build/fixedform-corpus/manifest.txt (§5.7 step
// 1): the head split on spaces, `values=` running to the end of the line, a
// quoted value whole.
func gooseVersionManifest(t *testing.T, corpus string) gooseManifest {
	t.Helper()
	f, err := os.Open(filepath.Join(corpus, "manifest.txt"))
	if err != nil {
		t.Fatalf("the corpus manifest is not readable, so no row has an oracle: %v", err)
	}
	defer f.Close()
	m := gooseManifest{lines: map[string]gooseManifestLine{}}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		text := sc.Text()
		head, values, ok := strings.Cut(text, " values=")
		if !ok {
			continue
		}
		var l gooseManifestLine
		for field := range strings.FieldsSeq(head) {
			k, v, _ := strings.Cut(field, "=")
			switch k {
			case "file":
				l.file = v
			case "root":
				l.root = v
			case "records":
				l.records, _ = strconv.Atoi(v)
			case "row":
				l.row = v
			}
		}
		for _, kv := range gooseSplitValues(values) {
			p, v, ok := strings.Cut(kv, "=")
			if !ok {
				t.Fatalf("manifest %s: a value %q with no `=`", l.file, kv)
			}
			l.values = append(l.values, gooseManifestValue{p, v})
		}
		m.lines[l.file] = l
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return m
}

// gooseSplitValues splits on commas outside a quoted value.
func gooseSplitValues(s string) []string {
	var out []string
	var cur strings.Builder
	quoted, escaped := false, false
	for _, c := range s {
		switch {
		case escaped:
			escaped = false
		case c == '\\' && quoted:
			escaped = true
		case c == '"':
			quoted = !quoted
		case c == ',' && !quoted:
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteRune(c)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// ---- the owed value ---------------------------------------------------------

// gooseWriterSchema is the schema that wrote a corpus file, by its side
// prefix (§5.7 step 1, §5.9 #28): old_, new_, mid_, a_, b_ and many_.
func gooseWriterSchema(t *testing.T, line gooseManifestLine) string {
	t.Helper()
	for prefix, tag := range map[string]string{"old_": "VOLD_", "many_": "VOLD_", "new_": "VNEW_", "mid_": "VMID_", "a_": "VBRA_", "b_": "VBRB_"} {
		if strings.HasPrefix(line.file, prefix) {
			return "test/tables/" + tag + line.row + ".schema"
		}
	}
	t.Fatalf("%s: no writer schema for this file's side", line.file)
	return ""
}

// gooseOwed turns a manifest line into Goose assignments onto a fresh value of
// the READER's root, one list per record. The manifest names the WRITER's
// fields, so each path is walked through the writer's schema and the reader's
// in step and joined by WIRE NAME (§5.9 #41: a `was =` rename compares); a
// field the reader does not name (the unknown census) is skipped.
func gooseOwed(t *testing.T, reader *ir.Unit, root string, line gooseManifestLine) [][]string {
	t.Helper()
	rst := fixedRootStruct(t, reader, root)
	wst := fixedRootStruct(t, evolutionLoad(t, gooseWriterSchema(t, line)), root)
	out := make([][]string, line.records)
	for _, v := range line.values {
		rec, rest, ok := strings.Cut(v.path, ".")
		if !ok || len(rec) < 2 || rec[0] != 'r' {
			t.Fatalf("%s: a manifest path %q with no record", line.file, v.path)
		}
		k, err := strconv.Atoi(rec[1:])
		if err != nil || k < 0 || k >= line.records {
			t.Fatalf("%s: a manifest path %q past the %d records", line.file, v.path, line.records)
		}
		a, ok := gooseAssign(t, wst, rst, strings.Split(rest, "."), v.value)
		if !ok {
			continue
		}
		out[k] = append(out[k], a...)
	}
	return out
}

// gooseSeg splits `name[3]` into the name and its index (-1 for none).
func gooseSeg(s string) (string, int) {
	name, idx, ok := strings.Cut(s, "[")
	if !ok {
		return s, -1
	}
	n, _ := strconv.Atoi(strings.TrimSuffix(idx, "]"))
	return name, n
}

func gooseFieldNamed(st *ir.Struct, name string) *ir.Field {
	for _, f := range st.Fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}

func gooseFieldWired(st *ir.Struct, wire string) *ir.Field {
	for _, f := range st.Fields {
		if ir.TableFieldWireName(f) == wire {
			return f
		}
	}
	return nil
}

// gooseJoin is the reader's field for the writer's field `name`, by wire name.
func gooseJoin(wst, rst *ir.Struct, name string) (*ir.Field, *ir.Field) {
	wf := gooseFieldNamed(wst, name)
	if wf == nil {
		return nil, nil
	}
	return wf, gooseFieldWired(rst, ir.TableFieldWireName(wf))
}

var gooseCompanions = map[string]string{"_length": "Length", "_count": "Count", "_present": "Present"}

func gooseAssign(t *testing.T, wst, rst *ir.Struct, segs []string, value string) ([]string, bool) {
	t.Helper()
	var prefix string
	var pre []string
	for i := 0; i < len(segs); i++ {
		name, idx := gooseSeg(segs[i])
		last := i == len(segs)-1
		wf, rf := gooseJoin(wst, rst, name)
		if wf == nil && last {
			// a companion: <field>_length, _count, _present
			for suffix, slot := range gooseCompanions {
				base := strings.TrimSuffix(name, suffix)
				if base == name {
					continue
				}
				if bw, br := gooseJoin(wst, rst, base); bw != nil {
					if br == nil {
						return nil, false
					}
					v := value
					if suffix == "_present" {
						v = map[string]string{"0": "false", "1": "true"}[value]
					}
					return append(pre, fmt.Sprintf("%s.%s%s = %s", prefix, ir.GoExportName(br.Name), slot, v)), true
				}
			}
		}
		if wf == nil {
			t.Fatalf("a manifest path names %q, which the writer's schema does not declare", name)
		}
		if rf == nil {
			return nil, false // a field the reader does not name: the unknown census
		}
		if rf.Type.Optional && !wf.Type.Optional {
			// THE PRESENT COMPANION OWES 1 (§5.9 #40): the writer carried the
			// value, so the reader's `?T` is present
			pre = append(pre, fmt.Sprintf("%s.%s = true", prefix, ir.GoExportName(rf.Name+"_present")))
		}
		prefix += "." + ir.GoExportName(rf.Name)
		text := rf.Type.Kind == ir.TString || rf.Type.Kind == ir.TBytes || rf.Type.Kind == ir.TWString
		if idx >= 0 && !(last && text) {
			prefix += fmt.Sprintf("[%d]", idx)
		}
		if last {
			if we, ok := wf.Type.Ref.(*ir.Enum); ok && rf.Type.Kind == ir.TNamed {
				// the writer's ordinal, joined to the reader's variant by wire name
				return append(pre, fmt.Sprintf("%s = %s", prefix, gooseEnumOrdinal(t, we, rf.Type.Ref.(*ir.Enum), value))), true
			}
			return append(pre, gooseScalar(t, rf, prefix, idx, value)...), true
		}
		if rf.Type.Kind != ir.TNamed || wf.Type.Kind != ir.TNamed {
			t.Fatalf("a manifest path walks through %s, which nests nothing", rf.Name)
		}
		switch rref := rf.Type.Ref.(type) {
		case *ir.Struct:
			wref, ok := wf.Type.Ref.(*ir.Struct)
			if !ok {
				t.Fatalf("%s is a struct to the reader and not to the writer", rf.Name)
			}
			wst, rst = wref, rref
		case *ir.Union:
			wref, ok := wf.Type.Ref.(*ir.Union)
			if !ok {
				t.Fatalf("%s is a union to the reader and not to the writer", rf.Name)
			}
			if i+1 >= len(segs) {
				t.Fatalf("a manifest path ends on the union %s", rf.Name)
			}
			armName, _ := gooseSeg(segs[i+1])
			if armName == "type" {
				return append(pre, fmt.Sprintf("%s.Type = %s", prefix, gooseUnionTag(t, wref, rref, value))), true
			}
			warm, rarm := gooseArms(wref, rref, armName)
			if warm == nil {
				t.Fatalf("a manifest path names the arm %q, which the writer's %s does not declare", armName, wref.Name)
			}
			if rarm == nil {
				return nil, false
			}
			prefix += "." + ir.GoExportName(rarm.Name)
			i++
			if i == len(segs)-1 {
				return append(pre, gooseScalar(t, rarm.F, prefix, -1, value)...), true
			}
			if warm.Ref == nil || rarm.Ref == nil {
				t.Fatalf("a manifest path walks into the payload-free arm %s", rarm.Name)
			}
			wst, rst = warm.Ref, rarm.Ref
		default:
			t.Fatalf("a manifest path walks through %s, which nests nothing", rf.Name)
		}
	}
	return nil, false
}

// gooseEnumOrdinal is the reader's ordinal for the writer's raw ordinal, by
// variant wire name; a raw past the writer's variants stays as written.
func gooseEnumOrdinal(t *testing.T, w, r *ir.Enum, value string) string {
	t.Helper()
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		t.Fatalf("enum %s: a value %q that is no ordinal", w.Name, value)
	}
	if n == 0 || n > len(w.Variants) {
		return value
	}
	for j := range r.Variants {
		if r.VariantWireName(j) == w.VariantWireName(n-1) {
			return strconv.Itoa(j + 1)
		}
	}
	t.Fatalf("enum %s: the writer's variant %s has no reader variant", w.Name, w.Variants[n-1])
	return ""
}

// goosePoison is every storage slot of a value laid to the poison: integer
// leaves 0x5A in every byte of their width, bools true, floats 0x5A5A…,
// text 0x5A to its bound, counts and lengths AT their bound (a count past it
// is not a value the surface holds), optionals present; a union's arms are
// poisoned and its tag left alone.
func goosePoison(st *ir.Struct, prefix string) []string {
	var out []string
	for _, f := range st.Fields {
		out = append(out, goosePoisonField(f, prefix+"."+ir.GoExportName(f.Name), prefix, ir.GoExportName(f.Name))...)
	}
	return out
}

func goosePoisonField(f *ir.Field, slot, owner, name string) []string {
	var out []string
	if f.Type.Optional {
		out = append(out, fmt.Sprintf("%s.%s = true", owner, ir.GoExportName(f.Name+"_present")))
	}
	switch f.Array {
	case ir.ArrayNone:
		return append(out, goosePoisonLeaf(f, slot, owner, name)...)
	case ir.ArrayFixed, ir.ArrayCounted:
		if f.Array == ir.ArrayCounted {
			out = append(out, fmt.Sprintf("%s.%sCount = %d", owner, name, f.ArrayBound))
		}
		for i := int64(0); i < f.ArrayBound; i++ {
			out = append(out, goosePoisonLeaf(f, fmt.Sprintf("%s[%d]", slot, i), owner, fmt.Sprintf("%s#%d", name, i))...)
		}
	}
	return out
}

func goosePoisonLeaf(f *ir.Field, slot, owner, name string) []string {
	lengthSlot := func() string {
		if base, i, ok := strings.Cut(name, "#"); ok {
			return fmt.Sprintf("%s.%sLength[%s]", owner, base, i)
		}
		return fmt.Sprintf("%s.%sLength", owner, name)
	}
	fill := func(bits int) string {
		var v uint64
		for b := 0; b < bits; b += 8 {
			v |= uint64(0x5A) << b
		}
		return strconv.FormatUint(v, 10)
	}
	switch f.Type.Kind {
	case ir.TBool:
		return []string{slot + " = true"}
	case ir.TFloat32:
		return []string{slot + " = schema_rt::fixed_bits_f32(0x5A5A5A5A)"}
	case ir.TFloat64:
		return []string{slot + " = schema_rt::fixed_bits_f64(0x5A5A5A5A5A5A5A5A)"}
	case ir.TInt, ir.TFixed:
		w := f.Type.Width
		if f.Type.Kind == ir.TFixed {
			for _, n := range []int{8, 16, 32, 64} {
				if w <= n {
					w = n
					break
				}
			}
		}
		if w > 64 {
			return nil // an Int128's two lanes: no row of the gate carries one
		}
		return []string{fmt.Sprintf("%s = %s", slot, fill(w))}
	case ir.TBits:
		v, _ := strconv.ParseUint(fill(64), 10, 64)
		if f.Type.Width < 64 {
			v &= (uint64(1) << f.Type.Width) - 1
		}
		return []string{fmt.Sprintf("%s = %d", slot, v)}
	case ir.TString, ir.TBytes, ir.TWString:
		unit := "90"
		if f.Type.Kind == ir.TWString {
			unit = "23130"
		}
		out := []string{fmt.Sprintf("%s = %d", lengthSlot(), f.Type.Size)}
		for i := int64(0); i < f.Type.Size; i++ {
			out = append(out, fmt.Sprintf("%s[%d] = %s", slot, i, unit))
		}
		return out
	case ir.TNamed:
		switch r := f.Type.Ref.(type) {
		case *ir.Enum:
			return []string{fmt.Sprintf("%s = %s", slot, fill(r.StorageBits))}
		case *ir.Flags:
			return []string{fmt.Sprintf("%s = %s", slot, fill(64))}
		case *ir.Struct:
			return goosePoison(r, slot)
		case *ir.Union:
			var out []string
			for _, v := range r.Variants {
				if v.F == nil || v.Void() {
					continue
				}
				arm := *v.F
				arm.Name = v.Name
				out = append(out, goosePoisonField(&arm, slot+"."+ir.GoExportName(v.Name), slot, ir.GoExportName(v.Name))...)
			}
			return out
		}
	}
	return nil
}

// gooseArms joins the writer's arm `name` to the reader's by wire name.
func gooseArms(w, r *ir.Union, name string) (*ir.UnionVariant, *ir.UnionVariant) {
	for i := range w.Variants {
		if w.Variants[i].Name != name {
			continue
		}
		for j := range r.Variants {
			if r.Variants[j].WireName() == w.Variants[i].WireName() {
				return &w.Variants[i], &r.Variants[j]
			}
		}
		return &w.Variants[i], nil
	}
	return nil, nil
}

// gooseUnionTag is the reader's tag for the writer's: the manifest gives the
// WRITER's arm ordinal, joined to the reader's arm by wire name (None stays 0).
func gooseUnionTag(t *testing.T, w, r *ir.Union, value string) string {
	t.Helper()
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 || n > len(w.Variants) {
		t.Fatalf("union %s: a tag %q that is no writer ordinal", w.Name, value)
	}
	if n == 0 {
		return "0"
	}
	for j := range r.Variants {
		if r.Variants[j].WireName() == w.Variants[n-1].WireName() {
			return strconv.Itoa(j + 1)
		}
	}
	t.Fatalf("union %s: the writer's arm %s has no reader arm", w.Name, w.Variants[n-1].Name)
	return ""
}

// gooseScalar is the assignment of one manifest value onto the reader's slot.
func gooseScalar(t *testing.T, f *ir.Field, slot string, idx int, value string) []string {
	t.Helper()
	switch f.Type.Kind {
	case ir.TString:
		s, err := strconv.Unquote(value)
		if err != nil {
			t.Fatalf("%s: text %q: %v", f.Name, value, err)
		}
		var out []string
		for i := 0; i < len(s); i++ {
			out = append(out, fmt.Sprintf("%s[%d] = %d", slot, i, s[i]))
		}
		return out
	case ir.TWString:
		units := gooseUTF16(t, value)
		var out []string
		for i, u := range units {
			out = append(out, fmt.Sprintf("%s[%d] = %d", slot, i, u))
		}
		return out
	case ir.TBytes:
		if idx < 0 {
			t.Fatalf("%s: a bytes value with no index", f.Name)
		}
		return []string{fmt.Sprintf("%s[%d] = %s", slot, idx, value)}
	case ir.TFloat32:
		return []string{fmt.Sprintf("%s = schema_rt::fixed_bits_f32(0x%08x)", slot, gooseFloatBits(t, f, value, 32))}
	case ir.TFloat64:
		return []string{fmt.Sprintf("%s = schema_rt::fixed_bits_f64(0x%016x)", slot, gooseFloatBits(t, f, value, 64))}
	case ir.TFixed:
		_, raw, ok := strings.Cut(value, "|raw=")
		if !ok {
			raw = value // an element of a fixed array is its raw value
		}
		return []string{fmt.Sprintf("%s = %s", slot, gooseInt(t, raw))}
	case ir.TBool:
		return []string{fmt.Sprintf("%s = %s", slot, map[string]string{"0": "false", "1": "true", "false": "false", "true": "true"}[value])}
	}
	return []string{fmt.Sprintf("%s = %s", slot, gooseInt(t, value))}
}

// gooseInt spells a decimal integer for Goose: int64's minimum has no
// literal (docs/PORT-GOOSE.md, THE NUMBER MODEL).
func gooseInt(t *testing.T, v string) string {
	t.Helper()
	n, ok := new(big.Int).SetString(v, 10)
	if !ok {
		t.Fatalf("a manifest integer %q that is no decimal", v)
	}
	if n.Cmp(big.NewInt(math.MinInt64)) == 0 {
		return "(-9223372036854775807 - 1)"
	}
	return n.String()
}

// gooseFloatBits is the bit pattern a float slot owes: the manifest's
// `<digits>|0x<bits>` half, widened f32 -> f64 as Go's float64(float32)
// does when the reader's slot is wider than the writer's bits (a NaN keeps its
// payload and takes the quiet bit, the FU ruling).
func gooseFloatBits(t *testing.T, f *ir.Field, value string, width int) uint64 {
	t.Helper()
	_, hexPart, ok := strings.Cut(value, "|0x")
	if !ok {
		t.Fatalf("%s: the manifest's %q carries no |0x<bits> half: a decimal alone is not a bit-exact oracle", f.Name, value)
	}
	bits, err := strconv.ParseUint(hexPart, 16, 64)
	if err != nil {
		t.Fatalf("%s: unreadable bits %q: %v", f.Name, hexPart, err)
	}
	if width == 64 && len(hexPart) <= 8 {
		b := uint32(bits)
		if b&0x7F800000 == 0x7F800000 && b&0x007FFFFF != 0 {
			return uint64(b>>31)<<63 | uint64(0x7FF)<<52 | uint64(1)<<51 | uint64(b&0x007FFFFF)<<29
		}
		return math.Float64bits(float64(math.Float32frombits(b)))
	}
	return bits
}

// gooseUTF16 reads a manifest `u"…"` value as code units: `\uXXXX` is one
// unit as written (a lone or paired surrogate stays what it is), every other
// rune its UTF-16 spelling.
func gooseUTF16(t *testing.T, v string) []uint16 {
	t.Helper()
	if !strings.HasPrefix(v, `u"`) || !strings.HasSuffix(v, `"`) {
		t.Fatalf("a wide text value %q that is no u\"…\"", v)
	}
	s := v[2 : len(v)-1]
	var out []uint16
	for i := 0; i < len(s); {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'u':
				n, err := strconv.ParseUint(s[i+2:i+6], 16, 16)
				if err != nil {
					t.Fatalf("a wide text escape in %q: %v", v, err)
				}
				out = append(out, uint16(n))
				i += 6
				continue
			case '\\', '"':
				out = append(out, uint16(s[i+1]))
				i += 2
				continue
			}
		}
		r := []rune(s[i:])[0]
		if r >= 0x10000 {
			r -= 0x10000
			out = append(out, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
		} else {
			out = append(out, uint16(r))
		}
		i += len(string(r))
	}
	return out
}
