package ctable

// TestFixedRoadmapCT11Optionals is the c leg's card schema-c-t11-optionals:
// rows optional-scalars, optional-nesting, optional-arrays, scalar-defaults and
// text-bytes-flags-defaults of docs/roadmap.sexp's node `fixed-tables`, in
// roadmap order. One subtest per task id, table-driven, t.Parallel() first.
//
// Each task is [capability]: "the leg writes and reads the construct with
// non-default and boundary values, asserted, in a target a gate runs"
// (docs/SPEC-TABLES.md §2.3 for `?T` and `?[N]T`, §3.4 for the fixed record and
// the template's declared defaults).
//
// THE HARNESS IS THIS LEG'S OWN FIXED-FORM ONE (fixedversioning_test.go): a
// schema is generated to C, compiled with the generated header and RUN, and the
// body here drives the generated surface — `<snake>_reset`,
// `<snake>_fixed_save` and `<snake>_fixed_load` — exactly as
// cRunVersionProbe drives the same surface for a corpus read. Nothing outside
// this package's helpers is added.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedRoadmapCT11Optionals(t *testing.T) {
	t.Parallel()
	tasks := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"optional-scalars/c/valid-data", cT11OptionalScalars},
		{"optional-nesting/c/valid-data", cT11OptionalNesting},
		{"optional-arrays/c/valid-data", cT11OptionalArrays},
		{"scalar-defaults/c/valid-data", cT11ScalarDefaults},
		{"text-bytes-flags-defaults/c/valid-data", cT11TextBytesFlagsDefaults},
	}
	for _, task := range tasks {
		t.Run(task.id, func(t *testing.T) {
			t.Parallel()
			task.run(t)
		})
	}
}

// cT11Run generates `src` to C, compiles the generated unit with a probe main
// whose body is `body`, runs it and returns the combined output. It is the
// cRunVersionProbe shape with the corpus file and the load scaffolding removed:
// the body owns the whole run.
func cT11Run(t *testing.T, src, body string) ([]byte, error) {
	t.Helper()
	u := cUnitOf(t, src)
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	var header string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.h") {
			header = name
		}
	}
	if header == "" {
		t.Fatal("no Table.h was generated")
	}
	root := cFixedRootName(t, u)
	snake := ir.RustSnake(root)
	probe := fmt.Sprintf(`#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include "%s"

int main( void )
{
    static TableFixedEntry plan[4096];
    (void) plan;
%s
    return 0;
}
`, header, strings.NewReplacer("@SNAKE@", snake, "@ROOT@", root).Replace(body))
	if err := os.WriteFile(filepath.Join(dir, "probe.c"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	serialize, err := filepath.Abs("../../../../serialize.c")
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"-std=c99", "-O1", "-I" + dir, "-I" + serialize}, gccFortifySilence(cc)...)
	args = append(args, filepath.Join(dir, "probe.c"), "-o", filepath.Join(dir, "probe"), "-lm")
	build := exec.Command(cc, args...)
	if out, err := build.CombinedOutput(); err != nil {
		return out, fmt.Errorf("the probe did not compile: %w", err)
	}
	return exec.Command(filepath.Join(dir, "probe")).CombinedOutput()
}

