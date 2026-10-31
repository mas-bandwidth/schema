package ctable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE C LEG'S FINITE RULE (docs/SPEC-TABLES.md §3.3; Glenn, 2026-10-27,
// "tables go finite-only too, same rule as packets"): every read refuses a NaN
// or an infinity on its raw bits as non_finite_float, the tolerant read too,
// and lands nothing; every writer refuses one; -0 and the subnormals ride
// bit-exact, and a -0 under a +0 default is not elided.
//
// TestCFiniteFloats is the pin: one probe over every form the C leg carries —
// the file wire's value read, the region read of a variable root and its
// retaining read (ruling 3: a retained unknown kind 10/11 entry is tested like
// a known one), the message batch, the fixed form (whose finite pass runs over
// storage before the bounds pass, a compressed float included: ruling 1), and
// the writers of each, the cook among them. Ruling 2 has no site on this leg:
// C block rows and cooked records are the caller's structs read in place, and
// no accessor decodes a float.
//
// TestCFiniteFloatNegativeControl is the gate's own proof: each check is
// sabotaged in turn in the emitted source, and the probe must go red on
// exactly the rows that check owns, and on no other.

const finiteProbeSchema = `package probe

fixed table Cell
{
    a float32
    b float64
    c [2]float32
    d float32 | min = -10, max = 10, resolution = 0.01
}

table Node
{
    v float32
    w float64
    next *Node
}
`

