package cpptable

// fixedroadmap_t03 — the cpp leg's fixed-closure, retirement, retired-runtime
// and numeric-evolution rows of docs/roadmap.sexp's node `fixed-tables`
// (ROADMAP.md "NEW Fixed Tables"), the assertions no other test of this leg
// makes. Law: docs/FIXED-FORM-ALGORITHM.md (the closure §5.5, the floor §5.2,
// what is retired §5.6, the bounds pass §4.6), docs/FIXED-FORM-VERSIONING-TESTS.md
// ("The floor and the hash", "The old contract's tests, retired by name"),
// docs/SPEC-TABLES.md §3.4, §21.2.
//
// EVERY ASSERTION HERE READS THE CPP THIS LEG EMITS or the shared runtime body
// it emits them with, so the harness runs on a tree that never built the C++
// reference corpus and never carries a sibling runtime checkout; the
// compile-and-run half of this leg's fixed form is fixedversioning_test.go,
// which skips exactly there. The capability subtest compiles the leg's own
// generated header alone — the fixed header takes no serialize dependency —
// so it proves the write/read round trip without the corpus.

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// t03Unit parses and checks one schema source, so a probe can name it without
// a fixture on disk. It fails the test on a parse or check error.
func t03Unit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	u, errs := t03Check(t, src)
	if len(errs) > 0 {
		t.Fatalf("check: %v", errs)
	}
	return u
}

// t03Check parses and checks one source and answers the checker's errors, so a
// closure refusal can be read by name rather than caught as a fixture bug.
func t03Check(t *testing.T, src string) (*ir.Unit, []error) {
	t.Helper()
	ast, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		return nil, []error{perrs[0]}
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: ast,
	}})
	return u, cerrs
}

// t03Header emits the fixed table header of a unit and answers it by name.
func t03Header(t *testing.T, u *ir.Unit) string {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	for name, data := range files {
		if strings.HasSuffix(name, "Table.h") {
			return string(data)
		}
	}
	t.Fatalf("no *Table.h was emitted")
	return ""
}

// t03Has is a required substring of emitted source, named by the rule it proves.
func t03Has(t *testing.T, src, want, what string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Errorf("%s: the emitted source does not carry %q", what, want)
	}
}

// t03RunCapability generates a unit, compiles a probe against the fixed header
// ALONE (the fixed form takes no serialize dependency) and answers its stdout.
func t03RunCapability(t *testing.T, u *ir.Unit, probe string) string {
	t.Helper()
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatalf("cpp.Generate: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.cpp"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	build := exec.Command(cxx, "-std=c++17", "-O0", "-I"+dir, filepath.Join(dir, "probe.cpp"), "-o", filepath.Join(dir, "probe"))
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the capability probe did not compile: %v\n%s", err, out)
	}
	out, err := exec.Command(filepath.Join(dir, "probe")).CombinedOutput()
	if err != nil {
		t.Fatalf("the capability probe failed: %v\n%s", err, out)
	}
	return string(out)
}

