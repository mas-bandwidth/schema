package cpptable

// fixedroadmap_t08 — the cpp leg's union-guards, plan-execution, interoperability
// and hostile-input rows of docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md
// "NEW Fixed Tables"): the assertions no other test of this leg makes. Law:
// docs/FIXED-FORM-ALGORITHM.md §4.1/§5.8 row 6/§5.9 #19 and §5.7 step 1 (the
// manifest), docs/FIXED-FORM-VERSIONING-TESTS.md (the guard rows, the floor and
// the hash, "the manifest is the wire"), and docs/SPEC-TABLES.md §3.3/§3.4. One
// subtest per task id, and the assertion under each is the clause the task's own
// title states, read off the plan the schema compiler lays down (ir/fixedform.go)
// or off the C++ runtime this leg emits with it (fixedruntime.go).
//
// THE CONFORMANCE ROWS RUN A COMPILED PROBE against the C++ reference's byte
// oracle (build/fixedform-corpus, docs/SPEC-TABLES.md §3.4): the cpp runtime the
// emitter lays down reads and writes the same bytes the reference wrote, so the
// assertion is a byte fact and not a spelling one. The corpus is built by
// `make tables-fixedform-corpus`; where it is absent the test SKIPS exactly the
// way fixedversioning_test.go does (never a green over an unrun row).

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// t08Union is one fixed table whose single field is a union with an arm that
// carries text: the shape W7's clause needs — a guarded entry whose op has an
// argument of its own.
const t08Union = `package t08

type Alpha { s string(8) = "" }
type Beta  { n int32 = 0 }

union Pick
{
    alpha Alpha
    beta  Beta
}

fixed table Root
{
    pick Pick
}
`

// t08Nested is an arm inside an arm: `Outer.i` holds `Inner`, and `Inner.a`
// holds a scalar, so the inner arm's entries answer to both tags.
const t08Nested = `package t08n

type Leaf { n int32 = 0 }

union Inner
{
    a Leaf
}

union Outer
{
    i Inner
    b Leaf
}

fixed table Root
{
    o Outer
}
`

// t08Unit parses and checks one inline schema.
func t08Unit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t08.schema")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return loadUnit(t, path)
}

