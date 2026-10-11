package cpptable

// THE T07 WRITES CARDS ON THE CPP LEG. Every task below is asserted here once,
// in one table-driven test with one subtest per task id, and every assertion
// quotes the page sentence it implements. The clause is read from the roadmap
// node (docs/roadmap.sexp) and the law is docs/FIXED-FORM-ALGORITHM.md §3.1/§3.4
// and docs/SPEC-TABLES.md §3.4.
//
// TWO KINDS OF ASSERTION, both direct:
//
//   - The WRITE and READ clauses (W1, W3, W4, C14) are RUN: a schema is
//     generated into C++, compiled with c++ and a probe runs the generated
//     Save/Load over bytes whose declared slack is poisoned. The failure is a
//     wrong wire byte or a wrong counter, never a guess about the emitter text.
//   - W9's prefill is RUN too: the probe includes the generated runtime and
//     calls TableFixedFills (the subtraction) and TableFixedFillRun over a
//     cover and a plan, so the clause that the runtime's subtraction computes
//     the unwritten ranges only is asserted behaviourally, not by the printed
//     header's shape.
//   - The clauses that ARE an emission rule (W5's write-side check, W8's Dst row
//     convention) are asserted on the printed C++ the emitter produces from a
//     schema, which is the guarded site: a planted edit to the emitter turns the
//     subtest red naming the rule.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
)

// cppT07Run generates `src` under base, compiles a C++ probe whose main() is
// `body`, runs it and returns its combined output. The probe has a one-megabyte
// static `buf` and the generated header already in scope: body is the statements
// of main.
func cppT07Run(t *testing.T, src, base, body string, extra ...string) ([]byte, error) {
	t.Helper()
	dir := t.TempDir()
	sp := filepath.Join(dir, base+".schema")
	if err := os.WriteFile(sp, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, sp)
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	outDir := t.TempDir()
	var header string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(outDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.h") {
			header = name
		}
	}
	if header == "" {
		t.Fatal("no Table.h was generated")
	}
	probe := fmt.Sprintf(`#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "%s"

using namespace %s;

int main( void )
{
    static uint8_t buf[1 << 20];
%s
    return 0;
}
`, header, u.Package, body)
	if err := os.WriteFile(filepath.Join(outDir, "probe.cpp"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	args := append([]string{"-std=c++17", "-O1", "-I" + outDir,
		filepath.Join(outDir, "probe.cpp"), "-o", filepath.Join(outDir, "probe")}, extra...)
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		return out, fmt.Errorf("the probe did not compile: %w", err)
	}
	return exec.Command(filepath.Join(outDir, "probe")).CombinedOutput()
}

// cppT07Header is the printed C++ for `src` under base, which is what the
// emission-rule subtests read. Only the generated header is returned; the
// runtime is part of it.
func cppT07Header(t *testing.T, src, base string) string {
	t.Helper()
	dir := t.TempDir()
	sp := filepath.Join(dir, base+".schema")
	if err := os.WriteFile(sp, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, sp)
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	for name, data := range files {
		if strings.HasSuffix(name, "Table.h") {
			return string(data)
		}
	}
	t.Fatal("no Table.h was generated")
	return ""
}

func TestFixedRoadmapCppT07Writes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"cpp/C14", cppT07C14},
		{"cpp/W4", cppT07W4},
		{"cpp/W9", cppT07W9},
		{"cpp/W1", cppT07W1},
		{"cpp/W3", cppT07W3},
		{"cpp/W5", cppT07W5},
		{"cpp/W8", cppT07W8},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			t.Parallel()
			c.run(t)
		})
	}
}

