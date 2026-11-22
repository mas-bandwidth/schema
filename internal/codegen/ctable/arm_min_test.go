package ctable

// TestCCountedMinimumHolds holds ruling R10 on the C leg: a counted [A..B]
// holds A wherever it sits, as a plain field and in a union arm
// (docs/SPEC-TABLES.md §3.4; the shared rows are json-hostile arm-min-fix-*
// and plain-min-*, and framing armmin_fix_count_* and plainmin_fix_count_*).
// Each case runs once for the arm and once for the plain field: the probe
// reaches the field through the SHAPE macros the case puts in front of it.
//
// THE READ SIDE, in every form: a probe built with NDEBUG (so the debug-only
// write asserts are gone) writes a count of 1 and of 0 through each writer and
// reads it back through the matching reader, which must refuse: the variable
// wire, the fixed form and the text form, while a count at A still reads. The
// message form packs the count as count - A in its own writer, so a count
// below A is reached only from a WRITER whose announced shape has a lower
// minimum: a second unit, the same names with [..4], writes the messages and
// the [2..4] unit reads them.
//
// THE WRITE SIDE is C's own idiom, a debug assert (docs/SPEC.md §5.1): each
// writer (variable wire, fixed form, message form, cook) handed a count below
// A in a debug build stops in its own process, and the text writer refuses in
// every build as it already does a count past B.

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

const cArmMinSchema = `package armmin
union MinPick { few [2..4]uint32
    other int32 }
fixed table Holder { pick MinPick }
`

// the writer of the message case: the same names, a minimum of zero
const cArmMinLooseSchema = `package armmin
union MinPick { few [..4]uint32
    other int32 }
fixed table Holder { pick MinPick }
`

// the arm's SHAPE: how the probe reaches the counted array
const cArmMinShape = `#define PREP( v ) ( (v)->pick.type = MIN_PICK_TYPE_FEW )
#define COUNT( v ) ( (v)->pick.as.few.value_count )
#define ELEM( v ) ( (v)->pick.as.few.value )
#define TEXT "{\"pick\":{\"few\":%s}}"
`

// the same counted [A..B] as a plain field, and its [..4] writer
const cPlainMinSchema = `package plainmin
fixed table Holder { few [2..4]uint32 }
`

const cPlainMinLooseSchema = `package plainmin
fixed table Holder { few [..4]uint32 }
`

const cPlainMinShape = `#define PREP( v ) ( (void) (v) )
#define COUNT( v ) ( (v)->few_count )
#define ELEM( v ) ( (v)->few )
#define TEXT "{\"few\":%s}"
`