// t08Root is the fixture's one fixed root. The walk here follows cppFixedRoot:
// the outer fixed table no other fixed table of the unit names by value.
func t08Root(t *testing.T, u *ir.Unit) *ir.Struct {
	t.Helper()
	roots := ir.TableFixedRoots(u)
	if len(roots) == 0 {
		t.Fatal("t08: the fixture declares no fixed root")
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

// t08Emit is [Generate]'s source for a unit, all files joined, so a clause can
// read any emitted spelling without knowing which file carries it.
func t08Emit(t *testing.T, u *ir.Unit) string {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var b strings.Builder
	for name, data := range files {
		fmt.Fprintf(&b, "// ---- %s ----\n", name)
		b.Write(data)
	}
	return b.String()
}

// t08EntryStruct is the emitted `struct TableFixedEntry` body and nothing else:
// its declarations ARE the plan entry `struct Op` of docs/FIXED-FORM-ALGORITHM.md
// §4.1 (the plan), so a clause about a lane this entry carries reads this slice.
// Reading the whole runtime would let a neighbouring declaration (TableFixedDst's
// meta, TableFixedCompiler's argw) satisfy the spelling, which is exactly the
// regression "arg and meta ARE TWO LANES" (line 253) and the guard's own width
// (line 244) exist to catch.
func t08EntryStruct(t *testing.T) string {
	t.Helper()
	const open = "struct TableFixedEntry"
	i := strings.Index(tableFixedRuntime, open)
	if i < 0 {
		t.Fatalf("the emitted runtime carries no %q", open)
	}
	rest := tableFixedRuntime[i:]
	j := strings.Index(rest, "};")
	if j < 0 {
		t.Fatalf("the emitted %q struct is not closed", open)
	}
	return rest[:j]
}

func TestFixedRoadmapCppT08WritesConformance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"cpp/W7", t08W7},
		{"cpp/W16", t08W16},
		{"cpp/R28", t08R28},
		{"cpp/W6", t08W6},
		{"cpp/P1", t08P1},
		{"cpp/W13", t08W13},
		{"cpp/P2", t08P2},
		{"cpp/R27", t08R27},
		{"cpp/P3", t08P3},
		{"cpp/P4", t08P4},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t08W7: cpp/W7 "arg and meta are two lanes". A guarded entry's Arg is the
// guard's tag and nothing else, while Meta is the op's own argument (a text
// entry's flavour); §5.8 row 6's second lane and the ir/fixedform.go comment
// that "they shared one, and a string(N) under a union's arm then had to be
// either guarded correctly or read with the right flavour and could not be
// both". The fixture's `pick.alpha.s` is exactly that collision: one leaf that
// carries BOTH a guard's ordinal and a text flavour, on two fields.
func t08W7(t *testing.T) {
	u := t08Unit(t, t08Union)
	st := t08Root(t, u)
	plan, _ := ir.TableFixedBuildPlanNormalised(u, st)

	found := false
	for _, e := range plan {
		if e.Guard == ir.TableFixedNoGuard || e.Op != ir.TableFixedOpText {
			continue
		}
		found = true
		if e.Arg == 0 {
			t.Errorf("W7: the guarded text leaf carries no guard ordinal: %+v", e)
		}
		if e.Meta == 0 {
			t.Errorf("W7: the guarded text leaf carries no text flavour on Meta: %+v", e)
		}
		if e.Arg != 1 {
			t.Errorf("W7: the arm's ordinal is %d, want the first arm's 1: %+v", e.Arg, e)
		}
	}
	if !found {
		t.Fatal("W7: no guarded text leaf in the plan; the fixture proves nothing")
	}

	// The runtime lanes the plan is laid into are the two facts kept apart.
	// §4.1: "arg and meta ARE TWO LANES BECAUSE THEY ARE TWO FACTS, and must
	// never share one". The lanes are read off the ENTRY struct, not the whole
	// runtime, where TableFixedDst repeats `uint8_t meta = 0;`.
	entry := t08EntryStruct(t)
	for _, want := range []string{
		"uint64_t arg = 0;",
		"uint8_t meta = 0;",
	} {
		if !strings.Contains(entry, want) {
			t.Errorf("W7: the emitted entry does not carry the lane %q", want)
		}
	}
	if i, j := strings.Index(entry, "uint64_t arg = 0;"), strings.Index(entry, "uint8_t meta = 0;"); i < 0 || j < 0 || j < i {
		t.Error("W7: meta does not stand after arg on the entry")
	}
}

// t08W16: cpp/W16 "nested union answers outer tag". An entry inside an arm
// inside an arm carries the OUTER tag on the second lane as a CONJUNCTION
// (ir/fixedform.go: "an arm nested inside an arm answers to the OUTER tag,
// which is the one that decides whether any of it is there at all"), so
// Guard2/Arg2/ArgW2 name the outer condition while Guard/Arg/ArgW keep the
// inner arm's own selection (§4.1, §5.8 row 6).
func t08W16(t *testing.T) {
	u := t08Unit(t, t08Nested)
	st := t08Root(t, u)
	plan, _ := ir.TableFixedBuildPlanNormalised(u, st)

	found := false
	for _, e := range plan {
		if e.Guard == ir.TableFixedNoGuard || e.Guard2 == ir.TableFixedNoGuard {
			continue
		}
		found = true
		if e.Arg == 0 || e.Arg2 == 0 {
			t.Errorf("W16: a doubly guarded entry is missing an ordinal: %+v", e)
		}
		if e.Guard2 == e.Guard {
			t.Errorf("W16: the outer and inner guards are the same offset %d: %+v", e.Guard2, e)
		}
	}
	if !found {
		t.Fatal("W16: no entry answers to two tags; the nested fixture proves nothing")
	}

	// Both tags are tested, and the second is a conjunction, not a replacement.
	want := "if ( p.guard2 != kTableFixedNoGuard && TableFixedTagAt( src, p.guard2, p.argw2 ) != p.arg2 ) { continue; }"
	if !strings.Contains(tableFixedRuntime, want) {
		t.Errorf("W16: the run loop does not test the outer tag as a conjunction; want %q", want)
	}
}

// t08R28: cpp/R28 "the guard chain: ... outermost first, the only bound the
// layout's 64; an arm inside an arm answers to the OUTER tag". On this leg the
// chain rides as two guard lanes on the entry (guard/arg/argw and
// guard2/arg2/argw2), each compared at its own width, and the walk's one bound
// is the layout's depth cap of 64 (§5.8 row 6; the depth guard refuses a
// stranger's deep chain rather than recursing).
func t08R28(t *testing.T) {
	u := t08Unit(t, t08Nested)
	st := t08Root(t, u)
	plan, _ := ir.TableFixedBuildPlanNormalised(u, st)

	// Every guarded entry carries an OFFSET (guard) plus its tag (arg) and the
	// tag's width (argw); the outer condition is carried on the second lane.
	for _, e := range plan {
		if e.Guard == ir.TableFixedNoGuard {
			continue
		}
		if e.ArgW <= 0 {
			t.Errorf("R28: a guarded entry carries no tag width: %+v", e)
		}
		if e.Guard2 != ir.TableFixedNoGuard && e.ArgW2 <= 0 {
			t.Errorf("R28: a doubly guarded entry carries no outer tag width: %+v", e)
		}
	}
	// The plan entry's aggregate names the guard lanes in the emitted order.
	// §4.1: `argw` is the guard's WIDTH and "the compare is made at it, never on
	// a prefix"; the width is read off the ENTRY struct, not the whole runtime,
	// where TableFixedCompiler repeats `uint8_t argw = 1;`.
	entry := t08EntryStruct(t)
	if !strings.Contains(entry, "uint32_t guard = kTableFixedNoGuard;") {
		t.Error("R28: the entry carries no guard offset lane")
	}
	if !strings.Contains(entry, "uint8_t argw = 1;") {
		t.Error("R28: the entry carries no guard width lane")
	}
	// The tag is compared at its own width, not its low byte.
	if !strings.Contains(tableFixedRuntime, "inline uint64_t TableFixedTagAt( const uint8_t * src, uint32_t guard, uint8_t argw )") {
		t.Error("R28: the tag is not read at its own width")
	}
	// The layout's only depth bound is 64, and the walk refuses past it.
	if !strings.Contains(tableFixedRuntime, "constexpr int32_t kTableFixedMaxDepth = 64;") {
		t.Error("R28: the layout's depth cap is not 64")
	}
	if !strings.Contains(tableFixedRuntime, "if ( c.depth > 64 ) { c.hostile = true; return; }") {
		t.Error("R28: the walk does not refuse a chain past 64")
	}
}

// t08W6: cpp/W6 "plan partition / split". The plan is PARTITIONED — every
// unguarded entry first, then every guarded one — and `guarded` is where the
// second half starts (ir/fixedform.go TableFixedBuildPlan, the runtime's "A
// PLAN IS PARTITIONED"). The run loop walks the unguarded half with no guard
// test at all, then the guarded half with the conjunction.
func t08W6(t *testing.T) {
	u := t08Unit(t, t08Union)
	st := t08Root(t, u)
	plan, guarded := ir.TableFixedBuildPlanNormalised(u, st)

	if guarded <= 0 || guarded >= len(plan) {
		t.Fatalf("W6: the split %d of %d leaves is not a partition of both halves", guarded, len(plan))
	}
	for i, e := range plan {
		unguarded := e.Guard == ir.TableFixedNoGuard
		if unguarded != (i < guarded) {
			t.Fatalf("W6: leaf %d is on the wrong side of the split at %d: guard=%d", i, guarded, e.Guard)
		}
	}

	src := t08Emit(t, u)
	if !strings.Contains(src, "RootFixedPlanGuarded = ") {
		t.Error("W6: the emitted plan does not lay the split down as a constant")
	}
	for _, want := range []string{
		"for ( int32_t i = 0; i < guarded; ++i )",
		"for ( int32_t i = guarded; i < count; ++i )",
	} {
		if !strings.Contains(tableFixedRuntime, want) {
			t.Errorf("W6: the run loop does not carry the partition loop %q", want)
		}
	}
}

// t08P1: cpp/P1 "identity == compiled". Identity is a plan walked by the SAME
// loop as a plan compiled from a stranger's layout (fixedform.go: "Identity is
// a PLAN walked by the same loop"). The emitted load selects the identity plan
// by the file's own hash and the compiled plan by a known-older hash, and both
// reach the ONE TableFixedRun call site — there is no second identity path.
func t08P1(t *testing.T) {
	u := t08Unit(t, t08Union)
	src := t08Emit(t, u)

	if n := strings.Count(src, "TableFixedRun( entries, entry_count, entry_guarded,"); n != 1 {
		t.Fatalf("P1: the load has %d run call sites, want the one shared loop", n)
	}
	for _, want := range []string{
		"const TableFixedEntry * entries = RootFixedPlan;",
		"entries = dest;",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("P1: the load does not reach the shared loop through %q", want)
		}
	}
	// The shared loop is the partition walk, defined once.
	if n := strings.Count(tableFixedRuntime, "inline void TableFixedRun( const TableFixedEntry * plan, int32_t count, int32_t guarded,"); n != 1 {
		t.Fatalf("P1: the runtime defines %d TableFixedRun loops, want one", n)
	}
}