// ---- cpp/R6 ----------------------------------------------------------------
//
// cpp/R6: SPEC-TABLES §3.4 and §5.2 — a table whose record body is past the
// 65536-byte ceiling has NO FORM EMITTED, is NAMED by the compiler, and no
// lineage entry handed in for it is parsed even when the lock carries one.
func t03R6(t *testing.T) {
	src := `package probe

fixed table Big
{
    blob bytes(70000)
}
`
	// THE LOCK CARRIES A Big LINEAGE ENTRY: the fixture is written to disk, a
	// lock is produced over it, and the lock is reopened, so the entry the
	// clause names is really present when the generator runs. A by-hand lock
	// would not prove the file path the driver uses.
	dir := t.TempDir()
	path := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := lockfile.Update(loadUnit(t, path), []string{path}); err != nil {
		t.Fatalf("R6: lock: %v", err)
	}
	lock, ok, err := lockfile.Open([]string{path})
	if err != nil || !ok || lock == nil {
		t.Fatalf("R6: Open: ok=%v err=%v", ok, err)
	}
	u := loadUnit(t, path)
	st := u.Tables["Big"]
	if st == nil {
		t.Fatal("R6: the unit declares no Big")
	}
	if ir.TableFixedTypeBytes(st) <= ir.TableFixedRecordMaxBytes {
		t.Fatalf("R6: the fixture body = %d, not past %d", ir.TableFixedTypeBytes(st), ir.TableFixedRecordMaxBytes)
	}
	for _, r := range ir.TableFixedFormRoots(u) {
		if r.Name == "Big" {
			t.Error("R6: a table past the ceiling is a fixed-form root")
		}
	}
	// THE REFUSAL NAMES THE TABLE (docs/FIXED-FORM-ALGORITHM.md §5.9 #8,
	// docs/SPEC-TABLES.md §3.4): the form's refusal is carried BY NAME in the
	// source this leg emits — the same "NO FIXED FORM" sentence the JS leg's
	// module carries (jstable/fixedjs.go) — so a table whose form is not
	// emitted is never silent. It is the FORM's refusal and not the unit's: a
	// declared fixed table past the ceiling still compiles and keeps its CLASS
	// (compiler/fixedrecordsize_test.go).
	// THE LOCK REALLY CARRIES ONE: without this the last clause would pass
	// vacuously, because no lineage entry was ever offered.
	if lin := lockfile.Lineage(lock, "Big"); len(lin) == 0 {
		t.Fatal("R6: the lock carries no Big lineage entry, so the clause is not tested")
	}
	// NO FORM IS EMITTED, and NO LINEAGE ENTRY IS PARSED for Big EVEN THOUGH
	// the lock carries one: the header the generator writes with that lock in
	// hand carries no Big fixed hash or writer, and does carry the named
	// refusal.
	files, err := GenerateLineage(u, lock)
	if err != nil {
		t.Fatalf("R6: GenerateLineage: %v", err)
	}
	named := false
	for name, body := range files {
		text := string(body)
		if strings.Contains(text, "BigFixedHash") || strings.Contains(text, "BigFixedSave") {
			t.Errorf("R6: %s emitted a fixed form for a table past the ceiling, though the lock carries a Big lineage entry", name)
		}
		if strings.Contains(text, "table Big has NO FIXED FORM") && strings.Contains(text, "65536") {
			named = true
		}
	}
	if !named {
		t.Error("R6: the emitted source does not carry the refusal NAMING Big and the 65536-byte ceiling, so a table with no form is silent")
	}
}

// ---- cpp/R14 ---------------------------------------------------------------
//
// cpp/R14: ALGORITHM §5.2's PLAN and §4.6's bounds — the runtime bounds a
// single entry and a subtree to the 65536 a reader caps a record at, the zero
// root to the same name, and — the clause the task adds — every compiled entry
// reaching past the WRITER's own declared record size is refused whole under
// TableMessageReason::layout_record_too_large.
func t03R14(t *testing.T) {
	u := t03Unit(t, `package probe

fixed table T
{
    x int32
    y int32
}
`)
	hdr := t03Header(t, u)
	t03Has(t, hdr, "constexpr uint32_t kTableFixedRecordMaxBytes = 65536u;", "R14 the 65536 ceiling constant")
	t03Has(t, hdr, "if ( e.size > kTableFixedRecordMaxBytes ) { TableFixedFail( c, layout_record_too_large ); return 0; }", "R14 a single entry past the 65536 bound")
	t03Has(t, hdr, "if ( sum > kTableFixedRecordMaxBytes ) { TableFixedFail( c, layout_record_too_large ); return 0; }", "R14 a subtree's sum past the 65536 bound")
	t03Has(t, hdr, "if ( root.size == 0u || root.size > kTableFixedRecordMaxBytes ) { why = layout_record_too_large; return false; }", "R14 the zero root and the 65536 bound")
	t03Has(t, hdr, "c.record = TableFixedEntryAt( theirs, 0 ).size;", "R14 the writer's own declared record is the compiler's bound")
	t03Has(t, hdr, "if ( c.hostile && report != NULL ) { report->reason = layout_record_too_large; }", "R14 an entry reaching past the writer's record is named layout_record_too_large")
	t03Has(t, hdr, "report->reason = ( made == -2 ) ? TableMessageReason::layout_record_too_large : TableMessageReason::plan_too_large;", "R14 the plan is refused WHOLE")
}