const cArmMinProbe = `#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "HEADER"
SHAPE

static uint8_t buf[1 << 16], again[1 << 16];
static TableFixedEntry plan[4096];

/* a Holder whose counted array holds the count under test */
static void make( Holder * v, int32_t n )
{
    holder_reset( v );
    PREP( v );
    COUNT( v ) = n;
    ELEM( v )[0] = 7;
    ELEM( v )[1] = 9;
}

static int64_t spill( const char * path, const uint8_t * data, int64_t size )
{
    FILE * f = fopen( path, "wb" );
    if ( f == NULL ) { return -1; }
    fwrite( data, 1, (size_t) size, f );
    fclose( f );
    return size;
}

static int64_t slurp( const char * path )
{
    FILE * f = fopen( path, "rb" );
    size_t got;
    if ( f == NULL ) { return -1; }
    got = fread( buf, 1, sizeof( buf ), f );
    fclose( f );
    return (int64_t) got;
}

int main( int argc, char ** argv )
{
    Holder v, back[4];
    TableReport r;
    int64_t size;
    int bad = 0;
    int32_t n;
    if ( argc < 2 ) { return 2; }

    /* THE READ SIDE: every form refuses a count below A and still reads a
       count at A. */
    if ( strcmp( argv[1], "read" ) == 0 )
    {
        for ( n = 0; n <= 2; n++ )
        {
            const int want = n == 2; /* 1 = must read, 0 = must refuse */
            int got;
            make( &v, n );
            size = holder_save( &v, buf, sizeof( buf ) );
            memset( &r, 0, sizeof( r ) );
            got = size > 0 && holder_load( &back[0], buf, size, &r ) && !r.refused;
            if ( got != want ) { printf( "wire count %d: read=%d\n", (int) n, got ); bad++; }

            size = holder_fixed_save( &v, 1, buf, sizeof( buf ) );
            memset( &r, 0, sizeof( r ) );
            got = size > 0 && holder_fixed_load( back, 4, buf, size, plan, 4096, NULL, &r ) == 1 && !r.refused;
            if ( got != want ) { printf( "fixed count %d: read=%d\n", (int) n, got ); bad++; }

            {
                char text[128];
                const char * list = n == 0 ? "[]" : n == 1 ? "[7]" : "[7,9]";
                snprintf( text, sizeof( text ), TEXT, list );
                memset( &r, 0, sizeof( r ) );
                got = holder_from_json( &back[0], text, (int64_t) strlen( text ), &r ) && !r.refused;
                if ( got != want ) { printf( "json count %d: read=%d\n", (int) n, got ); bad++; }
            }
        }
        if ( bad ) { return 1; }
        printf( "every form holds the minimum\n" );
        return 0;
    }

    /* THE MESSAGE CASE, the writer's half: run by the [..4] unit. */
    if ( strcmp( argv[1], "message-write" ) == 0 && argc == 4 )
    {
        TableReport ignored;
        make( &v, 1 );
        if ( spill( argv[2], kTableAnnounce, kTableAnnounceBytes ) < 0 ) { return 1; }
        memset( &ignored, 0, sizeof( ignored ) );
        size = holder_save_messages( &v, 1, buf, sizeof( buf ), &ignored );
        if ( size <= 0 || spill( argv[3], buf, size ) < 0 ) { printf( "message write failed\n" ); return 1; }
        printf( "WROTE\n" );
        return 0;
    }

    /* and the reader's half: run by the [2..4] unit, which must refuse. */
    if ( strcmp( argv[1], "message-read" ) == 0 && argc == 4 )
    {
        static TableMessageEntry entries[4096];
        static uint8_t announce[1 << 16];
        TableVocabulary voc = table_vocabulary( entries, 4096 );
        int64_t count = 4, an;
        int ok;
        an = slurp( argv[2] );
        if ( an < 0 ) { return 1; }
        memcpy( announce, buf, (size_t) an );
        memset( &r, 0, sizeof( r ) );
        if ( !announce_read( &voc, announce, an, &r ) ) { printf( "announce refused\n" ); return 1; }
        size = slurp( argv[3] );
        if ( size < 0 ) { return 1; }
        memset( &r, 0, sizeof( r ) );
        ok = holder_load_messages( back, &count, &voc, buf, size, &r );
        if ( ok && !r.refused ) { printf( "message: a count below the minimum read\n" ); return 1; }
        printf( "the message form holds the minimum\n" );
        return 0;
    }

    /* A FRESH VALUE IS LAWFUL: reset leaves the field born, A default
       elements, and every writer saves it in a debug build; the wire and
       fixed forms read it back born. */
    if ( strcmp( argv[1], "fresh" ) == 0 )
    {
        TableReport ignored;
        holder_reset( &v );
        memset( &ignored, 0, sizeof( ignored ) );
        size = holder_save( &v, buf, sizeof( buf ) );
        memset( &r, 0, sizeof( r ) );
        if ( size <= 0 || !holder_load( &back[0], buf, size, &r ) || r.refused || holder_save( &back[0], again, sizeof( again ) ) != size || memcmp( again, buf, (size_t) size ) != 0 ) { printf( "wire: a fresh value did not round-trip\n" ); bad++; }
        size = holder_fixed_save( &v, 1, buf, sizeof( buf ) );
        memset( &r, 0, sizeof( r ) );
        if ( size <= 0 || holder_fixed_load( back, 4, buf, size, plan, 4096, NULL, &r ) != 1 || r.refused ) { printf( "fixed: a fresh value did not round-trip\n" ); bad++; }
        if ( holder_save_messages( &v, 1, buf, sizeof( buf ), &ignored ) <= 0 ) { printf( "message: a fresh value did not save\n" ); bad++; }
        if ( !holder_cook( &v, buf, sizeof( buf ), (TableByteOrder) 0 ) ) { printf( "cook: a fresh value did not save\n" ); bad++; }
        if ( holder_to_json( &v, (char *) buf, sizeof( buf ) ) < 0 ) { printf( "json: a fresh value did not save\n" ); bad++; }
        if ( bad ) { return 1; }
        printf( "a fresh value saves in every form\n" );
        return 0;
    }

    /* THE WRITE SIDE: one writer per process, handed a count below A. */
    if ( strcmp( argv[1], "write" ) == 0 && argc == 3 )
    {
        TableReport ignored;
        make( &v, 1 );
        memset( &ignored, 0, sizeof( ignored ) );
        if ( strcmp( argv[2], "wire" ) == 0 ) { size = holder_save( &v, buf, sizeof( buf ) ); }
        else if ( strcmp( argv[2], "fixed" ) == 0 ) { size = holder_fixed_save( &v, 1, buf, sizeof( buf ) ); }
        else if ( strcmp( argv[2], "message" ) == 0 ) { size = holder_save_messages( &v, 1, buf, sizeof( buf ), &ignored ); }
        else if ( strcmp( argv[2], "cook" ) == 0 ) { size = holder_cook( &v, buf, sizeof( buf ), (TableByteOrder) 0 ) ? 1 : -1; }
        else if ( strcmp( argv[2], "json" ) == 0 )
        {
            /* the text writer refuses in every build */
            size = holder_to_json( &v, (char *) buf, sizeof( buf ) );
            if ( size >= 0 ) { printf( "json wrote a count below the minimum\n" ); return 1; }
            printf( "REFUSED\n" );
            return 0;
        }
        else { return 2; }
        printf( "SAVED %lld\n", (long long) size );
        return 0;
    }
    printf( "unknown mode\n" );
    return 2;
}
`

