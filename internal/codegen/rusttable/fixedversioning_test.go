package rusttable

// THE VERSIONING FIXTURES ON THE RUST LEG (docs/FIXED-FORM-VERSIONING-TESTS.md,
// the procedures in docs/FIXED-FORM-ALGORITHM.md §5). The Go leg's
// internal/codegen/gotable/fixedversioning_test.go is the worked example and
// this file is its twin: the same rows, the same two read columns, the same
// floor and hash rows.
//
//   NEW-READS-OLD    the widened reader reads the older writer's file: every
//                    old value lands exactly, the reader's tail is its declared
//                    default, and the counters are §5.4's — for an APPEND, all
//                    of them zero, because an append the reader knows is not an
//                    event.
//   OLD-REFUSES-NEW  the older reader given the widened writer's file refuses
//                    `layout_newer` BEFORE any record, reporting THE FILE'S
//                    HASH and nothing else, with no counter moved and
//                    `malformed` false (§5.3's joint answer), and the
//                    destination still the fresh value it went in as.
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
//
// ONE WORKSPACE, ONE `cargo test`, AND THAT IS THE TWO-MINUTE RULE (the
// 2026-09-10 ruling). A probe is a generated crate and there are fifty-odd of
// them; fifty-odd `cargo run`s would each take the target directory's build
// lock in turn and compile `serialize-official` again behind it. So every probe
// is a MEMBER of one workspace under build/rust-versioning-probes, the target
// directory is build/rust-versioning-target, and ONE `cargo test --workspace
// --no-fail-fast` builds the dependency once and runs every probe across every
// core. The Go subtests below do not shell out at all: they read that one run's
// output and name their own row in it.
//
// THE COST OF THAT CHOICE, named so nobody has to rediscover it: a crate that
// does not COMPILE fails the whole workspace, so one broken row reddens every
// row with the same message. That is the right trade while §5 is being ported —
// a compile failure against an API that does not exist yet is the FIRST red
// this file is for — and the per-row message says which it is.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/rust"
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
	// every other row is an APPEND and owes every counter at zero.
	widens bool
	// poison fills the destination with 0x5A through a byte view before the
	// load. §5.7: a `Reset` before the load only proves the load did not
	// clobber what was there, and a zero fill looks exactly like a zero
	// default, so neither can tell a prefill that ran from one that never did.
	poison bool
	// check is Rust source asserting the landed values, over `values` and `n`.
	check string
	// unported is the REFUSAL that stops this leg's language surface from
	// holding the row's kind at all, cited by name. §5.9 #44: such a row is ⚪
	// UNPORTED and skipped BY NAME, never 🔴 and NEVER FOLDED INTO A GREEN — a
	// red is a debt §5 owes, an unported row is a debt another section owes,
	// and a reviewer may not read either as "this row passed".
	unported string
}