// ---- cpp/R22 ---------------------------------------------------------------
//
// cpp/R22: SPEC-TABLES §2.2 and ALGORITHM §5.5 — a table or type reached by
// value from a fixed table is itself fixed; a pointer, map or unbounded array
// in that closure is a compile refusal naming the construct; T is never in its
// own closure.
func t03R22(t *testing.T) {
	fixed := t03Unit(t, `package probe

fixed table Inner { a int32 }
fixed table T { inner Inner }
`)
	if breaks := ir.FixedClosureBreaks(func(name string) *ir.Struct { return fixed.Tables[name] }, "T"); len(breaks) != 0 {
		t.Errorf("R22: a fixed table nested by value is a closure break: %v", breaks)
	}
	for _, tc := range []struct{ name, src, want string }{
		{"plain table", "package probe\ntable Node { x int32 }\nfixed table T { node Node }\n", "holds the plain table Node by value"},
		{"pointer", "package probe\ntable Node { x int32 }\nfixed table Scene { head *Node }\n", "is a pointer"},
		{"map", "package probe\nfixed table Fleet { ships map[uint32]int32 }\n", "is a map"},
		{"unbounded array", "package probe\nfixed table Log { entries []int32 }\n", "is an unbounded array"},
	} {
		_, errs := t03Check(t, tc.src)
		if len(errs) == 0 {
			t.Errorf("R22 %s: the closure break is not a compile refusal", tc.name)
			continue
		}
		got := errs[0].Error()
		if !strings.Contains(got, tc.want) || !strings.Contains(got, "docs/SPEC-TABLES.md") {
			t.Errorf("R22 %s: refusal = %q, want it to name %q and §2.2", tc.name, got, tc.want)
		}
	}
	// T is never in its own closure: a fixed table that reaches itself by value
	// is refused, so a table cannot be its own closure member.
	if _, errs := t03Check(t, "package probe\nfixed table T { next T }\n"); len(errs) == 0 {
		t.Error("R22: a fixed table that reaches itself by value is not refused")
	}
}

// ---- cpp/R3 ----------------------------------------------------------------
//
// cpp/R3: the floor is 1 + the highest retired index (0 when none); below the
// floor is TableMessageReason::layout_unsupported, reporting the file's hash. The emitted floor
// constant is pinned here at 0 (nothing retired) and 1 (the VNEW_floor fixture,
// whose one retired entry is fixtureFloor), and the refusal's shape is read by
// name off the emitted load.
func t03R3(t *testing.T) {
	present := t03Unit(t, `package probe

fixed table T
{
    x int32
}
`)
	hdr := t03Header(t, present)
	t03Has(t, hdr, "constexpr int32_t TFixedFloor = 0;", "R3 no retired entry leaves the floor 0")

	floor := t03Header(t, loadUnit(t, filepath.Join("..", "..", "..", "test", "tables", "VNEW_floor.schema")))
	t03Has(t, floor, "FixedFloor = 1;", "R3 one retired entry puts the floor at 1 + the highest retired index")
	t03Has(t, floor, "if ( lineage_at < floor_at )", "R3 below the floor is refused")
	t03Has(t, floor, "report->reason = TableMessageReason::layout_unsupported; report->layout_hash = hash;", "R3 TableMessageReason::layout_unsupported reports the file's hash")
}

// ---- cpp/R32 ---------------------------------------------------------------
//
// cpp/R32: retire for real — a retired version is refused by name, once,
// idempotently. The by-name refusal is the emitted load's TableMessageReason::layout_unsupported
// branch on a lineage entry at or below the floor; the once/idempotent half is
// that the refusal returns before the record loop and writes no destination.
func t03R32(t *testing.T) {
	u := t03Unit(t, `package probe

fixed table T
{
    x int32
    y int32
}
`)
	hdr := t03Header(t, u)
	// THE BY-NAME REFUSAL: a retired hash (lineage_at < floor_at) is refused
	// TableMessageReason::layout_unsupported carrying the hash, and the scan returns -1 BEFORE the
	// record loop, so nothing is decoded and nothing is written.
	t03Has(t, hdr, "if ( lineage_at < floor_at )", "R32 a retired entry is below the floor")
	t03Has(t, hdr, "report->refused = true; report->reason = TableMessageReason::layout_unsupported; report->layout_hash = hash; return -1;", "R32 the retired version is refused by name and returns early")
	load := t03LoadBody(hdr)
	// ONCE: the floor is tested a single time in the emitted load.
	at := strings.Index(load, "lineage_at < floor_at")
	if n := strings.Count(load, "lineage_at < floor_at"); n != 1 {
		t.Errorf("R32: the floor is tested %d times in the emitted load, want once", n)
	}
	// AND BEFORE the trusted-layout memcmp and the record loop, so a retired
	// file is named before any byte of it is read or written.
	known := strings.Index(load, "FixedKnown[lineage_at]")
	records := strings.Index(load, "for ( int64_t k = 0; k < n; ++k )")
	if at < 0 || known < 0 || records < 0 || at >= known || known >= records {
		t.Errorf("R32: the floor refusal is not before the layout comparison and the record loop (%d, %d, %d)", at, known, records)
	}
	// IDEMPOTENT: the guarded body is a PURE named refusal — it reads
	// `lineage_at` and `hash` and returns, mutating no counter and writing no
	// destination, so a second load of the same file answers the same name.
	body := load[at:]
	if end := strings.Index(body, "\n    }"); end >= 0 {
		body = body[:end]
	}
	for _, touched := range []string{"values[", "defaults", "report->unknown", "report->widened", "memcpy", "at +="} {
		if strings.Contains(body, touched) {
			t.Errorf("R32: the floor refusal body is not a pure named refusal; it touches %q:\n%s", touched, body)
		}
	}
	if !strings.Contains(body, "return -1;") {
		t.Errorf("R32: the floor refusal body does not return before any plan:\n%s", body)
	}
}