// cArmMinBuild generates src's C, compiles the probe beside it with the given
// extra flags, and returns the program's path. shape is the SHAPE block, and
// probe the program (cArmMinProbe when empty).
func cArmMinBuild(t *testing.T, src, shape, probe, name string, flags ...string) string {
	t.Helper()
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	u := unitFrom(t, src)
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	var srcs []string
	header := ""
	for file, data := range files {
		if strings.Contains(file, "/") {
			continue
		}
		if strings.HasSuffix(file, "Table.h") {
			header = file
		}
		if err := os.WriteFile(filepath.Join(dir, file), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(file, ".c") {
			srcs = append(srcs, filepath.Join(dir, file))
		}
	}
	if header == "" {
		t.Fatal("no Table.h was generated")
	}
	if probe == "" {
		probe = cArmMinProbe
	}
	probe = strings.Replace(probe, "HEADER", header, 1)
	probe = strings.Replace(probe, "SHAPE", shape, 1)
	if err := os.WriteFile(filepath.Join(dir, "probe.c"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, name)
	build := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-O1", "-I" + dir}
	build = append(build, flags...)
	build = append(build, filepath.Join(dir, "probe.c"))
	build = append(build, gccFortifySilence(t, cc)...)
	build = append(build, srcs...)
	build = append(build, "-lm", "-o", out)
	if got, err := testproc.Command(t, cc, build...).CombinedOutput(); err != nil {
		t.Fatalf("compile %s: %v\n%s", name, err, got)
	}
	return out
}

func TestCCountedMinimumHolds(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "functional test: invokes the C compiler (cc) and runs compiled C probes")
	for _, c := range []struct{ name, schema, loose, shape string }{
		{"arm", cArmMinSchema, cArmMinLooseSchema, cArmMinShape},
		{"plain", cPlainMinSchema, cPlainMinLooseSchema, cPlainMinShape},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			cCountedMinimumHolds(t, c.schema, c.loose, c.shape)
		})
	}
}

func cCountedMinimumHolds(t *testing.T, schema, looseSchema, shape string) {
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()

	release := cArmMinBuild(t, schema, shape, "", "release", "-DNDEBUG")
	if out, err := testproc.CommandContext(t, ctx, release, "read").CombinedOutput(); err != nil {
		t.Fatalf("a read accepted a count below the minimum: %v\n%s", err, out)
	}

	loose := cArmMinBuild(t, looseSchema, shape, "", "loose", "-DNDEBUG")
	work := t.TempDir()
	announce := filepath.Join(work, "loose.announce")
	messages := filepath.Join(work, "loose.messages")
	if out, err := testproc.CommandContext(t, ctx, loose, "message-write", announce, messages).CombinedOutput(); err != nil {
		t.Fatalf("the [..4] writer did not write: %v\n%s", err, out)
	}
	if out, err := testproc.CommandContext(t, ctx, release, "message-read", announce, messages).CombinedOutput(); err != nil {
		t.Fatalf("message: %v\n%s", err, out)
	}

	debug := cArmMinBuild(t, schema, shape, "", "debug")
	if out, err := testproc.CommandContext(t, ctx, debug, "fresh").CombinedOutput(); err != nil {
		t.Errorf("a fresh value is not lawful: %v\n%s", err, out)
	}
	for _, writer := range []string{"wire", "fixed", "message", "cook"} {
		out, err := testproc.CommandContext(t, ctx, debug, "write", writer).CombinedOutput()
		if err == nil || strings.Contains(string(out), "SAVED") {
			t.Errorf("the %s writer produced a count below the minimum: %v\n%s", writer, err, out)
		}
	}
	if out, err := testproc.CommandContext(t, ctx, debug, "write", "json").CombinedOutput(); err != nil {
		t.Errorf("the text writer did not refuse a count below the minimum: %v\n%s", err, out)
	}
}