func cT11Probe(t *testing.T, src, body string) {
	t.Helper()
	out, err := cT11Run(t, src, body)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// THE FIVE SCHEMAS. Each is small and carries the construct the task names,
// plus a leading and trailing scalar so a mislaid size moves a neighbour.

const cT11OptionalScalarsSrc = `package t11opt

enum Tier
{
    Bronze
    Silver
    Gold
}

fixed table OptScalar
{
    lead uint32 = 1
    a    ?int32
    t    ?Tier
    tail uint32 = 2
}
`

const cT11OptionalNestingSrc = `package t11nest

fixed table Leaf
{
    x int32  = 7
    y uint32 = 9
}

fixed table OptNest
{
    lead uint32 = 1
    link ?Leaf
    tail uint32 = 2
}
`

const cT11OptionalArraysSrc = `package t11arr

fixed table OptArr
{
    lead uint32 = 1
    vals ?[4]int32
    trail uint32 = 2
}
`

const cT11ScalarDefaultsSrc = `package t11def

enum Mode
{
    Off
    On
    Auto
}

fixed table Defs
{
    a    int32  = 7
    b    uint32 = 9
    m    Mode   = On
    tail uint32 = 3
}
`

const cT11TextBytesFlagsSrc = `package t11txt

flags Fl
{
    Alpha
    Beta
    Gamma
}

fixed table Txt
{
    label string(8) = "hi"
    blob  bytes(4)  = "ab"
    f     Fl        = { Alpha, Gamma }
    tail  uint32    = 3
}
`

// ---- optional-scalars/c/valid-data -----------------------------------------
//
// §2.3: a `?T` is the value beside a presence byte and PRESENCE decides whether
// the field rides; §3.4: on the fixed form the payload rides WHOLE either way,
// and an absent one reads the template's zeros. The record is round-tripped
// through SAVE into LOAD with the scalar at the storage's own boundary
// (INT32_MIN) and the enum at its top ordinal (Gold), then with BOTH absent —
// the empty end §2.3 names.
func cT11OptionalScalars(t *testing.T) {
	cT11Probe(t, cT11OptionalScalarsSrc, `
    {
        @ROOT@ v[2], back[2];
        static uint8_t buf[65536];
        TableReport r;
        int64_t n, i;
        for ( i = 0; i < 2; ++i ) { memset( &v[i], 0, sizeof( v[i] ) ); memset( &back[i], 0, sizeof( back[i] ) ); }
        @SNAKE@_reset( &v[0] );
        @SNAKE@_reset( &v[1] );
        /* non-default and boundary: the scalar's minimum and the enum's top */
        v[0].lead = 10; v[0].tail = 20;
        v[0].a_present = 1; v[0].a = ( -2147483647 - 1 );
        v[0].t_present = 1; v[0].t = TIER_GOLD;
        /* the empty end: both companions absent */
        v[1].lead = 30; v[1].tail = 40;
        n = @SNAKE@_fixed_save( v, 2, buf, sizeof( buf ) );
        if ( n <= 0 ) { printf( "optional-scalars: save n=%lld\n", (long long) n ); return 1; }
        memset( &r, 0, sizeof( r ) );
        n = @SNAKE@_fixed_load( back, 2, buf, n, plan, 4096, NULL, &r );
        if ( n != 2 ) { printf( "optional-scalars: load n=%lld refused=%d reason=%d\n", (long long) n, r.refused, (int) r.reason ); return 1; }
        if ( !back[0].a_present || back[0].a != ( -2147483647 - 1 ) )
            { printf( "optional-scalars: the present scalar did not land: present=%d a=%d\n", back[0].a_present, back[0].a ); return 1; }
        if ( !back[0].t_present || back[0].t != TIER_GOLD )
            { printf( "optional-scalars: the present enum did not land: present=%d t=%d\n", back[0].t_present, (int) back[0].t ); return 1; }
        if ( back[0].lead != 10 || back[0].tail != 20 )
            { printf( "optional-scalars: a neighbour moved\n" ); return 1; }
        if ( back[1].a_present || back[1].t_present )
            { printf( "optional-scalars: the absent companions read present\n" ); return 1; }
        if ( back[1].lead != 30 || back[1].tail != 40 )
            { printf( "optional-scalars: an absent record moved a neighbour\n" ); return 1; }
        if ( r.clamped || r.refused || r.malformed || r.unknown || r.kind_mismatch )
            { printf( "optional-scalars: a clean read moved a counter\n" ); return 1; }
    }
`)
}

// ---- optional-nesting/c/valid-data -----------------------------------------
//
// §2.3 and ALGORITHM §7 P1/P3: a `?T` over a nested `fixed table` is the same
// presence-plus-whole-payload construct one shape up. The nested value is
// round-tripped with non-default members and the boundary of its uint32 member,
// then absent, so the presence byte decides the whole nested body.
func cT11OptionalNesting(t *testing.T) {
	cT11Probe(t, cT11OptionalNestingSrc, `
    {
        @ROOT@ v, back;
        static uint8_t buf[65536];
        TableReport r;
        int64_t n;
        memset( &v, 0, sizeof( v ) ); memset( &back, 0, sizeof( back ) );
        @SNAKE@_reset( &v );
        v.lead = 5; v.tail = 6;
        v.link_present = 1; v.link.x = -1000; v.link.y = 4000000000u;
        n = @SNAKE@_fixed_save( &v, 1, buf, sizeof( buf ) );
        if ( n <= 0 ) { printf( "optional-nesting: save n=%lld\n", (long long) n ); return 1; }
        memset( &r, 0, sizeof( r ) );
        n = @SNAKE@_fixed_load( &back, 1, buf, n, plan, 4096, NULL, &r );
        if ( n != 1 ) { printf( "optional-nesting: load n=%lld refused=%d reason=%d\n", (long long) n, r.refused, (int) r.reason ); return 1; }
        if ( !back.link_present || back.link.x != -1000 || back.link.y != 4000000000u )
            { printf( "optional-nesting: the present nested value did not land: present=%d x=%d y=%u\n", back.link_present, back.link.x, back.link.y ); return 1; }
        if ( back.lead != 5 || back.tail != 6 ) { printf( "optional-nesting: a neighbour moved\n" ); return 1; }
        /* and the absent end: the nested payload does not ride */
        @SNAKE@_reset( &v );
        v.link_present = 0;
        n = @SNAKE@_fixed_save( &v, 1, buf, sizeof( buf ) );
        if ( n <= 0 ) { printf( "optional-nesting: absent save n=%lld\n", (long long) n ); return 1; }
        memset( &back, 0, sizeof( back ) );
        memset( &r, 0, sizeof( r ) );
        n = @SNAKE@_fixed_load( &back, 1, buf, n, plan, 4096, NULL, &r );
        if ( n != 1 || back.link_present ) { printf( "optional-nesting: the absent nested value read present\n" ); return 1; }
        if ( r.clamped || r.refused || r.malformed ) { printf( "optional-nesting: a clean read moved a counter\n" ); return 1; }
    }
`)
}

// ---- optional-arrays/c/valid-data ------------------------------------------
//
// §2.3: `?[N]T` is the array's storage plus one presence byte, and a present
// optional array ALWAYS rides as the array framing — the fixed array whole.
// The four slots are round-tripped at the signed storage's two ends and two
// more values, then the whole array is absent: "present and empty" and "absent"
// are two values, which is what the presence bit buys.
func cT11OptionalArrays(t *testing.T) {
	cT11Probe(t, cT11OptionalArraysSrc, `
    {
        @ROOT@ v, back;
        static uint8_t buf[65536];
        TableReport r;
        int64_t n;
        memset( &v, 0, sizeof( v ) ); memset( &back, 0, sizeof( back ) );
        @SNAKE@_reset( &v );
        v.lead = 5; v.trail = 6;
        v.vals_present = 1;
        v.vals[0] = ( -2147483647 - 1 ); v.vals[1] = 0; v.vals[2] = 2147483647; v.vals[3] = -1;
        n = @SNAKE@_fixed_save( &v, 1, buf, sizeof( buf ) );
        if ( n <= 0 ) { printf( "optional-arrays: save n=%lld\n", (long long) n ); return 1; }
        memset( &r, 0, sizeof( r ) );
        n = @SNAKE@_fixed_load( &back, 1, buf, n, plan, 4096, NULL, &r );
        if ( n != 1 ) { printf( "optional-arrays: load n=%lld refused=%d reason=%d\n", (long long) n, r.refused, (int) r.reason ); return 1; }
        if ( !back.vals_present ) { printf( "optional-arrays: the present array read absent\n" ); return 1; }
        if ( back.vals[0] != ( -2147483647 - 1 ) || back.vals[1] != 0 || back.vals[2] != 2147483647 || back.vals[3] != -1 )
            { printf( "optional-arrays: a slot did not land: %d %d %d %d\n", back.vals[0], back.vals[1], back.vals[2], back.vals[3] ); return 1; }
        if ( back.lead != 5 || back.trail != 6 ) { printf( "optional-arrays: a neighbour moved\n" ); return 1; }
        /* and the absent end */
        @SNAKE@_reset( &v );
        v.vals_present = 0;
        n = @SNAKE@_fixed_save( &v, 1, buf, sizeof( buf ) );
        if ( n <= 0 ) { printf( "optional-arrays: absent save n=%lld\n", (long long) n ); return 1; }
        memset( &back, 0, sizeof( back ) );
        memset( &r, 0, sizeof( r ) );
        n = @SNAKE@_fixed_load( &back, 1, buf, n, plan, 4096, NULL, &r );
        if ( n != 1 || back.vals_present ) { printf( "optional-arrays: the absent array read present\n" ); return 1; }
        if ( r.clamped || r.refused || r.malformed ) { printf( "optional-arrays: a clean read moved a counter\n" ); return 1; }
    }
`)
}

// ---- scalar-defaults/c/valid-data ------------------------------------------
//
// §3.4: the FIXED record's template memcpy's the declared defaults first, so a
// field the writer does not touch holds them. `reset` is that template's one
// definition (codecs.go), so it is asserted directly at the declared defaults;
// the record is then round-tripped holding them, and again holding the
// boundary values of each storage.
func cT11ScalarDefaults(t *testing.T) {
	cT11Probe(t, cT11ScalarDefaultsSrc, `
    {
        @ROOT@ v, back;
        static uint8_t buf[65536];
        TableReport r;
        int64_t n;
        memset( &v, 0, sizeof( v ) ); memset( &back, 0, sizeof( back ) );
        @SNAKE@_reset( &v );
        if ( v.a != 7 || v.b != 9 || v.m != MODE_ON || v.tail != 3 )
            { printf( "scalar-defaults: reset is not the declared defaults: a=%d b=%u m=%d tail=%u\n", v.a, v.b, (int) v.m, v.tail ); return 1; }
        n = @SNAKE@_fixed_save( &v, 1, buf, sizeof( buf ) );
        if ( n <= 0 ) { printf( "scalar-defaults: save n=%lld\n", (long long) n ); return 1; }
        memset( &r, 0, sizeof( r ) );
        n = @SNAKE@_fixed_load( &back, 1, buf, n, plan, 4096, NULL, &r );
        if ( n != 1 ) { printf( "scalar-defaults: load n=%lld\n", (long long) n ); return 1; }
        if ( back.a != 7 || back.b != 9 || back.m != MODE_ON || back.tail != 3 )
            { printf( "scalar-defaults: the defaults did not round-trip: a=%d b=%u m=%d tail=%u\n", back.a, back.b, (int) back.m, back.tail ); return 1; }
        /* non-default and boundary of each storage */
        @SNAKE@_reset( &v );
        v.a = ( -2147483647 - 1 ); v.b = 4294967295u; v.m = MODE_AUTO; v.tail = 4294967295u;
        n = @SNAKE@_fixed_save( &v, 1, buf, sizeof( buf ) );
        memset( &back, 0, sizeof( back ) );
        memset( &r, 0, sizeof( r ) );
        n = @SNAKE@_fixed_load( &back, 1, buf, n, plan, 4096, NULL, &r );
        if ( n != 1 || back.a != ( -2147483647 - 1 ) || back.b != 4294967295u || back.m != MODE_AUTO || back.tail != 4294967295u )
            { printf( "scalar-defaults: the boundary values did not round-trip: a=%d b=%u m=%d tail=%u\n", back.a, back.b, (int) back.m, back.tail ); return 1; }
        if ( r.clamped || r.refused || r.malformed ) { printf( "scalar-defaults: a clean read moved a counter\n" ); return 1; }
    }
`)
}

// ---- text-bytes-flags-defaults/c/valid-data --------------------------------
//
// §3.4 and SPEC §4.2: a `string(N)`, a `bytes(N)` and a `flags` mask all carry
// a declared default, and the template holds it. Each is asserted at its
// declared default, round-tripped, and then round-tripped at the boundary —
// the text and byte buffers filled to their declared capacities (N = 8, N = 4)
// and the flags mask at every storage bit set.
func cT11TextBytesFlagsDefaults(t *testing.T) {
	cT11Probe(t, cT11TextBytesFlagsSrc, `
    {
        @ROOT@ v, back;
        static uint8_t buf[65536];
        TableReport r;
        uint64_t fmask;
        int64_t n;
        memset( &v, 0, sizeof( v ) ); memset( &back, 0, sizeof( back ) );
        @SNAKE@_reset( &v );
        if ( v.label_length != 2 || memcmp( v.label, "hi", 2 ) != 0 )
            { printf( "text-bytes-flags-defaults: the string default is not \"hi\": len=%d\n", v.label_length ); return 1; }
        if ( v.blob_length != 2 || memcmp( v.blob, "ab", 2 ) != 0 )
            { printf( "text-bytes-flags-defaults: the bytes default is not \"ab\": len=%d\n", v.blob_length ); return 1; }
        fmask = v.f;
        if ( fmask != ( FL_ALPHA | FL_GAMMA ) )
            { printf( "text-bytes-flags-defaults: the flags default is not {Alpha,Gamma}: 0x%llx\n", (unsigned long long) fmask ); return 1; }
        n = @SNAKE@_fixed_save( &v, 1, buf, sizeof( buf ) );
        if ( n <= 0 ) { printf( "text-bytes-flags-defaults: save n=%lld\n", (long long) n ); return 1; }
        memset( &r, 0, sizeof( r ) );
        n = @SNAKE@_fixed_load( &back, 1, buf, n, plan, 4096, NULL, &r );
        if ( n != 1 ) { printf( "text-bytes-flags-defaults: load n=%lld\n", (long long) n ); return 1; }
        if ( back.label_length != 2 || memcmp( back.label, "hi", 2 ) != 0 || back.blob_length != 2 || memcmp( back.blob, "ab", 2 ) != 0 || back.f != fmask )
            { printf( "text-bytes-flags-defaults: a declared default did not round-trip\n" ); return 1; }
        /* non-default and boundary: buffers full to capacity, every flag bit set */
        @SNAKE@_reset( &v );
        memcpy( v.label, "abcdefgh", 8 ); v.label_length = 8;
        memcpy( v.blob, "WXYZ", 4 ); v.blob_length = 4;
        v.f = ~(uint64_t) 0;
        n = @SNAKE@_fixed_save( &v, 1, buf, sizeof( buf ) );
        memset( &back, 0, sizeof( back ) );
        memset( &r, 0, sizeof( r ) );
        n = @SNAKE@_fixed_load( &back, 1, buf, n, plan, 4096, NULL, &r );
        if ( n != 1 || back.label_length != 8 || memcmp( back.label, "abcdefgh", 8 ) != 0 )
            { printf( "text-bytes-flags-defaults: the full string did not round-trip: len=%d\n", back.label_length ); return 1; }
        if ( back.blob_length != 4 || memcmp( back.blob, "WXYZ", 4 ) != 0 )
            { printf( "text-bytes-flags-defaults: the full bytes did not round-trip: len=%d\n", back.blob_length ); return 1; }
        if ( back.f != ~(uint64_t) 0 ) { printf( "text-bytes-flags-defaults: the flags mask did not round-trip: 0x%llx\n", (unsigned long long) back.f ); return 1; }
        if ( r.clamped || r.refused || r.malformed ) { printf( "text-bytes-flags-defaults: a clean read moved a counter\n" ); return 1; }
    }
`)
}