// t03LoadBody is the emitted TOLERANT load's body, the one that walks the
// plan (the plain FixedLoad is the strict wrapper around it, SPEC-TABLES
// §6.5), or the whole header.
func t03LoadBody(hdr string) string {
	i := strings.Index(hdr, "FixedLoadTolerant(")
	if i < 0 {
		return hdr
	}
	return hdr[i:]
}

// ---- cpp/R10 ---------------------------------------------------------------
//
// cpp/R10: ALGORITHM §5.3/§5.6 — the run-time walk of a stranger's layout and
// the recompute of the header's hash are retired. The emitted load takes the
// header's hash as given and never recomputes it, and it parses only the
// TRUSTED known layout, never the file's bytes.
func t03R10(t *testing.T) {
	u := t03Unit(t, `package probe

fixed table T
{
    x int32
    y int32
}
`)
	hdr := t03Header(t, u)
	load := t03LoadBody(hdr)
	t03Has(t, load, "const uint64_t hash = TableFixedGet64( data + kTableFixedHashAt );", "R10 the header's hash is taken as given")
	if strings.Contains(load, "TableFixedHashOf(") {
		t.Error("R10: the load recomputes the header's hash")
	}
	t03Has(t, load, "TableFixedParseLayout( known.layout, known.layout_bytes, parsed, why )", "R10 the layout parsed is the TRUSTED known one")
	if strings.Contains(load, "TableFixedParseLayout( layout,") {
		t.Error("R10: the load walks the stranger's own layout bytes")
	}
}

// ---- cpp/R15 ---------------------------------------------------------------
//
// cpp/R15: ALGORITHM §5.6 and FIXED-FORM-VERSIONING-TESTS.md "retired by name"
// — the four forward-read clamps are retired; each is TableFixedReason::layout_unknown now. An
// unknown hash refuses TableFixedReason::layout_unknown before any record. THE COUNT CLAMP ACROSS
// BOUNDS is the clause the first half proves: the SAME forged count past the
// reader's bound is clamped on the reader's OWN hash (the hostile, known-layout
// half §5.6 keeps) and lands TableFixedReason::layout_unknown under an UNKNOWN hash (the retiree),
// with no counter moved and nothing decoded. The range clamp across versions is
// the same refusal: the load's only bounds pass runs over storage after a plan,
// and no newer file reaches it.
func t03R15(t *testing.T) {
	u := t03Unit(t, `package probe

fixed table T
{
    vals [..8]int32
    pick int32 = 0 | min = 0, max = 100
}
`)
	hdr := t03Header(t, u)
	load := t03LoadBody(hdr)
	t03Has(t, load, "if ( lineage_at < 0 )", "R15 a hash in no lineage is refused")
	t03Has(t, load, "report->refused = true; report->reason = TableFixedReason::layout_unknown; report->layout_hash = hash; return -1;", "R15 an unknown hash is TableFixedReason::layout_unknown, not a forward read")
	// THE FOUR RETIRED CLAMPS: no forward remap of an unknown variant to None
	// survives in the load, and the only `unknown` census is the deprecated
	// field's, counted once per plan after the record loop.
	if strings.Contains(load, "= kTableFixedNone") {
		t.Error("R15: the load remaps an unknown variant to None")
	}
	t03Has(t, hdr, "report->unknown++;", "R15 the deprecated field's census is the remaining drop count")

	// THE LIVE PROOF OF "EACH IS TableFixedReason::layout_unknown NOW": save a lawful file, forge
	// its count word to 12 — past the reader's own bound of 8 — and read it
	// twice. Under the reader's own hash the count past the PLAN's bound refuses
	// count_over_bound by name (the OWNER RULE, docs/SPEC-TABLES.md §3.4); under
	// an unknown header hash the SAME forged count lands layout_newer, no counter
	// moves and the poisoned destination keeps every byte.
	out := t03RunCapability(t, u, `#include <stdio.h>
#include <string.h>
#include "ProbeTable.h"
using namespace probe;
int main()
{
    T v; TReset( v );
    v.vals_count = 8;
    for ( int i = 0; i < 8; ++i ) { v.vals[ i ] = 100 + i; }
    v.pick = 50;

    static uint8_t base[ 4096 ];
    const int64_t written = TFixedSave( &v, 1, base, (int64_t) sizeof( base ) );
    if ( written <= 0 ) { printf( "save failed\n" ); return 1; }

    const int64_t layout = (int64_t) TableFixedGet32( base + kTableFixedHeaderBytes );
    const int64_t body = kTableFixedHeaderBytes + 4 + layout + 8;
    /* THE COUNT'S WIRE OFFSET IS THE PLAN'S src, not the struct's offset: the
       storage member sits after the array while the wire writes the count
       first, and the emitted plan is the one place that maps the two. */
    int64_t count_src = -1;
    for ( int i = 0; i < TFixedPlanCount; ++i )
        if ( TFixedPlan[ i ].op == kTableFixedCount ) { count_src = (int64_t) TFixedPlan[ i ].src; }
    if ( count_src < 0 ) { printf( "no count op in the plan\n" ); return 1; }
    const int64_t count_at = body + count_src;

    static TableFixedEntry plan[ 512 ];
    T back[ 1 ];
    TableReport r;
    int64_t n;

    /* THE HOSTILE, KNOWN-LAYOUT HALF STAYS (§5.6: "every hostile check on a
       KNOWN layout's records stays"): the count forged past the reader's bound
       refuses count_over_bound by name, nothing decoded, no counter moved. */
    static uint8_t hostile[ 4096 ];
    memcpy( hostile, base, (size_t) written );
    TableFixedPut32( hostile + count_at, 12u );
    memset( back, 0, sizeof( back ) );
    TReset( back[ 0 ] );
    memset( &r, 0, sizeof( r ) );
    n = TFixedLoad( back, 1, hostile, written, plan, 512, NULL, &r );
    printf( "hostile n=%lld clamped=%d cob=%d\n", (long long) n, r.clamped, (int) ( r.reason == count_over_bound ) );

    /* THE CROSS-VERSION HALF IS RETIRED: the SAME forged count under an UNKNOWN
       hash — a newer writer's file — lands TableFixedReason::layout_unknown before any record. No
       counter moves and the poisoned destination keeps every byte. */
    static uint8_t newer[ 4096 ];
    memcpy( newer, base, (size_t) written );
    TableFixedPut32( newer + count_at, 12u );
    TableFixedPut64( newer + kTableFixedHashAt, 0x0123456789ABCDEFull );
    memset( back, 0x5A, sizeof( back ) );
    memset( &r, 0, sizeof( r ) );
    n = TFixedLoad( back, 1, newer, written, plan, 512, NULL, &r );
    printf( "newer n=%lld refused=%d newer=%d widened=%d clamped=%d poisoned=%d\n",
            (long long) n, (int) r.refused, (int) ( r.reason == TableFixedReason::layout_unknown ),
            r.widened, r.clamped, (int) ( ( (const uint8_t *) &back[ 0 ] )[ 0 ] == 0x5A ) );
    return 0;
}
`)
	for _, want := range []string{
		"hostile n=-1 clamped=0 cob=1",
		"newer n=-1 refused=1 newer=1 widened=0 clamped=0 poisoned=1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("R15: the probe output %q does not carry %q", strings.TrimSpace(out), want)
		}
	}
}