// ---- CPP/C14 bounds pass walks live only -----------------------------------
//
// docs/roadmap.sexp cpp/C14, "bounds pass walks live only"; the law is the
// generated read-side pass's own rule (internal/codegen/cpptable/fixedform.go,
// the comment above emitFixedClamp: "It walks only what a read can have written
// — a counted array's LIVE elements and never its slack"). The wire is written
// with two live elements, the second out of range, and the two unused slots
// overlaid with 0x5A5A5A5A, an out-of-range value no writer wrote. The read's
// clamp counter must be EXACTLY one: the live element, and never the slack.
func cppT07C14(t *testing.T) {
	src := `package c14
fixed table C14 {
    n    int32 = 0 | min = 0, max = 10
    xs   [..4]int32 | min = 0, max = 10
    tail int32 = 4
}
`
	body := `
    C14 one;
    C14Reset( one );
    one.n = 5;
    one.xs[0] = 5;
    one.xs[1] = 999; // the ONLY live value out of [0,10]
    one.xs_count = 2;
    one.tail = 4;
    const int64_t need = C14FixedMeasure( 1 );
    C14FixedSave( &one, 1, buf, need );
    const uint8_t * b = buf + kTableFixedHeaderBytes + 4 + C14FixedLayoutBytes + 8;
    // xs: count at body+4, elements at body+8 (n is 4 bytes, so xs starts at 4).
    for ( int i = 8 + 2 * 4; i < 8 + 4 * 4; ++i ) { ((uint8_t *) b)[i] = 0x5A; }
    C14 back;
    C14Reset( back );
    TableReport r;
    memset( &r, 0, sizeof( r ) );
    static TableFixedEntry plan[1024];
    const int64_t n = C14FixedLoad( &back, 1, buf, need, plan, 1024, NULL, &r );
    if ( n != 1 ) { printf( "C14: the record did not read: n=%lld\n", (long long) n ); return 1; }
    if ( r.clamped != 1 )
        { printf( "C14: bounds pass walks live only: the slack's poisoned slots must not count and the one live 999 must: clamped=%d, want exactly 1\n", r.clamped ); return 1; }
    if ( back.xs[1] != 10 )
        { printf( "C14: the live out-of-range value did not clamp: xs[1]=%d\n", back.xs[1] ); return 1; }
    printf( "C14 ok\n" );
`
	out, err := cppT07Run(t, src, "c14", body)
	if err != nil {
		t.Fatalf("C14: %v\n%s", err, out)
	}
}

// ---- CPP/W4 read slack unspecified -----------------------------------------
//
// docs/roadmap.sexp cpp/W4, "read slack unspecified"; docs/SPEC-TABLES.md §3.4
// and the read pass's rule (fixedform.go: "a counted array's LIVE elements and
// never its slack"). A peer leaves non-zero garbage in the declared slack of a
// text field and of a counted array; the read must succeed, move NO counter and
// name no refusal, while the live prefix reads and the field past the slack
// stands. The first read of the same writer's own file proves the garbage is
// the only difference and that the wire really carries it.
func cppT07W4(t *testing.T) {
	src := `package w4
fixed table W4 {
    label string(8)
    marks [..4]int32 | min = 0, max = 10
    tail  int32 = 42
}
`
	body := `
    W4 one;
    W4Reset( one );
    one.label[0] = 'h'; one.label[1] = 'i'; one.label_length = 2;
    one.marks[0] = 7; one.marks_count = 1;
    one.tail = 42;
    const int64_t need = W4FixedMeasure( 1 );
    W4FixedSave( &one, 1, buf, need );
    const uint8_t * b = buf + kTableFixedHeaderBytes + 4 + W4FixedLayoutBytes + 8;
    // label: length(4) + 8 bytes at body 0..12; marks: count(4) + 16 at 12..32; tail 32..36.
    for ( int i = 4 + 2; i < 4 + 8; ++i ) { ((uint8_t *) b)[i] = 0xAA; }
    for ( int i = 12 + 4 + 4; i < 12 + 4 + 16; ++i ) { ((uint8_t *) b)[i] = 0x5A; }
    // NOT VACUOUS: the wire really carries non-zero garbage in the declared slack.
    if ( b[4 + 2] == 0 || b[12 + 4 + 4] == 0 ) { printf( "W4: the wire slack was not poisoned\n" ); return 1; }
    W4 back;
    W4Reset( back );
    TableReport r;
    memset( &r, 0, sizeof( r ) );
    static TableFixedEntry plan[256];
    const int64_t n = W4FixedLoad( &back, 1, buf, need, plan, 256, NULL, &r );
    if ( n != 1 ) { printf( "W4: a peer's garbage in the slack refused the record: n=%lld\n", (long long) n ); return 1; }
    if ( r.clamped || r.unknown || r.kind_mismatch || r.duplicate || r.malformed || r.refused )
        { printf( "W4: read slack unspecified: garbage in declared slack moved a counter: c=%d u=%d km=%d d=%d m=%d x=%d\n", r.clamped, r.unknown, r.kind_mismatch, r.duplicate, (int) r.malformed, (int) r.refused ); return 1; }
    if ( back.label_length != 2 || back.label[0] != 'h' || back.label[1] != 'i' )
        { printf( "W4: the used text did not read\n" ); return 1; }
    if ( back.marks_count != 1 || back.marks[0] != 7 )
        { printf( "W4: the live prefix did not read\n" ); return 1; }
    if ( back.tail != 42 )
        { printf( "W4: the field past the slack moved: %d\n", back.tail ); return 1; }
    printf( "W4 ok\n" );
`
	out, err := cppT07Run(t, src, "w4", body)
	if err != nil {
		t.Fatalf("W4: %v\n%s", err, out)
	}
}