var versionRows = []versionRow{
	{row: "array_bounded_grow"},
	{row: "array_elem_widen", widens: true, check: `
    // EXACTLY FOUR, PER RECORD (§5.9 #33). widened moves once per ENTRY, a fold
    // across a widen is forbidden, and the number is min(their_n, my_n) — which
    // on this row is the bound, 4. A leg that FOLDED the run and widened after
    // lands the reader's defaults over the writer's values, silently, and leaves
    // widened at ZERO; a leg that folds and still widens counts ONE. Neither
    // reads as four, which is why the assertion is exact and not "> 0".
    assert_eq!(
        report.widened, 4 * n as u32,
        "a widened [..4] run owes EXACTLY 4 widen entries per record, not {}", report.widened
    );`},
	{row: "array_fixed_grow", poison: true, check: `
    // 4 exact, 4 at the ELEMENT DEFAULT — a nested Vec's OWN defaults, x = 7
    // and y = 9, which a zero fill cannot fake (bill §12.6).
    assert!(
        values[0].lead == 0xAAAA_AAAA && values[0].trail == 0xBBBB_BBBB,
        "the poison survived in a neighbour: lead={} trail={}", values[0].lead, values[0].trail
    );
    for k in 0..4 {
        assert!(
            values[0].vals[k].x != 0x5A5A_5A5Au32 as i32,
            "slot {k} the writer carried is still poison"
        );
    }
    for k in 4..8 {
        assert!(
            values[0].vals[k].x == 7 && values[0].vals[k].y == 9,
            "slot {k} past the writer's bound is not the ELEMENT'S declared default: x={} y={}",
            values[0].vals[k].x, values[0].vals[k].y
        );
    }`},
	{row: "bits_grow", widens: true},
	{row: "bytes_grow"},
	{row: "constant_grow"},
	{row: "enum_append"},
	{row: "enum_width", widens: true},
	{row: "field_append", check: `
    assert!(
        values[0].x == 11 && values[0].y == 22 && values[0].z == 33,
        "the old writer's values did not land: x={} y={} z={}", values[0].x, values[0].y, values[0].z
    );
    assert!(
        values[0].w == 77,
        "the appended field is not its declared default: w={}", values[0].w
    );`},
	{row: "field_deprecate", sameHash: true},
	{row: "field_undeprecate", sameHash: true},
	{row: "fixed_I_grow", widens: true},
	{row: "flags_append"},
	{row: "float_widen", widens: true},
	{row: "int_widen", widens: true, check: `
    assert!(n == 3, "the int_widen file carries three records, not {n}");
    let want: [i32; 3] = [-1, -32768, 32767];
    for k in 0..3 {
        assert!(
            values[k].v == want[k],
            "record {k} widened wrong: {} and not {}", values[k].v, want[k]
        );
        assert!(
            values[k].lead == 0xAAAA_AAAA && values[k].trail == 0xBBBB_BBBB,
            "record {k} moved a neighbour: lead={:#x} trail={:#x}", values[k].lead, values[k].trail
        );
    }`},
	{row: "keyed_array_enum_append", poison: true, check: `
    // The slot an APPENDED KEY opened is the element's declared default, and
    // the element is a fixed table with a NONZERO one for exactly this reason
    // (§5.7): n = 7, which a zero fill cannot fake.
    for k in 0..4 {
        assert!(
            values[0].slots[k].n != 0x5A5A_5A5Au32 as i32,
            "slot {k} is still poison: the prefill did not run"
        );
    }
    assert!(
        values[0].slots[3].n == 7,
        "the slot the appended key opened is not its declared default: n={}", values[0].slots[3].n
    );`},
	{row: "nested_append"},
	{row: "optional_add", poison: true, check: `
    // THE PRESENT COMPANION IS THE ONE NEWER-ONLY FIELD THAT IS NOT A DEFAULT
    // (§5.9 #40, bill §12.8): the writer sent a T, so the ?T this reader
    // declares IS present and owes 1 — not the fresh value's false. A leg that
    // prefills the companion and emits no present entry reads a value the
    // writer sent as ABSENT: values intact, presence lost, and every value check
    // still passing.
    assert!(
        values[0].link_present,
        "T into ?T owes present = 1, and the prefill's false is not it (§5.9 #40)"
    );
    assert!(
        values[0].lead == 1 || values[0].lead != 0x5A5A_5A5A,
        "the poison survived in a neighbour: lead={:#x}", values[0].lead
    );`},
	{row: "range_widen"},
	{row: "rename_without_was", sameHash: true},
	{row: "string_grow"},
	{row: "uint_widen", widens: true},
	{row: "union_append"},
	{row: "union_arm_payload_widen", poison: true, check: `
    // THE ARM'S APPENDED FIELD IS ITS DECLARED DEFAULT, 55, and the only thing
    // that can land it is a PREFILL IMAGE whose union was reset ARM BY ARM at
    // each arm's own overlay storage, in declared order, with the tag None last
    // (§5.2's prefill, ruled an INSTRUCTION by §5.9 #38). A union zeroed whole
    // lands 0 here and every value check but this one still passes.
    let arm = values[0]
        .pick
        .alpha()
        .unwrap_or_else(|| panic!("the old writer's record does not select the alpha arm: tag={}", values[0].pick.tag()));
    assert!(
        arm.y == 55,
        "the field appended to the arm's payload is not its declared default: y={} (a union zeroed whole, not reset arm by arm)", arm.y
    );`},
	{row: "wstring_grow", unported: "kind 33 is not carried in a TABLE closure by this leg: compiler/widetext.go's tableWideTextTargets lists C, C++, C#, Dart and Go and NOT rust, so `refuseWideText` refuses a Rust unit that declares a wstring(N) field and there is no reader to generate. This harness reaches the backend directly and would BYPASS that refusal, which §5.9 #44 forbids folding into a green"},
}

// ---- NEW-READS-OLD ----------------------------------------------------------

func TestFixedVersioningNewReadsOld(t *testing.T) {
	out := versionRun(t)
	for _, r := range versionRows {
		t.Run(r.row, func(t *testing.T) {
			if r.unported != "" {
				t.Skipf("UNPORTED (§5.9 #44), not green and not red: %s", r.unported)
			}
			versionAssert(t, out, "v_"+r.row+"_new_reads_old")
		})
	}
}