// ---- the conformance harness -------------------------------------------------

// t08Probe generates `units` with both the packet emitter (cpp.Generate) and the
// table emitter (Generate), writes them beside a probe, compiles the probe with
// the sibling serialize runtime on the include path and runs it, answering its
// stdout. `asan` adds AddressSanitizer and UndefinedBehaviorSanitizer and makes
// the first finding abort, which is what P3's sweep needs. A compile or a
// non-zero exit fails the test, so a probe cannot pass by not running.
func t08Probe(t *testing.T, units []*ir.Unit, probe string, asan bool) string {
	t.Helper()
	dir := t.TempDir()
	for _, u := range units {
		files, err := cpp.Generate(u)
		if err != nil {
			t.Fatalf("cpp.Generate: %v", err)
		}
		tables, err := Generate(u)
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		for name, data := range tables {
			files[name] = data
		}
		for name, data := range files {
			if strings.Contains(name, "/") {
				continue
			}
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.cpp"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	serialize, err := filepath.Abs("../../../../serialize")
	if err != nil {
		t.Fatal(err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	args := []string{"-std=c++17", "-O1", "-I" + dir, "-I" + serialize}
	if asan {
		args = append(args, "-fsanitize=address,undefined", "-fno-sanitize-recover=all", "-g")
	}
	args = append(args, filepath.Join(dir, "probe.cpp"), "-o", filepath.Join(dir, "probe"))
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("t08: the probe did not compile: %v\n%s", err, out)
	}
	out, err := exec.Command(filepath.Join(dir, "probe")).CombinedOutput()
	if err != nil {
		t.Fatalf("t08: the probe failed: %v\n%s", err, out)
	}
	return string(out)
}

// t08FX1 is the versioning pair's FX1 unit (test/tables/FX1.schema): the root
// FxRoot whose layout and hash the reference's corpus (build/fixedform-corpus/
// fx1.bin) carries. Its package is tblfx1 and its emitted header FX1Table.h.
func t08FX1(t *testing.T) *ir.Unit {
	t.Helper()
	return loadUnit(t, filepath.Join("..", "..", "..", "test", "tables", "FX1.schema"))
}

// ---- cpp/W13 -----------------------------------------------------------------

// t08W13 holds W13's clause: "layout+hash == C++ reference". The corpus file
// fx1.bin is the C++ reference's byte oracle (docs/SPEC-TABLES.md §3.4: "the
// C++ backend is the REFERENCE for this form, so the reference is what writes
// the bytes"). The file's header carries the reference's layout hash and the
// layout block; the leg's emitted runtime carries FxRootFixedHash and
// FxRootFixedLayout. The clause is that the two are the SAME bytes: the runtime
// this emitter lays down names the reference's exact layout and hash.
func t08W13(t *testing.T) {
	corpus := cppFixedCorpus(t)
	u := t08FX1(t)
	file := filepath.Join(corpus, "fx1.bin")
	probe := fmt.Sprintf(`#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "FX1Table.h"

using namespace tblfx1;

int main( void )
{
    static uint8_t data[1 << 20];
    FILE * f = fopen( %q, "rb" );
    if ( f == NULL ) { printf( "W13: no corpus file\n" ); return 1; }
    const int64_t len = (int64_t) fread( data, 1, sizeof( data ), f );
    fclose( f );
    (void) len;
    const uint64_t h = TableFixedGet64( data + kTableFixedHashAt );
    if ( h != FxRootFixedHash ) { printf( "W13: header hash %%016llx != emitted %%016llx\n", (unsigned long long) h, (unsigned long long) FxRootFixedHash ); return 1; }
    const uint32_t lb = TableFixedGet32( data + kTableFixedHeaderBytes );
    if ( lb != (uint32_t) FxRootFixedLayoutBytes ) { printf( "W13: layout bytes %%u != emitted %%lld\n", lb, (long long) FxRootFixedLayoutBytes ); return 1; }
    if ( memcmp( data + kTableFixedHeaderBytes + 4, FxRootFixedLayout, (size_t) FxRootFixedLayoutBytes ) != 0 ) { printf( "W13: layout bytes differ from the emitted layout\n" ); return 1; }
    printf( "W13 ok hash=%%016llx\n", (unsigned long long) h );
    return 0;
}
`, file)
	out := t08Probe(t, []*ir.Unit{u}, probe, false)
	if !strings.Contains(out, "W13 ok") {
		t.Fatalf("W13: the probe said %q", out)
	}
}

// ---- cpp/P2 ------------------------------------------------------------------

// t08P2 holds P2's clause: "write-read-write byte-identical". The reference's
// fx1.bin is read through the ONE plan-driven load path and saved back; the
// emitted bytes must be the file's own bytes, every field included
// (docs/FIXED-FORM-ALGORITHM.md §5.7's THE WRITE: "a byte a port encodes
// differently is a byte that does not come back").
func t08P2(t *testing.T) {
	corpus := cppFixedCorpus(t)
	u := t08FX1(t)
	file := filepath.Join(corpus, "fx1.bin")
	probe := fmt.Sprintf(`#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "FX1Table.h"

using namespace tblfx1;

int main( void )
{
    static uint8_t data[1 << 20];
    FILE * f = fopen( %q, "rb" );
    if ( f == NULL ) { printf( "P2: no corpus file\n" ); return 1; }
    const int64_t len = (int64_t) fread( data, 1, sizeof( data ), f );
    fclose( f );
    FxRoot values[8];
    static TableFixedEntry plan[4096];
    TableReport r;
    memset( &r, 0, sizeof( r ) );
    const int64_t n = FxRootFixedLoad( values, 8, data, len, plan, 4096, NULL, &r );
    if ( n < 0 ) { printf( "P2: the reference file did not load\n" ); return 1; }
    static uint8_t ob[1 << 20];
    const int64_t m = FxRootFixedSave( values, n, ob, (int64_t) sizeof( ob ) );
    if ( m != len ) { printf( "P2: re-save %%lld != file %%lld\n", (long long) m, (long long) len ); return 1; }
    if ( memcmp( ob, data, (size_t) len ) != 0 ) { printf( "P2: write-read-write differs\n" ); return 1; }
    printf( "P2 ok n=%%lld\n", (long long) n );
    return 0;
}
`, file)
	out := t08Probe(t, []*ir.Unit{u}, probe, false)
	if !strings.Contains(out, "P2 ok") {
		t.Fatalf("P2: the probe said %q", out)
	}
}

// ---- cpp/R27 -----------------------------------------------------------------

// t08R27 holds R27's clause: "the manifest is the wire: build/fixedform-corpus/
// manifest.txt is the single value oracle every leg reads to". docs/
// FIXED-FORM-ALGORITHM.md §5.7 step 1 and §5.9 #32: the dump writes the values
// it put on the wire into the manifest, and a leg asserts THE MANIFEST and never
// reads the dump. The clause here is that the manifest's declared values for
// fx1.bin are the values the leg's runtime READS OUT OF the bytes.
func t08R27(t *testing.T) {
	corpus := cppFixedCorpus(t)
	u := t08FX1(t)
	manifest, err := os.Open(filepath.Join(corpus, "manifest.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer manifest.Close()

	// The manifest's line for fx1.bin, parsed as §5.9 #32 defines it: split the
	// head on spaces and take values= whole to the end of the line.
	want := map[string]int64{}
	found := false
	sc := bufio.NewScanner(manifest)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "file=fx1.bin ") {
			continue
		}
		found = true
		for _, kv := range strings.Split(line[strings.Index(line, "values=")+len("values="):], ",") {
			eq := strings.Index(kv, "=")
			if eq <= 0 {
				continue
			}
			if v, err := strconv.ParseInt(kv[eq+1:], 10, 64); err == nil {
				want[kv[:eq]] = v
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("R27: the manifest carries no line for fx1.bin")
	}
	for _, k := range []string{"r0.keep", "r0.narrow", "r0.nested.a", "r0.label_length", "r0.marks_count", "r1.keep"} {
		if _, ok := want[k]; !ok {
			t.Fatalf("R27: the manifest's fx1.bin line does not declare %s", k)
		}
	}

	file := filepath.Join(corpus, "fx1.bin")
	probe := fmt.Sprintf(`#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "FX1Table.h"

using namespace tblfx1;

int main( void )
{
    static uint8_t data[1 << 20];
    FILE * f = fopen( %q, "rb" );
    if ( f == NULL ) { printf( "R27: no corpus file\n" ); return 1; }
    const int64_t len = (int64_t) fread( data, 1, sizeof( data ), f );
    fclose( f );
    FxRoot values[8];
    static TableFixedEntry plan[4096];
    TableReport r;
    memset( &r, 0, sizeof( r ) );
    const int64_t n = FxRootFixedLoad( values, 8, data, len, plan, 4096, NULL, &r );
    if ( n != 2 ) { printf( "R27: read %%lld records, want 2\n", (long long) n ); return 1; }
    printf( "r0.keep=%%lld\n", (long long) values[0].keep );
    printf( "r0.narrow=%%lld\n", (long long) values[0].narrow );
    printf( "r0.nested.a=%%lld\n", (long long) values[0].nested.a );
    printf( "r0.label_length=%%lld\n", (long long) values[0].label_length );
    printf( "r0.marks_count=%%lld\n", (long long) values[0].marks_count );
    printf( "r1.keep=%%lld\n", (long long) values[1].keep );
    return 0;
}
`, file)
	out := t08Probe(t, []*ir.Unit{u}, probe, false)
	got := map[string]int64{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		eq := strings.Index(line, "=")
		if eq <= 0 {
			continue
		}
		if v, err := strconv.ParseInt(strings.TrimSpace(line[eq+1:]), 10, 64); err == nil {
			got[line[:eq]] = v
		}
	}
	for k, v := range got {
		if want[k] != v {
			t.Errorf("R27: the manifest says %s=%d, the wire carries %d", k, want[k], v)
		}
	}
	if len(got) != 6 {
		t.Fatalf("R27: the probe reported %d of 6 values: %q", len(got), out)
	}
}

// ---- cpp/P3 ------------------------------------------------------------------

// t08P3 holds P3's clause: "hostile bytes, sweep + sanitizer". docs/
// FIXED-FORM-ALGORITHM.md §5.7 and test/tables/fixedform_properties.cpp's P3:
// "every byte of every record, mutated to {00,01,02,7f,80,ff} ... every landed
// field is within its bound or the read is a named refusal". The probe is built
// with AddressSanitizer and UndefinedBehaviorSanitizer, so a walk that steps
// outside a record or lands a bad value aborts and the subtest fails.
func t08P3(t *testing.T) {
	corpus := cppFixedCorpus(t)
	u := t08FX1(t)
	file := filepath.Join(corpus, "fx1.bin")
	probe := fmt.Sprintf(`#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "FX1Table.h"

using namespace tblfx1;

int main( void )
{
    static uint8_t data[1 << 20];
    static uint8_t orig[1 << 20];
    FILE * f = fopen( %q, "rb" );
    if ( f == NULL ) { printf( "P3: no corpus file\n" ); return 1; }
    const int64_t len = (int64_t) fread( data, 1, sizeof( data ), f );
    fclose( f );
    memcpy( orig, data, (size_t) len );
    const uint32_t lb = TableFixedGet32( data + kTableFixedHeaderBytes );
    const int64_t start = kTableFixedHeaderBytes + 4 + (int64_t) lb;
    const int64_t records = ( len - start ) / FxRootFixedRecordBytes;
    if ( records < 1 ) { printf( "P3: no records to sweep\n" ); return 1; }
    const uint8_t poisons[6] = { 0x00, 0x01, 0x02, 0x7f, 0x80, 0xff };
    int64_t loads = 0;
    for ( int64_t k = 0; k < records; ++k )
    {
        const int64_t rec = start + k * FxRootFixedRecordBytes;
        for ( int64_t i = rec + 8; i < rec + FxRootFixedRecordBytes; ++i )
        {
            for ( int p = 0; p < 6; ++p )
            {
                memcpy( data, orig, (size_t) len );
                data[i] = poisons[p];
                FxRoot back[8];
                for ( int q = 0; q < 8; ++q ) { FxRootReset( back[q] ); }
                static TableFixedEntry plan[4096];
                TableReport rr;
                memset( &rr, 0, sizeof( rr ) );
                const int64_t nn = FxRootFixedLoad( back, 8, data, len, plan, 4096, NULL, &rr );
                ++loads;
                if ( nn >= 0 )
                {
                    for ( int64_t q = 0; q < nn; ++q )
                    {
                        if ( back[q].label_length > 8 || back[q].marks_count > 4 || back[q].blob_length > 6 )
                        {
                            printf( "P3: record %%lld byte %%lld poison %%02x landed out of bound\n", (long long) k, (long long) i, poisons[p] );
                            return 1;
                        }
                    }
                }
                else if ( !rr.refused && !rr.malformed )
                {
                    printf( "P3: record %%lld byte %%lld poison %%02x failed without a named refusal\n", (long long) k, (long long) i, poisons[p] );
                    return 1;
                }
            }
        }
    }
    printf( "P3 ok loads=%%lld\n", (long long) loads );
    return 0;
}
`, file)
	out := t08Probe(t, []*ir.Unit{u}, probe, true)
	if !strings.Contains(out, "P3 ok") {
		t.Fatalf("P3: the probe said %q", out)
	}
}

// ---- cpp/P4 ------------------------------------------------------------------

// t08P4 holds P4's clause: "wrong plan goes red". test/tables/fixedform_main.cpp
// case 8 is the rule: "a reader given the WRONG PLAN for a record ... must come
// out wrong. A test that never watched the wrong plan fail never checked the
// right one." Two fixed tables of DIFFERENT layouts are emitted into one header;
// one record is written and the run loop is given table A's plan (which
// reproduces it) and then table B's plan (which must not).
func t08P4(t *testing.T) {
	u := t08Unit(t, `package t08p4

fixed table A
{
    lead  uint32 = 0
    x     int32  = 0
    y     int32  = 0
    trail uint32 = 0
}

fixed table B
{
    p int8 = 0
    q int8 = 0
}
`)
	probe := `#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "t08Table.h"

using namespace t08p4;

int main( void )
{
    A one;
    AReset( one );
    one.lead = 0xAABBCCDDu; one.x = 111; one.y = 222; one.trail = 0x11223344u;
    static uint8_t w[1 << 16];
    const int64_t wn = AFixedSave( &one, 1, w, (int64_t) sizeof( w ) );
    if ( wn < 0 ) { printf( "P4: A did not save\n" ); return 1; }
    const uint8_t * body = w + kTableFixedHeaderBytes + 4 + AFixedLayoutBytes + 8; // the record's own eight-byte hash first

    A good;
    AReset( good );
    TableReport r;
    memset( &r, 0, sizeof( r ) );
    TableFixedRun( AFixedPlan, AFixedPlanCount, AFixedPlanGuarded, body, (uint8_t *) &good, &r );
    const bool right = good.lead == 0xAABBCCDDu && good.x == 111 && good.y == 222 && good.trail == 0x11223344u;
    if ( !right ) { printf( "P4: the RIGHT plan did not reproduce the record\n" ); return 1; }

    A wrong;
    AReset( wrong );
    TableReport r2;
    memset( &r2, 0, sizeof( r2 ) );
    TableFixedRun( BFixedPlan, BFixedPlanCount, BFixedPlanGuarded, body, (uint8_t *) &wrong, &r2 );
    const bool bad = wrong.lead == 0xAABBCCDDu && wrong.x == 111 && wrong.y == 222 && wrong.trail == 0x11223344u;
    if ( bad ) { printf( "P4: the WRONG plan reproduced the record\n" ); return 1; }
    printf( "P4 ok right=%d wrong=%d\n", (int) right, (int) bad );
    return 0;
}
`
	out := t08Probe(t, []*ir.Unit{u}, probe, false)
	if !strings.Contains(out, "P4 ok right=1 wrong=0") {
		t.Fatalf("P4: the probe said %q", out)
	}
}