// ---- CPP/W1 write slack is template zeros ----------------------------------
//
// docs/roadmap.sexp cpp/W1, "write slack is template zeros"; docs/SPEC-TABLES.md
// §3.4 ("the slack is zero") and the writer's own rule (fixedform.go's
// emitFixedWriteText and the Save loop's `memset( at + 8, 0, ...BodyBytes )`).
// The destination buffer is stained 0x5A before the save: a writer that copied
// storage instead of the template would carry the stain onto the declared slack
// of a partly-used text and a partly-filled counted array. The live bytes must
// still be the writer's.
func cppT07W1(t *testing.T) {
	src := `package w1
fixed table W1 {
    note  string(12)
    marks [..4]int32
}
`
	body := `
    W1 one;
    W1Reset( one );
    one.note[0] = 'h'; one.note[1] = 'i'; one.note_length = 2;
    one.marks[0] = 7; one.marks_count = 1;
    const int64_t need = W1FixedMeasure( 1 );
    for ( int64_t i = 0; i < need; ++i ) { buf[i] = 0x5A; }
    if ( W1FixedSave( &one, 1, buf, need ) != need ) { printf( "W1: the save did not write the measure\n" ); return 1; }
    const uint8_t * b = buf + kTableFixedHeaderBytes + 4 + W1FixedLayoutBytes + 8;
    // note: length(4) + 12 at body 0..16, so the slack is body 6..16.
    for ( int i = 4 + 2; i < 4 + 12; ++i )
        if ( b[i] != 0 ) { printf( "W1: write slack is template zeros: note body[%d]=%#x, the 0x5A stain reached the wire\n", i, b[i] ); return 1; }
    // marks: count(4) + 16 at body 16..36, so the slack is body 24..36.
    for ( int i = 16 + 4 + 4; i < 16 + 4 + 16; ++i )
        if ( b[i] != 0 ) { printf( "W1: write slack is template zeros: marks body[%d]=%#x, the 0x5A stain reached the wire\n", i, b[i] ); return 1; }
    if ( b[4] != 'h' || b[5] != 'i' ) { printf( "W1: the used text did not land\n" ); return 1; }
    if ( b[16] != 1 || b[20] != 7 ) { printf( "W1: the live count/element did not land\n" ); return 1; }
    printf( "W1 ok\n" );
`
	out, err := cppT07Run(t, src, "w1", body)
	if err != nil {
		t.Fatalf("W1: %v\n%s", err, out)
	}
}