// ---- OLD-REFUSES-NEW --------------------------------------------------------

func TestFixedVersioningOldRefusesNew(t *testing.T) {
	out := versionRun(t)
	for _, r := range versionRows {
		t.Run(r.row, func(t *testing.T) {
			if r.unported != "" {
				t.Skipf("UNPORTED (§5.9 #44), not green and not red: %s", r.unported)
			}
			versionAssert(t, out, "v_"+r.row+"_old_refuses_new")
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
	out := versionRun(t)
	for _, name := range []string{"floor_at", "floor_below", "floor_raise_live"} {
		t.Run(name, func(t *testing.T) {
			versionAssert(t, out, "v_"+name)
		})
	}
}

// ---- the hash ---------------------------------------------------------------

func TestFixedVersioningHash(t *testing.T) {
	out := versionRun(t)
	for _, name := range []string{"hash_unknown", "hash_known_bytes_differ", "hash_identity"} {
		t.Run(name, func(t *testing.T) {
			versionAssert(t, out, "v_"+name)
		})
	}
}

// ---- the writer's text cap --------------------------------------------------
//
// §5.2 TEXT: the span is `min(me.size, te.size) - 4` and the cap is `span /
// unit`, so the bound a forged length is held to is THE WRITER's and never this
// reader's grown one. The row is `string_grow` — a writer at `string(8)`, a
// reader at `string(16)` — with the length word forged to 12: inside the
// reader's bound, past the writer's span, and it must clamp to 8 and COUNT.

func TestFixedVersioningTextCap(t *testing.T) {
	out := versionRun(t)
	versionAssert(t, out, "v_string_grow_forged_length")
}

// ---- lineage_merge ----------------------------------------------------------
//
// Two branches append different fields; after the merge BOTH pre-merge files
// read on the merged build (the name-subset rule, bill §8a.1).

func TestFixedVersioningLineageMerge(t *testing.T) {
	out := versionRun(t)
	versionAssert(t, out, "v_lineage_merge")
}

// ---- the probes -------------------------------------------------------------

// versionProbe is ONE generated crate: the reader's unit, the lineage the build
// hands it (the older units' locked entries, OLDEST FIRST), how many of those
// entries the operator retired, and the Rust source of the module that asserts.
type versionProbe struct {
	name   string   // the crate directory and its package name
	reader string   // the reader's schema, by base name
	older  []string // the lineage handed it, oldest first
	retire int      // the first `retire` entries are marked retired
	src    string   // the probe module's Rust source
}

// versionProbeSet is every probe this file runs, and it is STATIC: the rows are
// a table, the floor is three retire levels of one reader, and the hash cases
// and lineage_merge are one crate each. Nothing here reads the corpus — only
// names files in it — so the set can be built before the oracle is checked.
func versionProbeSet(corpus string) []versionProbe {
	probes := make([]versionProbe, 0, 2*len(versionRows)+5)

	for _, r := range versionRows {
		if r.unported != "" {
			// NO PROBE AT ALL for a row whose kind this leg's language surface
			// cannot hold (§5.9 #44): generating one here would bypass the
			// compiler's own refusal and publish a green for a row that has no
			// reader. The mark and the reason live on the row.
			continue
		}
		// NEW-READS-OLD: the newer unit is handed the OLDER unit's locked entry.
		counters := `    assert!(
        report.widened == 0,
        "widened {} on an append row, and an append the reader knows is not an event", report.widened
    );`
		if r.widens {
			counters = `    assert!(report.widened != 0, "a WIDTH row moved no widened counter");`
		}
		probes = append(probes, versionProbe{
			name:   "probe_" + r.row + "_new_reads_old",
			reader: "VNEW_" + r.row,
			older:  []string{"VOLD_" + r.row},
			src: versionProbeSource(fmt.Sprintf(`
/// %[1]s, NEW-READS-OLD: the widened reader reads the older writer's file.
/// Every old value lands exactly, the reader's tail is its DECLARED DEFAULT,
/// and §5.4's counters are all at zero for an append (§5.3, §5.7).
#[test]
fn v_%[1]s_new_reads_old() {
    let data = std::fs::read(%[2]q).expect("the reference's old_%[1]s.bin");
    let mut values = vec![ROWTYPE::default(); 8];
%[3]s
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
    let n = n.unwrap_or_else(|| panic!(
        "the newer reader REFUSED the older writer's file: {} {:?}", report.reason.name(), report
    ));
    assert!(n >= 1, "a backward read lands at least one record, not {n}");
    assert!(
        !report.refused && report.reason == TableFixedReason::None,
        "a clean NEW-READS-OLD is not a refusal: {:?}", report
    );
    assert!(!report.malformed, "a clean backward read is not damage: {:?}", report);
    assert!(
        report.unknown == 0 && report.kind_mismatch == 0 && report.clamped == 0,
        "counters moved on a clean backward read: {:?}", report
    );
%[4]s
%[5]s
}
`, r.row, filepath.Join(corpus, "old_"+r.row+".bin"), versionPoison(r.poison), counters, r.check)),
		})

		// OLD-REFUSES-NEW: the older reader, no lineage beyond its own layout.
		body := `    let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
    assert!(
        n.is_none(),
        "the older reader did NOT refuse the newer writer's file: {:?} {:?}", n, report
    );
    assert_eq!(
        report.reason.name(), "layout_newer",
        "a hash in no lineage entry owes layout_newer, not {:?}", report.reason
    );
    assert!(report.refused, "a refusal by name sets refused: {:?}", report);
    assert_eq!(
        report.layout_hash, want,
        "layout_newer reports THE FILE'S hash, {:#018x}, not {:#018x}", want, report.layout_hash
    );
    assert!(
        !report.malformed,
        "a refusal by name never sets malformed too (§5.3, the joint answer): {:?}", report
    );
    assert!(
        report.widened == 0 && report.unknown == 0 && report.kind_mismatch == 0 && report.clamped == 0,
        "REFUSE is TOTAL: no counter moves: {:?}", report
    );
    assert!(
        as_bytes(&values[0]) == as_bytes(&fresh),
        "REFUSE wrote destination bytes: {:02x?}", as_bytes(&values[0])
    );`
		if r.sameHash {
			// A row whose edit moves no layout byte and no digest byte has NO
			// second column: the hashes are equal and the file reads in BOTH
			// directions (§5.7).
			body = `    let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
    let _ = want;
    let _ = as_bytes(&fresh);
    let n = n.unwrap_or_else(|| panic!(
        "a row that moved no layout byte must READ in both directions: {} {:?}",
        report.reason.name(), report
    ));
    assert!(n >= 1, "both directions read, so at least one record lands, not {n}");
    assert!(
        !report.refused && report.reason == TableFixedReason::None && !report.malformed,
        "an EQUAL HASH is not a version: {:?}", report
    );`
		}
		probes = append(probes, versionProbe{
			name:   "probe_" + r.row + "_old_refuses_new",
			reader: "VOLD_" + r.row,
			src: versionProbeSource(fmt.Sprintf(`
/// %[1]s, OLD-REFUSES-NEW: the older reader given the widened writer's file
/// refuses `+"`layout_newer`"+` BEFORE any record, on THE FILE'S HASH and nothing
/// else, with no counter moved and nothing written (bill §12.4, §5.3).
#[test]
fn v_%[1]s_old_refuses_new() {
    let data = std::fs::read(%[2]q).expect("the reference's new_%[1]s.bin");
    let want = u64::from_le_bytes(data[8..16].try_into().expect("the header's hash is eight bytes"));
    let fresh = ROWTYPE::default();
    let mut values = vec![ROWTYPE::default(); 8];
    // THE COMPARAND'S PADDING IS MADE TO MATCH BEFORE THE LOAD. "nothing
    // written" is checked through a RAW BYTE VIEW, and a Row carries PADDING
    // wherever a narrow member sits before a wider one -- tier then seq in the
    // two enum rows: one byte of ordinal, three of pad, four of scalar.
    // Padding bytes hold whatever the memory held, and a fresh value on the
    // stack need not agree there with a vector's slot out of the allocator, so
    // without this copy the assert reads allocator garbage as a write the
    // reader never made: red on Linux CI, green on a workstation whose heap
    // happened to come back zeroed. Copying FRESH'S OWN BYTES into every slot
    // settles the padding on BOTH sides of the compare, so afterwards EVERY
    // differing byte is a byte the reader itself stored, which is the thing
    // under test.
    unsafe {
        for v in values.iter_mut() {
            core::ptr::copy_nonoverlapping(
                (&fresh as *const ROWTYPE) as *const u8,
                (v as *mut ROWTYPE) as *mut u8,
                core::mem::size_of::<ROWTYPE>(),
            );
        }
    }
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
%[3]s
}
`, r.row, filepath.Join(corpus, "new_"+r.row+".bin"), body)),
		})
	}

	// THE WRITER'S TEXT CAP, FORGED. The only thing that can catch a cap taken
	// from the READER's bound is a length BETWEEN the two bounds, so this probe
	// forges one: lawful for the reader, past the writer's span, and owed a
	// clamp to the writer's 8 with one `clamped` counted (§5.2 TEXT, §5.4).
	probes = append(probes, versionProbe{
		name:   "probe_string_grow_forged_length",
		reader: "VNEW_string_grow",
		older:  []string{"VOLD_string_grow"},
		src: versionProbeSource(fmt.Sprintf(`
/// string_grow, THE WRITER'S CAP: a forged length inside the READER's bound but
/// past the WRITER's span clamps to the writer's span and counts one clamped.
#[test]
fn v_string_grow_forged_length() {
    let mut data = std::fs::read(%q).expect("the reference's old_string_grow.bin");
    // the records begin after the pinned header and the layout block
    let block = u32::from_le_bytes(data[16..20].try_into().expect("four bytes")) as usize;
    let at = 16 + 4 + block;
    // the writer's body is lead(4), then the text's LENGTH word, then its 8 bytes
    let len_at = at + 8 + 4;
    data[len_at..len_at + 4].copy_from_slice(&12i32.to_le_bytes());
    let mut values = vec![ROWTYPE::default(); 8];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
    let n = n.unwrap_or_else(|| panic!(
        "a forged LENGTH is not a refusal: {} {:?}", report.reason.name(), report
    ));
    assert!(n >= 1, "the forged record still reads, not {n}");
    assert_eq!(
        values[0].text_length, 8,
        "a length past THE WRITER's span must clamp to the writer's 8, not to this reader's 16"
    );
    assert_eq!(
        report.clamped, 1,
        "the clamp that fired must count ONE clamped: {:?}", report
    );
    assert!(!report.malformed, "a forged length is clamped, not damage: {:?}", report);
}
`, filepath.Join(corpus, "old_string_grow.bin"))),
	})

	// THE FLOOR. Nothing retired, entry 0 retired, entries 0 and 1 retired —
	// three readers of one lineage, VOLD then VMID then VNEW's own.
	floors := []struct {
		name   string
		retire int
		body   string
	}{
		{"floor_at", 0, `    // Nothing retired: the OLDEST file is AT the floor and it reads.
    let n = LOADFN(&mut values, &old_file, &mut plan, &mut remap, &mut report);
    assert!(
        n.is_some() && !report.refused && !report.malformed,
        "a file AT the floor must read: {:?} {:?}", n, report
    );`},
		{"floor_below", 1, `    // Entry 0 retired: the floor is 1, and the file one below it refuses
    // layout_unsupported — nothing decoded, no counter moved, AND THE HASH
    // FILLED (§5.9 #7: the operator needs to know which layout to stop writing).
    let want = u64::from_le_bytes(old_file[8..16].try_into().expect("eight bytes"));
    let n = LOADFN(&mut values, &old_file, &mut plan, &mut remap, &mut report);
    assert!(n.is_none(), "a file BELOW the floor is refused: {:?} {:?}", n, report);
    assert_eq!(
        report.reason.name(), "layout_unsupported",
        "below the floor owes layout_unsupported and not {:?}", report.reason
    );
    assert_eq!(
        report.layout_hash, want,
        "layout_unsupported carries the file's hash too: {:#018x} not {:#018x}", want, report.layout_hash
    );
    assert!(
        !report.malformed && report.widened == 0 && report.unknown == 0
            && report.kind_mismatch == 0 && report.clamped == 0,
        "REFUSE is TOTAL: {:?}", report
    );
    let mut report2 = TableFixedReport::default();
    let m = LOADFN(&mut values, &mid_file, &mut plan, &mut remap, &mut report2);
    assert!(
        m.is_some(),
        "the file AT the raised floor must still read: {:?} {:?}", m, report2
    );`},
		{"floor_raise_live", 2, `    // The floor raised by one: the file that read yesterday refuses today.
    let _ = &old_file;
    let n = LOADFN(&mut values, &mid_file, &mut plan, &mut remap, &mut report);
    assert!(n.is_none(), "the floor raised: yesterday's file must refuse: {:?}", report);
    assert_eq!(
        report.reason.name(), "layout_unsupported",
        "a retired entry is layout_unsupported and not {:?}", report.reason
    );`},
	}
	for _, f := range floors {
		probes = append(probes, versionProbe{
			name:   "probe_" + f.name,
			reader: "VNEW_floor",
			older:  []string{"VOLD_floor", "VMID_floor"},
			retire: f.retire,
			src: versionProbeSource(fmt.Sprintf(`
/// %[1]s (§5.2, §5.7's floor rows).
#[test]
fn v_%[1]s() {
    let old_file = std::fs::read(%[2]q).expect("the reference's old_floor.bin");
    let mid_file = std::fs::read(%[3]q).expect("the reference's mid_floor.bin");
    let mut values = vec![ROWTYPE::default(); 8];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
%[4]s
}
`, f.name, filepath.Join(corpus, "old_floor.bin"), filepath.Join(corpus, "mid_floor.bin"), f.body)),
		})
	}

	// THE HASH: three cases in one crate, because they share one reader.
	probes = append(probes, versionProbe{
		name:   "probe_hash",
		reader: "VNEW_field_append",
		older:  []string{"VOLD_field_append"},
		src: versionProbeSource(fmt.Sprintf(`
/// hash_unknown: a hash in NO lineage refuses layout_newer.
#[test]
fn v_hash_unknown() {
    let mut data = std::fs::read(%[1]q).expect("the reference's old_field_append.bin");
    data[8..16].copy_from_slice(&0xDEAD_BEEF_CAFE_F00Du64.to_le_bytes());
    let mut values = vec![ROWTYPE::default(); 8];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
    assert!(n.is_none(), "a stranger's hash is refused: {:?} {:?}", n, report);
    assert_eq!(report.reason.name(), "layout_newer", "hash_unknown: {:?}", report.reason);
    assert_eq!(report.layout_hash, 0xDEAD_BEEF_CAFE_F00D, "the refusal carries the FILE'S hash");
    assert!(!report.malformed, "a refusal by name is not damage: {:?}", report);
}

/// hash_known_bytes_differ: a KNOWN hash whose layout bytes differ from the
/// lock's is ONE name, layout_malformed — "a lie about a known version". The
/// seven §1.1 malformations under a known hash all land here, never in a
/// runtime walk (§5.3).
#[test]
fn v_hash_known_bytes_differ() {
    let mut data = std::fs::read(%[1]q).expect("the reference's old_field_append.bin");
    data[TABLE_FIXED_HEADER_BYTES + 4] ^= 0xFF;
    let mut values = vec![ROWTYPE::default(); 8];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
    assert!(n.is_none(), "a lie about a KNOWN version is refused: {:?} {:?}", n, report);
    assert_eq!(
        report.reason.name(), "layout_malformed",
        "bytes that disagree with the lock's are layout_malformed, not {:?}", report.reason
    );
    assert!(!report.malformed, "a refusal by name never sets the damage flag too: {:?}", report);
}

/// hash_identity: the reader's OWN hash selects the identity plan.
#[test]
fn v_hash_identity() {
    let data = std::fs::read(%[2]q).expect("the reference's new_field_append.bin");
    let mut values = vec![ROWTYPE::default(); 8];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
    assert!(
        n.is_some() && !report.refused && !report.malformed,
        "a reader's own file reads: {:?} {:?}", n, report
    );
    assert!(values[0].w == 777, "the identity plan lost a value: w={}", values[0].w);
}
`, filepath.Join(corpus, "old_field_append.bin"), filepath.Join(corpus, "new_field_append.bin"))),
	})

	// LINEAGE_MERGE: two branches append different fields; after the merge BOTH
	// pre-merge files read on the merged build (the name-subset rule, §8a.1).
	probes = append(probes, versionProbe{
		name:   "probe_lineage_merge",
		reader: "VNEW_lineage_merge",
		older:  []string{"VOLD_lineage_merge", "VBRA_lineage_merge", "VBRB_lineage_merge"},
		src: versionProbeSource(fmt.Sprintf(`
/// lineage_merge: both pre-merge files read on the merged build (bill §8a.1).
#[test]
fn v_lineage_merge() {
    for name in [%[1]q, %[2]q] {
        let data = std::fs::read(name).unwrap_or_else(|e| panic!("{name}: {e}"));
        let mut values = vec![ROWTYPE::default(); 8];
        let mut plan = vec![TableFixedEntry::default(); 4096];
        let mut remap = vec![0u16; 4096];
        let mut report = TableFixedReport::default();
        let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
        assert!(
            n.is_some() && !report.refused && !report.malformed,
            "{name}: both pre-merge files read on the merged build: {:?} {:?}", n, report
        );
    }
}
`, filepath.Join(corpus, "a_lineage_merge.bin"), filepath.Join(corpus, "b_lineage_merge.bin"))),
	})

	return probes
}

// versionPoison is the fill §5.7 asks for, or nothing.
func versionPoison(on bool) string {
	if !on {
		return ""
	}
	return `    // THE TEST OF THE PREFILL IS A POISON AND NOT A RESET (§5.7). A Reset
    // before the load only proves the load did not clobber what was there, and
    // a zero fill looks exactly like a zero default, so neither can tell a
    // prefill that RAN from one that never did.
    unsafe {
        core::ptr::write_bytes(
            values.as_mut_ptr() as *mut u8,
            0x5A,
            core::mem::size_of::<ROWTYPE>() * values.len(),
        );
    }`
}

// versionProbeSource wraps a probe's tests in the module preamble every one of
// them needs. ROWTYPE and LOADFN are substituted once the reader's own root is
// known, so a row's source can name the table without restating its name.
func versionProbeSource(tests string) string {
	return `// THE PROBE (docs/FIXED-FORM-ALGORITHM.md §5, the rows of
// docs/FIXED-FORM-VERSIONING-TESTS.md). Generated by
// internal/codegen/rusttable/fixedversioning_test.go; nothing here is tracked.
use crate::*;

/// A Row is plain data with no niche in it (SPEC §7.2) and the emitter derives
/// neither PartialEq nor Debug on one, so a BYTE VIEW is how a probe compares a
/// destination with a fresh value and how it poisons one.
#[allow(dead_code)]
fn as_bytes<T: Copy>(v: &T) -> &[u8] {
    unsafe { core::slice::from_raw_parts((v as *const T) as *const u8, core::mem::size_of::<T>()) }
}
` + tests
}

// ---- the harness ------------------------------------------------------------

func versionCorpus(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("../../../build/fixedform-corpus")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "old_field_append.bin")); err != nil {
		// THE SKIP IS A FAILURE WHEN SOMETHING PROMISED THE CORPUS. A bare
		// `go test ./...` on a tree that never built the oracle has nothing to
		// read and says so; but `make tables-rust-versioning` builds the corpus
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

func versionSchema(t *testing.T, base string) string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "test", "tables", base+".schema")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func versionUnit(t *testing.T, src string) *ir.Unit {
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

// versionRootName is the FILE's root: the one fixed table no other fixed table
// of the unit names by value. A row whose change is NESTED declares two.
func versionRootName(t *testing.T, u *ir.Unit) string {
	t.Helper()
	roots := ir.TableFixedRoots(u)
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
			return st.Name
		}
	}
	return roots[len(roots)-1].Name
}

// versionResult is the one cargo run's answer, shared by every test in this
// file: the combined output and whatever cargo itself said about exiting.
type versionResult struct {
	out string
	err error
}

var (
	versionOnce sync.Once
	versionGot  versionResult
)

// versionRun builds the workspace and runs it ONCE, and hands every caller the
// same output. Every wait in this file is bounded by `go test`'s own deadline
// and cargo's: there is no loop here that can fail to end.
func versionRun(t *testing.T) string {
	t.Helper()
	corpus := versionCorpus(t)
	versionOnce.Do(func() { versionGot = versionBuildAndRun(t, corpus) })
	if versionGot.out == "" {
		t.Fatalf("the probe workspace produced no output at all: %v", versionGot.err)
	}
	return versionGot.out
}

func versionBuildAndRun(t *testing.T, corpus string) versionResult {
	t.Helper()
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	serialize, err := filepath.Abs(filepath.Join(repo, "..", "serialize.rs"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(serialize); err != nil {
		t.Fatalf("the Rust runtime is not beside this tree at %s: %v", serialize, err)
	}
	root := filepath.Join(repo, "build", "rust-versioning-probes")
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatal(err)
	}

	probes := versionProbeSet(corpus)
	members := make([]string, 0, len(probes))
	for _, p := range probes {
		versionWriteCrate(t, root, serialize, p)
		members = append(members, fmt.Sprintf("%q", p.name))
	}

	// ONE WORKSPACE, so `serialize-official` and every probe's dependencies
	// compile once and the members build across every core. `debug = 0`
	// because a probe is read through its panic message and never through a
	// debugger, and the debug info is most of the link time.
	ws := fmt.Sprintf(`# Generated by internal/codegen/rusttable/fixedversioning_test.go. Not tracked.
[workspace]
resolver = "3"
members = [%s]

[profile.dev]
debug = 0
`, strings.Join(members, ", "))
	if err := os.WriteFile(filepath.Join(root, "Cargo.toml"), []byte(ws), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("cargo", "test", "--workspace", "--no-fail-fast")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CARGO_TARGET_DIR="+filepath.Join(repo, "build", "rust-versioning-target"))
	out, err := cmd.CombinedOutput()
	return versionResult{out: string(out), err: err}
}

// versionWriteCrate generates the READER's unit with the OLDER units' locked
// entries handed to it as its lineage — oldest first, the reader's own layout
// last — marks the first `retire` of them retired, which is what moves the
// floor, and drops the probe module beside it.
func versionWriteCrate(t *testing.T, root, serialize string, p versionProbe) {
	t.Helper()
	u := versionUnit(t, versionSchema(t, p.reader))
	table := versionRootName(t, u)

	lineage := map[string][]FixedLineageEntry{}
	for i, base := range p.older {
		o := versionUnit(t, versionSchema(t, base))
		for _, st := range ir.TableFixedRoots(o) {
			e, ok := FixedLineageOf(o, st.Name)
			if !ok {
				t.Fatalf("no lineage entry for %s in %s", st.Name, base)
			}
			e.Retired = i < p.retire
			if e.Retired {
				e.Reason = "retired by the test's lock"
			}
			lineage[st.Name] = append(lineage[st.Name], e)
		}
	}

	files, err := rust.Generate(u)
	if err != nil {
		t.Fatalf("%s: rust.Generate: %v", p.name, err)
	}
	tables, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatalf("%s: GenerateLineage: %v", p.name, err)
	}
	maps.Copy(files, tables)
	// The crate root is a generated index, exactly as compiler/target_rust.go
	// builds it, plus the one module this test adds.
	lib, err := rust.Lib(u, Modules(tables))
	if err != nil {
		t.Fatalf("%s: rust.Lib: %v", p.name, err)
	}
	files["lib.rs"] = append(lib, []byte("\n#[cfg(test)]\nmod version_probe;\n")...)

	src := strings.NewReplacer(
		"ROWTYPE", table+"Row",
		"LOADFN", ir.RustSnake(table)+"_fixed_load",
	).Replace(p.src)
	files["version_probe.rs"] = []byte(src)

	dir := filepath.Join(root, p.name, "src")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// THE TWO ACCELERATORS ARE OFF, and that is deliberate rather than thrift.
	// The FIXED FORM is a WIRE form and rides unconditionally (rusttable.go):
	// the `<Name>Row` a probe reads into and the `*_fixed.rs` codec that fills
	// it are both outside the `block` and `cook` cfgs, so a probe compiles the
	// whole of what it tests with neither feature on. Leaving them on compiled
	// the BLOCK PROJECTION for every row as well, and the `wstring_grow` row's
	// §19.3 projection size assert is RED on this head for reasons that have
	// nothing to do with §5 — a §19.3 finding that belongs to `make
	// tables-rust-features`, not to a versioning row, and one broken member
	// fails the whole workspace. The feature NAMES stay declared so the
	// generated lib.rs's `cfg(feature = ...)` gates resolve instead of warning.
	manifest := fmt.Sprintf(`# Generated by internal/codegen/rusttable/fixedversioning_test.go. Not tracked.
[package]
name = %q
version = "0.0.0"
edition = "2024"

[features]
default = []
block = []
cook = []

[dependencies]
serialize = { package = "serialize-official", path = %q }
`, p.name, serialize)
	if err := os.WriteFile(filepath.Join(root, p.name, "Cargo.toml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
}

// versionAssert reads ONE probe's verdict out of the one cargo run's output.
// libtest names every test it ran and every test it failed, so a row that is
// in neither list did not run at all — which is what a workspace that did not
// build looks like, and it is a failure and never a pass.
func versionAssert(t *testing.T, out, test string) {
	t.Helper()
	if strings.Contains(out, test+" stdout ----") {
		t.Fatalf("%s FAILED:\n%s", test, versionSection(out, test))
	}
	if !strings.Contains(out, test+" ... ok") {
		t.Fatalf("%s DID NOT RUN — the workspace did not build, or the name moved:\n%s",
			test, versionTail(out))
	}
}

// versionSection is the panic libtest printed under one test's name, and
// nothing else: bounded output by design (the 2026-09-09 ruling), because a
// whole cargo log per failing row is fifty logs nobody reads.
func versionSection(out, test string) string {
	at := strings.Index(out, test+" stdout ----")
	if at < 0 {
		return versionTail(out)
	}
	rest := out[at:]
	// THE PANIC IS BEHIND A BLANK LINE, so the cut is at the NEXT section's own
	// marker and never at the first empty line: cutting there printed the
	// header and threw the message away, and a row whose crate failed to
	// COMPILE printed nothing at all — the one case where the text is the only
	// thing that says what happened.
	if end := strings.Index(rest[len(test):], "\n---- "); end > 0 {
		rest = rest[:len(test)+end]
	}
	return versionClip(rest, 2000)
}

// versionTail is the END of cargo's output, which is where a compile error's
// summary and the test counts are.
func versionTail(out string) string { return versionClip(out, 4000) }

func versionClip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