// ---- cpp/E3/other-required-widens (capability) -----------------------------
//
// cpp/E3/other-required-widens: the leg writes and reads the construct with
// non-default and boundary values. The probe sets the signed-int boundaries,
// saves with the leg's own generated writer, loads back into a fresh value and
// asserts the exact landing — the widen ladder a fixed table rides.
func t03E3OtherWidens(t *testing.T) {
	u := t03Unit(t, `package probe

fixed table Bounds
{
    x int32
    y uint32
}
`)
	out := t03RunCapability(t, u, `#include <stdio.h>
#include "ProbeTable.h"
using namespace probe;
int main()
{
    Bounds v; BoundsReset( v );
    v.x = -2147483647 - 1; // INT_MIN
    v.y = 4294967295u;      // UINT_MAX
    static uint8_t buf[ 256 ];
    const int64_t n = BoundsFixedSave( &v, 1, buf, (int64_t) sizeof( buf ) );
    Bounds back; BoundsReset( back );
    static TableFixedEntry plan[ 64 ];
    TableReport r; memset( &r, 0, sizeof( r ) );
    const int64_t k = BoundsFixedLoad( &back, 1, buf, n, plan, 64, NULL, &r );
    printf( "n=%lld k=%lld x=%lld y=%u clamped=%d widened=%d\n",
            (long long) n, (long long) k, (long long) back.x, back.y, r.clamped, r.widened );
    return 0;
}
`)
	if !strings.Contains(out, "x=-2147483648") || !strings.Contains(out, "y=4294967295") {
		t.Errorf("E3/other-required-widens: the boundaries did not round trip: %s", out)
	}
	if !strings.Contains(out, "clamped=0") || !strings.Contains(out, "widened=0") {
		t.Errorf("E3/other-required-widens: a clean round trip moved a counter: %s", out)
	}
}