// ---- CPP/W3 zero behind a narrower arm -------------------------------------
//
// docs/roadmap.sexp cpp/W3, "zero behind a narrower arm"; docs/FIXED-FORM-
// ALGORITHM.md §3.1 ("A union writes the tag and the taken arm only, zero behind
// a narrower one") and docs/SPEC-TABLES.md §3.4 ("ZERO on write behind a
// narrower arm, IGNORED on read, never a refusal"). The union's slot is a
// one-byte tag then the widest arm, an int64, so NarrowArm's single byte is
// followed by seven bytes of declared slack. The output is stained 0x5A. The
// control writes the SAME slot on the wide arm and fills those bytes, so the
// zero is a fact about the narrow arm and not a slot that is universally zero.
func cppT07W3(t *testing.T) {
	src := `package w3
type NarrowArm { a int8 = 0 }
type WideArm { b int64 = 0 }
union Pick {
    narrow NarrowArm
    wide   WideArm
}
fixed table W3 {
    pick Pick
    tail int32 = 7
}
`
	body := `
    W3 one;
    W3Reset( one );
    one.pick.type = PickType::Narrow;
    one.pick.narrow.a = 0x7E;
    one.tail = 7;
    const int64_t need = W3FixedMeasure( 1 );
    for ( int64_t i = 0; i < need; ++i ) { buf[i] = 0x5A; }
    W3FixedSave( &one, 1, buf, need );
    const uint8_t * b = buf + kTableFixedHeaderBytes + 4 + W3FixedLayoutBytes + 8;
    if ( b[0] != (uint8_t) one.pick.type ) { printf( "W3: the tag is %d, want %d\n", b[0], (int) one.pick.type ); return 1; }
    if ( b[1] != 0x7E ) { printf( "W3: the narrow arm's live byte is %#x, not %#x\n", b[1], 0x7E ); return 1; }
    for ( int i = 2; i < 9; ++i )
        if ( b[i] != 0 ) { printf( "W3: zero behind a narrower arm: slack body[%d]=%#x; the 0x5A stain reached the wire\n", i, b[i] ); return 1; }
    if ( b[9] != 7 ) { printf( "W3: the field past the union moved\n" ); return 1; }
    W3 wide;
    W3Reset( wide );
    wide.pick.type = PickType::Wide;
    wide.pick.wide.b = 0x0102030405060708LL;
    wide.tail = 7;
    for ( int64_t i = 0; i < need; ++i ) { buf[i] = 0x5A; }
    W3FixedSave( &wide, 1, buf, need );
    b = buf + kTableFixedHeaderBytes + 4 + W3FixedLayoutBytes + 8;
    if ( b[0] != (uint8_t) wide.pick.type ) { printf( "W3: the wide tag is %d\n", b[0] ); return 1; }
    for ( int i = 1; i < 9; ++i )
        if ( b[i] == 0 && i != 1 ) { printf( "W3: NEGATIVE CONTROL: the wide arm did not fill body[%d], so the zero above is not the narrow arm's\n", i ); return 1; }
    printf( "W3 ok\n" );
`
	out, err := cppT07Run(t, src, "w3", body)
	if err != nil {
		t.Fatalf("W3: %v\n%s", err, out)
	}
}