// Each sentinel is a finite pattern no other byte run of its wire repeats; a
// row saves it, swaps it for a non-finite pattern and reads the result back.
const finiteProbe = `#include "ProbeTable.h"
#include <stdio.h>
#include <stdlib.h>

static int failures;
#define FAIL( row, why ) do { printf( "FAIL: %s: %s\n", row, why ); failures++; } while ( 0 )

static float f32of( uint32_t b ) { float f; memcpy( &f, &b, 4 ); return f; }
static double f64of( uint64_t b ) { double d; memcpy( &d, &b, 8 ); return d; }
static uint32_t f32bits( float f ) { uint32_t b; memcpy( &b, &f, 4 ); return b; }
static uint64_t f64bits( double d ) { uint64_t b; memcpy( &b, &d, 8 ); return b; }

#define SA 0x3f9e0651u            /* a: 1.23457 */
#define SB 0x400921fb54442d18ull  /* b: pi */
#define SC 0x40490fdbu            /* c[1]: pi in single */
#define SD 0x40b33333u            /* d: 5.6, inside the bounds */
#define SV 0x3fb504f3u            /* v: sqrt 2 */
#define SW 0x4005bf0a8b145769ull  /* w: e */

static void sentinel( Cell * value )
{
    cell_reset( value );
    value->a = f32of( SA ); value->b = f64of( SB ); value->c[0] = 0.5f; value->c[1] = f32of( SC ); value->d = f32of( SD );
}

/* replace the one LSB-first run of width bits equal to from by to: the
   file and fixed forms are byte aligned and the message form is not, and
   one search serves both */
static int patch( uint8_t * buffer, int64_t bytes, uint64_t from, uint64_t to, int width )
{
    int64_t at, i; int found = 0;
    for ( at = 0; at + width <= bytes * 8; at++ )
    {
        uint64_t v = 0;
        for ( i = 0; i < width; i++ ) { v |= (uint64_t) ( ( buffer[( at + i ) >> 3] >> ( ( at + i ) & 7 ) ) & 1 ) << i; }
        if ( v != from ) { continue; }
        for ( i = 0; i < width; i++ )
        {
            int64_t k = at + i; uint8_t bit = (uint8_t) ( ( to >> i ) & 1 );
            buffer[k >> 3] = (uint8_t) ( ( buffer[k >> 3] & ~( 1u << ( k & 7 ) ) ) | ( bit << ( k & 7 ) ) );
        }
        found++;
        at += width - 1;
    }
    return found;
}

static int cell_is_reset( const Cell * value )
{
    Cell fresh; cell_reset( &fresh );
    return f32bits( value->a ) == f32bits( fresh.a ) && f64bits( value->b ) == f64bits( fresh.b ) &&
           f32bits( value->c[0] ) == f32bits( fresh.c[0] ) && f32bits( value->c[1] ) == f32bits( fresh.c[1] ) &&
           f32bits( value->d ) == f32bits( fresh.d );
}

static int refused( const TableReport * report )
{
    return report->refused && report->reason == SCHEMA_TABLE_NON_FINITE_FLOAT && report->non_finite_floats > 0;
}

/* THE FILE WIRE: a value read, tolerant and strict */
static void file_row( const char * row, uint64_t from, uint64_t to, int width )
{
    Cell value; TableReport report; uint8_t wire[256]; int64_t n;
    sentinel( &value );
    n = cell_save( &value, wire, sizeof( wire ) );
    if ( n < 0 || patch( wire, n, from, to, width ) != 1 ) { FAIL( row, "the sentinel did not ride exactly once" ); return; }
    memset( &report, 0, sizeof( report ) );
    if ( cell_load_tolerant( &value, wire, n, &report ) || !refused( &report ) || !cell_is_reset( &value ) )
    { FAIL( row, "the tolerant read did not refuse it, or landed something" ); return; }
    sentinel( &value );
    if ( cell_load( &value, wire, n, &report ) || report.refuse_reason != SCHEMA_TABLE_REFUSE_NON_FINITE_FLOAT || !cell_is_reset( &value ) )
    { FAIL( row, "the strict read did not refuse it as SCHEMA_TABLE_REFUSE_NON_FINITE_FLOAT" ); }
}

/* THE MESSAGE BATCH: the second record carries it, and the whole batch is refused */
static void message_row( const char * row, uint64_t from, uint64_t to, int width )
{
    TableMessageEntry entries[kTableMessageEntriesHere]; TableVocabulary v = table_vocabulary( entries, kTableMessageEntriesHere );
    Cell values[2]; TableReport report; uint8_t wire[512]; int64_t n, count = 2;
    memset( &report, 0, sizeof( report ) );
    if ( !announce_read( &v, kTableAnnounce, kTableAnnounceBytes, &report ) ) { FAIL( row, "announce_read" ); return; }
    cell_reset( &values[0] ); values[0].a = 0.25f; values[0].b = 0.75; sentinel( &values[1] );
    n = cell_save_messages( values, 2, wire, sizeof( wire ), &report );
    if ( n < 0 || patch( wire, n, from, to, width ) != 1 ) { FAIL( row, "the sentinel did not ride exactly once" ); return; }
    memset( &report, 0, sizeof( report ) );
    if ( cell_load_messages_tolerant( values, &count, &v, wire, n, &report ) || count != 0 || !refused( &report ) ||
         !cell_is_reset( &values[0] ) || !cell_is_reset( &values[1] ) )
    { FAIL( row, "the batch was not refused whole" ); return; }
    count = 2;
    if ( cell_load_messages( values, &count, &v, wire, n, &report ) || report.refuse_reason != SCHEMA_TABLE_REFUSE_NON_FINITE_FLOAT )
    { FAIL( row, "the strict batch did not refuse it as SCHEMA_TABLE_REFUSE_NON_FINITE_FLOAT" ); }
}

/* THE FIXED FORM: the second record carries it; every record is reset */
static void fixed_row( const char * row, uint64_t from, uint64_t to, int width )
{
    Cell values[2]; TableReport report; uint8_t wire[512]; int64_t n;
    cell_reset( &values[0] ); values[0].a = 0.25f; sentinel( &values[1] );
    n = cell_fixed_save( values, 2, wire, sizeof( wire ) );
    if ( n < 0 || patch( wire, n, from, to, width ) != 1 ) { FAIL( row, "the sentinel did not ride exactly once" ); return; }
    memset( &report, 0, sizeof( report ) );
    if ( cell_fixed_load_tolerant( values, 2, wire, n, NULL, 0, NULL, &report ) != -1 || !refused( &report ) ||
         !cell_is_reset( &values[0] ) || !cell_is_reset( &values[1] ) || report.clamped != 0 )
    { FAIL( row, "the fixed read did not refuse the batch, or landed something" ); return; }
    if ( cell_fixed_load( values, 2, wire, n, NULL, 0, NULL, &report ) != -1 || report.refuse_reason != SCHEMA_TABLE_REFUSE_NON_FINITE_FLOAT )
    { FAIL( row, "the strict fixed read did not refuse it as SCHEMA_TABLE_REFUSE_NON_FINITE_FLOAT" ); }
}

/* A VARIABLE ROOT: the region read and its retaining twin */
static int64_t node_wire( uint8_t * wire, int64_t capacity, float v, double w )
{
    NodeBuilder b; Node * root; TableCtx ctx; int64_t n;
    if ( !node_builder_init( &b ) ) { return -1; }
    root = node_builder_root( &b ); root->v = v; root->w = w;
    memset( &ctx, 0, sizeof( ctx ) ); ctx.arena = &b.arena;
    n = node_save( &ctx, root, wire, capacity );
    node_builder_shutdown( &b );
    return n;
}

static void graph_row( const char * row, uint64_t from, uint64_t to, int width )
{
    uint8_t wire[512]; uint8_t * region; int64_t n, need; TableReport report;
    n = node_wire( wire, sizeof( wire ), f32of( SV ), f64of( SW ) );
    if ( n < 0 || patch( wire, n, from, to, width ) != 1 ) { FAIL( row, "the sentinel did not ride exactly once" ); return; }
    need = node_load_measure( wire, n );
    region = (uint8_t *) malloc( (size_t) ( need > 0 ? need : 1 ) );
    if ( need <= 0 || region == NULL ) { FAIL( row, "load_measure" ); free( region ); return; }
    memset( &report, 0, sizeof( report ) );
    if ( node_load_tolerant( region, need, wire, n, &report ) != NULL || !refused( &report ) )
    { FAIL( row, "the region read did not refuse it" ); free( region ); return; }
    if ( node_load( region, need, wire, n, &report ) != NULL || report.refuse_reason != SCHEMA_TABLE_REFUSE_NON_FINITE_FLOAT )
    { FAIL( row, "the strict region read did not refuse it as SCHEMA_TABLE_REFUSE_NON_FINITE_FLOAT" ); }
    free( region );
}

/* RULING 3: the field v renamed to an id this build does not know is an
   unknown kind 10 entry, retained by the retaining read and tested first */
static void retained_row( const char * row, uint64_t v_id )
{
    static uint8_t store[1 << 14]; static TableRetainId ids[256];
    uint8_t wire[512]; uint8_t * region; int64_t n, need; TableReport report; TableRetain retain;
    n = node_wire( wire, sizeof( wire ), f32of( SV ), f64of( SW ) );
    if ( n < 0 || patch( wire, n, v_id, 0x5eed5eed5eed5eedull, 64 ) != 1 || patch( wire, n, SV, 0x7fa00001u, 32 ) != 1 )
    { FAIL( row, "the field did not ride exactly once" ); return; }
    need = node_load_measure( wire, n );
    region = (uint8_t *) malloc( (size_t) ( need > 0 ? need : 1 ) );
    if ( need <= 0 || region == NULL ) { FAIL( row, "load_measure" ); free( region ); return; }
    memset( &retain, 0, sizeof( retain ) ); retain.bytes = store; retain.capacity = sizeof( store ); retain.ids = ids; retain.id_capacity = 256;
    memset( &report, 0, sizeof( report ) );
    if ( node_load_retain( region, need, wire, n, &retain, &report ) != NULL || !refused( &report ) || report.retained != 0 )
    { FAIL( row, "the retaining read kept a NaN" ); }
    free( region );
}

static void writer_rows( void )
{
    Cell value, values[2]; TableReport report; uint8_t out[512]; NodeBuilder b; Node * root; TableCtx ctx;
    sentinel( &value ); value.a = f32of( 0x7fc00000u );
    if ( cell_measure( &value ) != -1 || cell_save( &value, out, sizeof( out ) ) != -1 ) { FAIL( "save a", "the file writer wrote a NaN" ); }
    sentinel( &value ); value.c[1] = f32of( 0xff800000u );
    if ( cell_measure( &value ) != -1 || cell_save( &value, out, sizeof( out ) ) != -1 ) { FAIL( "save c", "the file writer wrote an infinity in an array" ); }
    if ( node_builder_init( &b ) )
    {
        root = node_builder_root( &b ); root->w = f64of( 0x7ff0000000000000ull );
        memset( &ctx, 0, sizeof( ctx ) ); ctx.arena = &b.arena;
        if ( node_save( &ctx, root, out, sizeof( out ) ) != -1 ) { FAIL( "save w", "the graph writer wrote an infinity" ); }
        node_builder_shutdown( &b );
    }
    sentinel( &values[0] ); sentinel( &values[1] ); values[1].b = f64of( 0xfff8000000000000ull );
    memset( &report, 0, sizeof( report ) );
    if ( cell_measure_messages( values, 2, &report ) != -1 || cell_save_messages( values, 2, out, sizeof( out ), &report ) != -1 )
    { FAIL( "message save", "the message writer wrote a NaN" ); }
    sentinel( &values[1] ); values[1].d = f32of( 0x7f800000u );
    if ( cell_save_messages( values, 2, out, sizeof( out ), &report ) != -1 ) { FAIL( "message save", "the message writer quantized an infinity" ); }
    sentinel( &values[1] ); values[1].a = f32of( 0xffc00001u );
    if ( cell_fixed_save( values, 2, out, sizeof( out ) ) != -1 ) { FAIL( "fixed save", "the fixed writer wrote a NaN" ); }
    sentinel( &value ); value.b = f64of( 0x7ff0000000000001ull );
    if ( cell_cook( &value, out, sizeof( out ), TableByteOrder_Little ) ) { FAIL( "cook", "the cook writer cooked a NaN" ); }
}

/* -0 AND THE SUBNORMALS RIDE BIT-EXACT, and -0 is not elided under +0 */
static void finite_rows( void )
{
    TableMessageEntry entries[kTableMessageEntriesHere]; TableVocabulary v = table_vocabulary( entries, kTableMessageEntriesHere );
    Cell value, back, values[1]; TableReport report; uint8_t out[512]; int64_t n, count = 1;
    cell_reset( &value ); cell_reset( &back );
    value.a = -0.0f;
    if ( cell_measure( &value ) == cell_measure( &back ) ) { FAIL( "finite", "a -0 under the +0 default was elided" ); }
    value.b = f64of( 0x8000000000000001ull ); value.c[1] = f32of( 0x00000001u );
    n = cell_save( &value, out, sizeof( out ) ); memset( &report, 0, sizeof( report ) );
    if ( n < 0 || !cell_load( &back, out, n, &report ) || f32bits( back.a ) != 0x80000000u || f64bits( back.b ) != 0x8000000000000001ull || f32bits( back.c[1] ) != 1u )
    { FAIL( "finite", "the file wire did not carry -0 and a subnormal bit-exact" ); }
    n = cell_fixed_save( &value, 1, out, sizeof( out ) ); memset( &report, 0, sizeof( report ) );
    if ( n < 0 || cell_fixed_load( &back, 1, out, n, NULL, 0, NULL, &report ) != 1 || f32bits( back.a ) != 0x80000000u || f64bits( back.b ) != 0x8000000000000001ull )
    { FAIL( "finite", "the fixed form did not carry -0 and a subnormal bit-exact" ); }
    memset( &report, 0, sizeof( report ) );
    if ( !announce_read( &v, kTableAnnounce, kTableAnnounceBytes, &report ) ) { FAIL( "finite", "announce_read" ); return; }
    values[0] = value; n = cell_save_messages( values, 1, out, sizeof( out ), &report );
    if ( n < 0 || !cell_load_messages( values, &count, &v, out, n, &report ) || count != 1 || f32bits( values[0].a ) != 0x80000000u || f64bits( values[0].b ) != 0x8000000000000001ull )
    { FAIL( "finite", "the message form did not carry -0 and a subnormal bit-exact" ); }
    if ( cell_cook( &value, out, sizeof( out ), TableByteOrder_Little ) != 1 ) { FAIL( "finite", "the cook refused a finite value" ); }
}

int main( void )
{
    file_row( "file a", SA, 0x7fc00000u, 32 );
    file_row( "file b", SB, 0x7ff0000000000000ull, 64 );
    file_row( "file c", SC, 0xff800001u, 32 );
    file_row( "file d", SD, 0x7f800000u, 32 );
    message_row( "message a", SA, 0xffa5a5a5u, 32 );
    message_row( "message b", SB, 0xfff0000000000000ull, 64 );
    fixed_row( "fixed b", SB, 0x7ff8000000000000ull, 64 );
    fixed_row( "fixed d", SD, 0xff800000u, 32 );
    graph_row( "graph v", SV, 0x7fc0dead, 32 );
    graph_row( "graph w", SW, 0xfff0000000000000ull, 64 );
    retained_row( "retained", NODE_V_ID );
    writer_rows();
    finite_rows();
    return failures != 0;
}
`