// ---- cpp/C8 ----------------------------------------------------------------
//
// cpp/C8: ALGORITHM §4.6 — a fixed-point field's bounds are in VALUE UNITS and
// its storage is raw, so both ends are shifted by F first; a bits(N) clamps to
// 2^N - 1. The emitted clamp shifts a fixed field's bounds and caps a bits(N)
// at the width's own top.
func t03C8(t *testing.T) {
	u := t03Unit(t, `package probe

fixed table F
{
    q fixed(12,4) | min = -8, max = 7
    w bits(12)
}
`)
	hdr := t03Header(t, u)
	if !strings.Contains(hdr, "FFixedClamp") {
		t.Fatal("C8: the unit declares no clamp for its bounded fields")
	}
	// The raw band is the value band shifted by F: -8 << 4 = -128 and 7 << 4 =
	// 112, and the bits(12) top is 4095 = 2^12 - 1.
	clamp := t03ClampBody(hdr, "FFixedClamp")
	t03Has(t, clamp, "-128", "C8 the fixed-point floor is the value floor shifted by F")
	t03Has(t, clamp, "112", "C8 the fixed-point ceiling is the value ceiling shifted by F")
	t03Has(t, clamp, "4095", "C8 a bits(12) clamps to 2^12 - 1")
}

// t03ClampBody is one emitted clamp function's body, or the whole header.
func t03ClampBody(hdr, name string) string {
	i := strings.Index(hdr, name+"(")
	if i < 0 {
		return hdr
	}
	return hdr[i:]
}

// ---- cpp/R20 ---------------------------------------------------------------
//
// cpp/R20: SPEC-TABLES §21.2's landing rules — an added field lands its
// declared default; a deprecated field is dropped and counted once under
// unknown per plan; a narrower integer or float is widened exactly and widened
// counts; a shorter array or string lands with the reader's slack as template
// zeros; an older enum's ordinals are the reader's, the list being a prefix.
// Every clause is read at the emitted site that implements it: the prefill and
// its zeroed source, the census flag, the widen ops, and the ordinal remap.
func t03R20(t *testing.T) {
	u := t03Unit(t, `package probe

fixed table T
{
    x int16
    y int32
}
`)
	hdr := t03Header(t, u)
	runtime := hdr // the shared fixed runtime is emitted whole into every table header
	// AN ADDED FIELD LANDS ITS DECLARED DEFAULT: an older writer's body leaves
	// bytes the reader's own value has no source for, and the load prefills
	// them from a value reset to the reader's declared defaults (§21.2). The
	// prefill's source is `defaults`, zeroed first and then Reset.
	t03Has(t, hdr, "memset( (void *) &defaults, 0, sizeof( defaults ) );", "R20 an added field's holes are prefilled")
	t03Has(t, hdr, "Reset( defaults );", "R20 the prefill source is the reader's declared defaults")
	t03Has(t, hdr, "TableFixedFillRun( fill, fill_count,", "R20 the prefill lands the declared defaults under the plan")
	// A DEPRECATED FIELD IS DROPPED AND COUNTED ONCE UNDER UNKNOWN PER PLAN:
	// each writer's field the reader cannot name adds one unknown, and only the
	// FIRST time it is seen (the census flag is false for later elements).
	t03Has(t, runtime, "if ( !named && census && c.report != NULL ) { c.report->unknown++; }", "R20 a field the reader cannot name is counted once")
	t03Has(t, runtime, "// ONCE PER FIELD PER ENDPOINT (ALG 5.4), so only element 0 censuses.", "R20 the census is once per field per plan")
	t03Has(t, runtime, "census && i == 0", "R20 only an array's element 0 censuses")
	t03Has(t, runtime, "census && k == 0", "R20 only a keyed array's element 0 censuses")
	// A NARROWER INTEGER OR FLOAT IS WIDENED EXACTLY and widened counts: the
	// signed rung extends by the sign bit, the float rung widens f32 into f64,
	// and the batch adds the per-entry tally to the report.
	t03Has(t, runtime, "case kTableFixedWiden:", "R20 a narrower integer widens by an op")
	t03Has(t, runtime, "const uint64_t top = 1ull << ( bits - 1 );", "R20 a signed widen's top bit")
	t03Has(t, runtime, "if ( raw & top ) { raw |= ~( ( top << 1 ) - 1ull ); }", "R20 a signed widen extends exactly by its sign bit")
	t03Has(t, runtime, "case kTableFixedWidenF:", "R20 a narrower float widens by its own op")
	t03Has(t, runtime, "report->widened += widened;", "R20 a widening counts on the report")
	// A SHORTER ARRAY OR STRING LANDS WITH THE READER'S SLACK AS TEMPLATE
	// ZEROS: the prefill's source is zeroed before Reset, so a tail the writer
	// never carried reads as the template's zeros, not as stale storage.
	t03Has(t, hdr, "memset( (void *) &defaults, 0, sizeof( defaults ) );", "R20 the reader's slack starts as template zeros")
	// AN OLDER ENUM'S ORDINALS ARE THE READER'S, the list being a prefix: the
	// ordinal is remapped through the plan's own table (built by NAME, one slot
	// per writer variant), so an appended list maps position for position.
	t03Has(t, runtime, "case kTableFixedOrdinal:", "R20 an older enum's ordinal is remapped")
	t03Has(t, runtime, "if ( raw != 0 && raw <= (uint64_t) table[0] ) { v = table[raw]; }", "R20 the ordinal is resolved through the plan's table")
	t03Has(t, runtime, "map[1 + j] = landed;", "R20 the older enum's ordinals are the reader's")
	t03Has(t, hdr, "TableFixedClampKnownRanges(", "R20 the writer's known ranges are clamped after the walk")
}