// ---- CPP/W5 write checks DEBUG only ----------------------------------------
//
// docs/roadmap.sexp cpp/W5, "write checks DEBUG only"; docs/FIXED-FORM-
// ALGORITHM.md §3.1 ("Write-side bound checks are DEBUG ONLY, by rule. `count <=
// Max` and `length <= N` are a caller contract"). The generated write body
// carries `schema_assert( ... <= N )` and the runtime defines schema_assert as
// `assert` behind `#ifndef schema_assert`, so NDEBUG — and a caller that defines
// its own schema_assert first — removes it: a release build pays nothing. The
// read side's pass is the unconditional one and it counts, so it is what keeps
// the wire safe.
func cppT07W5(t *testing.T) {
	src := `package w5
fixed table W5 {
    marks [..4]int32
    name  string(8)
    tail  int32 = 7
}
`
	h := cppT07Header(t, src, "w5")
	write := []string{
		"schema_assert( value.marks_count >= 0 && value.marks_count <= 4 ); // the declared count is the bound (§3.4)",
		"schema_assert( value.name_length >= 0 && value.name_length <= 8 ); // the declared length is the bound (§3.4)",
	}
	for _, want := range write {
		if !strings.Contains(h, want) {
			t.Errorf("W5: write checks DEBUG only: the write body must carry the DEBUG-only caller contract %q", want)
		}
	}
	if !strings.Contains(h, "#define schema_assert assert") {
		t.Errorf("W5: write checks DEBUG only: schema_assert must be defined as assert behind #ifndef so NDEBUG removes it")
	}
	// The read side is the unconditional pass, and it walks the live count.
	if !strings.Contains(h, "for ( int64_t i = 0; i < (int64_t) value.marks_count; ++i )") {
		t.Errorf("W5: the read-side pass over the live count must be unconditional")
	}
}

// ---- CPP/W8 bytes(N) takes the array row ------------------------------------
//
// docs/roadmap.sexp cpp/W8, "bytes(N) takes the array row"; docs/SPEC-TABLES.md
// §3.4, the `bytes(N)` row: it is an ARRAY of u8 on this wire, so its
// destination row is an ARRAY's — the buffer, with a stride of one and a live
// count beside it — and NOT a text field's, which is the other way round. The
// generated ProbeFixedDst prints both, and the two lines are the assertion: the
// blob row is `{ offsetof(Probe, blob), 1, offsetof(Probe, blob_length), 1, 3 }`
// and the label row, as the CONTROL, is the text convention.
func cppT07W8(t *testing.T) {
	src := `package w8
fixed table Probe {
    label string(8)
    blob  bytes(6)
    marks [..4]int32
}
`
	h := cppT07Header(t, src, "w8")
	blob := "{ (uint32_t) __builtin_offsetof( Probe, blob ), 1, (uint32_t) __builtin_offsetof( Probe, blob_length ), 1, 3 }, // blob"
	label := "{ (uint32_t) __builtin_offsetof( Probe, label_length ), 0, (uint32_t) __builtin_offsetof( Probe, label ), 0, 1 }, // label"
	if !strings.Contains(h, blob) {
		t.Errorf("W8: bytes(N) takes the array row: the bytes destination row must be the array's — buffer at dst, stride one, live count at aux — got no line %q", blob)
	}
	if !strings.Contains(h, label) {
		t.Errorf("W8: bytes(N) takes the array row: the text row is the CONTROL and must stay the text convention %q", label)
	}
}