// finiteSabotage is one check blinded in the emitted source (each pair a
// from and its replacement), and the rows the probe must then fail: those
// rows, every one of them, and no other. The anchors are whole statements,
// because the emitter's formatter breaks the compact runtime after each one.
type finiteSabotage struct {
	name  string
	pairs [][2]string
	rows  []string
}

var finiteSabotages = []finiteSabotage{
	{"variable read", [][2]string{{"if ( !table_bits_finite32( decoded_bits ) ) { table_non_finite_float( r->report ); }", "if ( 0 ) { table_non_finite_float( r->report ); }"}},
		[]string{"file a", "file c", "file d", "graph v"}},
	{"variable read f64", [][2]string{{"if ( !table_bits_finite64( decoded_bits ) ) { table_non_finite_float( r->report ); }", "if ( 0 ) { table_non_finite_float( r->report ); }"}},
		[]string{"file b", "graph w"}},
	{"message read", [][2]string{{"else if(!table_bits_finite32((uint32_t)raw)){", "else if(0){"}},
		[]string{"message a"}},
	{"message read f64", [][2]string{{"finite=table_bits_finite64(raw);", "finite=1;"}},
		[]string{"message b"}},
	{"fixed read", [][2]string{{"if ( !schema_probe_cell_fixed_finite_body_( values + k ) )\n", "if ( 0 )\n"}},
		[]string{"fixed b", "fixed d"}},
	{"retained read", [][2]string{{"if(kind==10?!table_bits_finite32((uint32_t)raw):!table_bits_finite64(raw)){", "if(0){"}},
		[]string{"retained"}},
	{"file writer", [][2]string{{") ) { return 0; } /* a NaN or an infinity is refused (§3.3) */", ") ) { (void) 0; } /* a NaN or an infinity is refused (§3.3) */"}},
		[]string{"save a", "save c", "save w"}},
	{"message writer", [][2]string{
		{"if(!table_double_finite(value->b)) return 0;", "if(0) return 0;"},
		{"if(!table_float_finite(value->d)) return 0;", "if(0) return 0;"}},
		[]string{"message save"}},
	{"fixed writer", [][2]string{{"if ( !schema_probe_cell_fixed_finite_body_( values + k ) ) { return -1; }", "if ( 0 ) { return -1; }"}},
		[]string{"fixed save"}},
	{"cook writer", [][2]string{{"if(!table_bits_finite64(bits)) { return 0; }", ""}},
		[]string{"cook"}},
}