// ---- cpp/R21 ---------------------------------------------------------------
//
// cpp/R21: ALGORITHM §4's widenf, under the finite rule (docs/SPEC-TABLES.md
// §3.3, 2026-10-27): every finite float32 widens exactly through the ordinary
// conversion, so the rung is plain, and a NaN never reaches it — the fixed
// read's finite pass refuses one as non_finite_float before the bounds pass,
// the writer refuses one before its first byte, and no NaN surgery is left.
func t03R21(t *testing.T) {
	u := t03Unit(t, `package probe

fixed table T
{
    v float32
    w float64
}
`)
	hdr := t03Header(t, u)
	t03Has(t, hdr, "TABLE_FIXED_INLINE bool TFixedFiniteBody( const T & value )", "R21 the record's finite pass is emitted")
	t03Has(t, hdr, "if ( !TableFloatFinite( value.v ) ) { return false; }", "R21 a float32 is tested")
	t03Has(t, hdr, "if ( !TableFloatFinite( value.w ) ) { return false; }", "R21 a float64 is tested")
	t03Has(t, hdr, "if ( !TFixedFiniteBody( values[k] ) )", "R21 the read runs the finite pass")
	t03Has(t, hdr, "TableNonFiniteFloat( report );", "R21 a NaN or an infinity refuses as non_finite_float")
	t03Has(t, hdr, "for ( int64_t k = 0; k < count; ++k ) { if ( !TFixedFiniteBody( values[k] ) ) { return -1; } }", "R21 the writer refuses one")
	t03Has(t, hdr, "const double d = (double) f;", "R21 widenf is the ordinary conversion")
	if strings.Contains(hdr, "TableWidenF32") {
		t.Error("R21: TableWidenF32 is gone with the NaN it carried")
	}
}