// ---- CPP/W9 prefill unwritten ranges only ----------------------------------
//
// docs/roadmap.sexp cpp/W9, "prefill unwritten ranges only"; docs/FIXED-FORM-
// ALGORITHM.md §5 and the runtime's own rule (fixedruntime.go, the comment above
// TableFixedFillRun: the prefill "answers it for EXACTLY THE BYTES THE PLAN DOES
// NOT LAND"). The prefill is a LIST OF RANGES, computed as the type's value
// bytes (the <Root>FixedCover) MINUS every byte the plan lands, and the identity
// plan's list is the empty limit case. The two functions are the guarded sites
// and they are RUN here, in the generated runtime, against a cover and a plan
// hand-built beside the generated code: TableFixedFills must return the cover's
// ranges MINUS the landed runs and no others, and TableFixedFillRun must copy
// the declared defaults into exactly those ranges, leaving a landed byte alone.
// The destination is stained 0x5A and the defaults 0x11, so a broken
// subtraction (a whole cover, an empty list, or a mislaid edge) is a wrong
// range or a wrong byte, never a guess about the emitted text.
func cppT07W9(t *testing.T) {
	src := `package w9
fixed table W9 {
    note  string(12)
    marks [..4]int32
}
`
	body := `
    // A COVER of two value ranges, and a plan that lands a run inside each:
    // [0,4) of the first and [18,20) of the second. The subtraction must
    // answer the three holes and nothing else.
    TableFixedFill cover[2];
    cover[0].dst = 0;  cover[0].size = 8;
    cover[1].dst = 16; cover[1].size = 8;
    static TableFixedEntry plan[2];
    memset( (void *) plan, 0, sizeof( plan ) );
    plan[0].op = kTableFixedCopy; plan[0].dst = 0;  plan[0].size = 4;
    plan[1].op = kTableFixedCopy; plan[1].dst = 18; plan[1].size = 2;
    TableFixedFill fill[8];
    const int32_t fill_count = TableFixedFills( plan, 2, cover, 2, fill );
    if ( fill_count != 3 )
        { printf( "W9: the subtraction must answer the three holes and nothing else: fill_count=%d\n", fill_count ); return 1; }
    const uint32_t want_dst[3] = { 4, 16, 20 };
    const uint32_t want_size[3] = { 4, 2, 4 };
    for ( int i = 0; i < 3; ++i )
        if ( fill[i].dst != want_dst[i] || fill[i].size != want_size[i] )
            { printf( "W9: prefill unwritten ranges only: fill[%d]={%u,%u}, want {%u,%u}\n", i, fill[i].dst, fill[i].size, want_dst[i], want_size[i] ); return 1; }
    // TableFixedFillRun copies the defaults into exactly those ranges.
    uint8_t defaults[32];
    uint8_t dst[32];
    memset( defaults, 0x11, sizeof( defaults ) );
    memset( dst, 0x5A, sizeof( dst ) );
    TableFixedFillRun( fill, fill_count, defaults, dst );
    for ( int i = 4; i < 8; ++i )  if ( dst[i] != 0x11 ) { printf( "W9: the unwritten byte %d took %#x, want the default 0x11\n", i, dst[i] ); return 1; }
    for ( int i = 16; i < 18; ++i ) if ( dst[i] != 0x11 ) { printf( "W9: the unwritten byte %d took %#x, want the default 0x11\n", i, dst[i] ); return 1; }
    for ( int i = 20; i < 24; ++i ) if ( dst[i] != 0x11 ) { printf( "W9: the unwritten byte %d took %#x, want the default 0x11\n", i, dst[i] ); return 1; }
    // A LANDED byte is not in the list, so the prefill left the stain: proof
    // the copy is cover MINUS landed and not the whole cover.
    for ( int i = 0; i < 4; ++i )  if ( dst[i] != 0x5A ) { printf( "W9: a landed byte %d was prefilled: %#x, want the 0x5A stain left alone\n", i, dst[i] ); return 1; }
    for ( int i = 18; i < 20; ++i ) if ( dst[i] != 0x5A ) { printf( "W9: a landed byte %d was prefilled: %#x, want the 0x5A stain left alone\n", i, dst[i] ); return 1; }
    // THE IDENTITY LIMIT CASE: a plan that lands the whole cover leaves nothing
    // to subtract, so the list is empty and no byte is prefilled.
    static TableFixedEntry whole[2];
    memset( (void *) whole, 0, sizeof( whole ) );
    whole[0].op = kTableFixedCopy; whole[0].dst = 0;  whole[0].size = 8;
    whole[1].op = kTableFixedCopy; whole[1].dst = 16; whole[1].size = 8;
    TableFixedFill empty[8];
    const int32_t empty_count = TableFixedFills( whole, 2, cover, 2, empty );
    if ( empty_count != 0 )
        { printf( "W9: the identity plan's list is empty: empty_count=%d\n", empty_count ); return 1; }
    printf( "W9 ok\n" );
`
	out, err := cppT07Run(t, src, "w9", body)
	if err != nil {
		t.Fatalf("W9: %v\n%s", err, out)
	}
}