// finiteProbeRun builds the probe over the generated unit, with sabotage (if
// any) applied to the emitted header, and answers the FAIL lines.
func finiteProbeRun(t *testing.T, sabotage *finiteSabotage) ([]string, string) {
	t.Helper()
	cc := os.Getenv("CC")
	if cc == "" {
		var err error
		cc, err = exec.LookPath("cc")
		if err != nil {
			t.Skip("generated C execution requires cc")
		}
	}
	u := unitFrom(t, finiteProbeSchema)
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	if sabotage != nil {
		for _, pair := range sabotage.pairs {
			patched := false
			for name, data := range files {
				if src := string(data); strings.Contains(src, pair[0]) {
					files[name] = []byte(strings.ReplaceAll(src, pair[0], pair[1]))
					patched = true
				}
			}
			if !patched {
				t.Fatalf("the %s sabotage patches nothing: no emitted file carries %q", sabotage.name, pair[0])
			}
		}
	}
	var vID uint64
	for _, f := range u.Tables["Node"].Fields {
		if f.Name == "v" {
			vID = ir.TableFieldWireId(f)
		}
	}
	source := strings.Replace(finiteProbe, "NODE_V_ID", fmt.Sprintf("0x%016xull", vID), 1)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.c"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	opt := "-O2"
	if sabotage != nil {
		opt = "-O0" // a control is run once and thrown away (make/c.mk TABLES_CFLAGS_CONTROL)
	}
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-Wshadow", opt, "-fsanitize=address,undefined", "-fno-sanitize-recover=all"}
	args = append(args, gccFortifySilence(cc)...)
	args = append(args, "-I", dir, filepath.Join(dir, "main.c"))
	for name := range files {
		if strings.HasSuffix(name, ".c") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	args = append(args, "-lm", "-o", filepath.Join(dir, "probe"))
	if output, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, output)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	output, _ := exec.CommandContext(ctx, filepath.Join(dir, "probe")).CombinedOutput()
	var fails []string
	for line := range strings.SplitSeq(string(output), "\n") {
		if rest, ok := strings.CutPrefix(line, "FAIL: "); ok {
			fails = append(fails, rest)
		}
	}
	if strings.Contains(string(output), "ERROR: AddressSanitizer") || strings.Contains(string(output), "runtime error:") {
		t.Fatalf("the probe tripped a sanitizer:\n%s", output)
	}
	return fails, string(output)
}

func TestCFiniteFloats(t *testing.T) {
	slowtest.Gate(t, "the C compiler (cc)")
	t.Parallel()
	if fails, out := finiteProbeRun(t, nil); len(fails) != 0 {
		t.Fatalf("the C finite rule does not hold:\n%s", out)
	}
}

func TestCFiniteFloatNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the C compiler (cc)")
	t.Parallel()
	for i := range finiteSabotages {
		c := &finiteSabotages[i]
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			fails, out := finiteProbeRun(t, c)
			if len(fails) == 0 {
				t.Fatalf("NEGATIVE CONTROL FAILED: the %s sabotage left the probe green:\n%s", c.name, out)
			}
			named := map[string]bool{}
			for _, line := range fails {
				row, _, _ := strings.Cut(line, ": ")
				owned := false
				for _, want := range c.rows {
					if row == want {
						named[want], owned = true, true
					}
				}
				if !owned {
					t.Errorf("the %s sabotage reddened a row it does not own: %s", c.name, line)
				}
			}
			for _, want := range c.rows {
				if !named[want] {
					t.Errorf("the %s sabotage left its row %q green:\n%s", c.name, want, out)
				}
			}
		})
	}
}