// ---- cpp/R29 ---------------------------------------------------------------
//
// cpp/R29 [owed]: ALGORITHM §5.7's `uint_widen` row and §4.6's bounds pass —
// "the band case: the widening across the 65536 ceiling, and the bounds pass
// clamping to the writer's bounds". The band edge is the u16 top 65535, the
// value just below 2^16 = 65536: widened into a u32 it is ZERO-extended to
// 65535, never sign-extended to 0xFFFFFFFF, and `widened` fires once with
// `clamped` at zero because 65535 is inside the writer's own uint16 set. The
// bounds pass clamps a forged value to the WRITER's declared bound and never
// the reader's wider own (bill §12.5): the same 150 is 100 on the writer's
// 0..100 with one clamp and rides whole at 150 on the reader's 0..200.
//
// THE PROBE RUNS THE EMITTED RUNTIME, not a grep of it. The widen plan is the
// one LOAD builds for a lineage entry — copy, an UNSIGNED widen, copy — run
// through the leg's own TableFixedRun; the bounds pass is the leg's own
// TableFixedClampKnownRanges, the same function the emitted BandFixedLoad calls
// with the WRITER's ranges. The negative control is the reader's wider range,
// so a pass that clamped to the reader's bound would be seen.
func t03R29(t *testing.T) {
	u := t03Unit(t, `package probe

fixed table Band
{
    x uint16 = 0 | min = 0, max = 100
    y uint32
}
`)
	hdr := t03Header(t, u)
	// BAND CARRIES A BOUND, so its reader emits the bounds pass the clause
	// names; without a bounded field nothing here could clamp.
	t03Has(t, hdr, "BandFixedClamp", "R29 the fixture's own bounds pass is emitted")

	out := t03RunCapability(t, u, `#include <stdio.h>
#include <string.h>
#include "ProbeTable.h"
using namespace probe;
int main()
{
    /* THE BAND EDGE (§5.7 uint_widen): an OLD writer's body — lead u32, the
       u16 v = 0xFFFF, trail u32 — read through a plan whose v entry is an
       UNSIGNED widen into the reader's u32. 0xFFFF is the u16 top, the value
       just below the 2^16 = 65536 ceiling. */
    uint8_t body[ 10 ];
    const uint32_t lead = 0xAAAAAAAAu, trail = 0xBBBBBBBBu;
    const uint16_t band = 0xFFFFu;
    memcpy( body + 0, &lead, 4 );
    memcpy( body + 4, &band, 2 );
    memcpy( body + 6, &trail, 4 );

    TableFixedEntry plan[ 3 ];
    memset( plan, 0, sizeof( plan ) );
    plan[ 0 ].src = 0; plan[ 0 ].dst = 0; plan[ 0 ].size = 4; plan[ 0 ].op = kTableFixedCopy;
    plan[ 1 ].src = 4; plan[ 1 ].dst = 4; plan[ 1 ].size = 2; plan[ 1 ].dstsize = 4; plan[ 1 ].op = kTableFixedWiden; plan[ 1 ].sign = 0;
    plan[ 2 ].src = 6; plan[ 2 ].dst = 8; plan[ 2 ].size = 4; plan[ 2 ].op = kTableFixedCopy;

    uint8_t image[ 16 ];
    memset( image, 0, sizeof( image ) );
    TableReport r;
    memset( &r, 0, sizeof( r ) );
    /* guarded == count: every entry here is an unguarded one, the first half
       of a plan, so the loop applies them without a tag test. */
    TableFixedRun( plan, 3, 3, body, image, &r );

    uint32_t out_v;
    memcpy( &out_v, image + 4, 4 );

    /* THE WRITER'S BOUNDS (§4.6, bill §12.5): the pass LOAD calls carries the
       WRITER's declared 0..100; a forged 150 past it clamps and counts once.
       The reader's wider 0..200 is the negative control: it must leave 150. */
    uint8_t writer[ 4 ], reader[ 4 ];
    const uint32_t forged = 150u;
    memcpy( writer, &forged, 4 );
    memcpy( reader, &forged, 4 );
    TableFixedKnownRange wb;
    memset( &wb, 0, sizeof( wb ) );
    wb.dst = 0; wb.width = 4; wb.sgn = 0; wb.lo = 0; wb.hi = 100;
    TableFixedKnownRange rb;
    memset( &rb, 0, sizeof( rb ) );
    rb.dst = 0; rb.width = 4; rb.sgn = 0; rb.lo = 0; rb.hi = 200;
    TableReport rw, rr;
    memset( &rw, 0, sizeof( rw ) );
    memset( &rr, 0, sizeof( rr ) );
    TableFixedClampKnownRanges( &wb, 1, writer, &rw );
    TableFixedClampKnownRanges( &rb, 1, reader, &rr );
    uint32_t held_w, held_r;
    memcpy( &held_w, writer, 4 );
    memcpy( &held_r, reader, 4 );

    printf( "v=%u; widened=%d; widen_clamped=%d; writer=%u; writer_clamped=%d; reader=%u; reader_clamped=%d\n",
            out_v, r.widened, r.clamped, held_w, rw.clamped, held_r, rr.clamped );
    return 0;
}
`)
	// THE LANDED VALUE AND THE TWO COUNTERS: 65535 zero-extended across the
	// band edge, one widened, nothing clamped; then the writer's bound and its
	// one clamp, against the reader's wider range landing 150 whole.
	for _, want := range []string{
		"v=65535",
		"widened=1",
		"widen_clamped=0",
		"writer=100",
		"writer_clamped=1",
		"reader=150",
		"reader_clamped=0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("R29: the probe output %q does not carry %q", strings.TrimSpace(out), want)
		}
	}
}

// ---- the card's test -------------------------------------------------------

func TestFixedRoadmapCppT03PlansVersions(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id    string
		check func(t *testing.T)
	}{
		{id: "cpp/R6", check: t03R6},
		{id: "cpp/R14", check: t03R14},
		{id: "cpp/R22", check: t03R22},
		{id: "cpp/R3", check: t03R3},
		{id: "cpp/R32", check: t03R32},
		{id: "cpp/R10", check: t03R10},
		{id: "cpp/R15", check: t03R15},
		{id: "cpp/E3/other-required-widens", check: t03E3OtherWidens},
		{id: "cpp/C8", check: t03C8},
		{id: "cpp/R20", check: t03R20},
		{id: "cpp/R21", check: t03R21},
		{id: "cpp/R29", check: t03R29},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.check(t)
		})
	}
}
