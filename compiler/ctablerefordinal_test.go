// THE ORDINAL SLOT CACHE IN C, HELD TO MAIN'S BYTES (docs/SPEC-TABLES.md §3).
//
// The C twin of compiler/tablerefordinal_test.go. Every id a generated field
// header or union arm header names is a COMPILE-TIME CONSTANT of the unit, so
// the emitter can hand the runtime the id's ORDINAL in the unit's vocabulary
// (ir.TableWireIds) beside the id itself. table_writer_id_at answers a repeat
// from slot[ordinal] and falls through to table_writer_id on a miss; the
// APPEND still happens only in table_writer_id, so FIRST-USE ORDER — which is
// the trailer's order — is the order it always was.
//
// THAT IS THE CLAIM, AND THE ONLY PROOF WORTH HAVING IS THE BYTES.
// testdata/crefordinal/golden.txt was produced by main's C emitter, which has
// no cache at all, over this schema and these values (regenerate with
// SCHEMA_UPDATE_CREFORDINAL_GOLDEN=1, which is how it was pinned); this test
// generates the sources with THIS emitter, saves the same values and compares.
//
// WHAT THE VALUES REACH FOR, and why each one is here:
//
//   - ELIDED NESTED TABLES: every third element's `leaf` is entirely default,
//     which is the elision that INTERNS ids and then REWINDS them back (the
//     nested-table default probe in internal/codegen/ctable/wire.go). A cached
//     slot that survived a rewind would hand back a reference to a POPPED
//     entry, and the file would name the wrong id at that header — this is the
//     interaction the cache could break;
//   - AN ENUM-KEYED ARRAY OF TABLES, held at every shape: all slots default
//     (the whole field elides after interning its own id and its keys), some
//     slots default (a per-slot rewind inside a field that does ride) and none
//     default. The keyed rewind is the one that pops a key interned through
//     the general path beside elements interned through the cache, so both
//     kinds of entry are popped in one call;
//   - A UNION FIELD, whose arm name is a compile-time id of its own, and an
//     ENUM FIELD, whose variant id is a RUNTIME value and stays on
//     table_writer_id;
//   - A NESTED TABLE OF 140 FIELDS, so the id table crosses 127 entries part
//     way through the array and a reference's LEB128 spelling grows from one
//     byte to two WHILE the elements are being written. A cache that answered
//     with a stale index would move a length there, not only an id.
//
// The driver also holds the save to its own contract at each value:
// <t>_save's length is <t>_measure's, a load and a re-save reproduce the bytes,
// and an undersized buffer answers -1 rather than a short file — the last
// under -DNDEBUG, because the `overflow` flag is not a debug check.
package compiler

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// cRefOrdinalWideFields is the nested table's field count. It is above 127 on
// purpose: the id table's references are LEB128, so a table closure that
// interns more than 127 entries is the only place a reference's WIDTH moves.
const cRefOrdinalWideFields = 140

func cRefOrdinalSchema() string {
	var b strings.Builder
	b.WriteString("package refordinal\n\n")
	b.WriteString("enum Slot { Low, High, Mid }\n\n")
	b.WriteString("// 140 fields, so the id table crosses 127 entries mid-array\n")
	b.WriteString("fixed table Wide\n{\n")
	for i := range cRefOrdinalWideFields {
		fmt.Fprintf(&b, "    f%03d int32\n", i)
	}
	b.WriteString("}\n\n")
	b.WriteString("fixed table Inner\n{\n    a    int32\n    leaf Wide\n    z    int32\n}\n\n")
	b.WriteString("union Pick\n{\n    alpha Inner\n    beta  int32\n}\n\n")
	b.WriteString("fixed table Outer\n{\n")
	b.WriteString("    many  [..96]Inner\n")
	b.WriteString("    banks [Slot]Inner\n")
	b.WriteString("    few   [..8]Inner\n")
	b.WriteString("    slots [4]Inner\n")
	b.WriteString("    pick  Pick\n")
	b.WriteString("    grade Slot\n")
	b.WriteString("    tag   string(16)\n")
	b.WriteString("}\n")
	return b.String()
}

func cRefOrdinalDriver() string {
	var b strings.Builder
	b.WriteString(`#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "ProbeTable.h"

static int failures = 0;
static uint8_t buffer[ 1 << 18 ];
static uint8_t twin[ 1 << 18 ];
static Outer value;
static Outer back;

static void set_wide( Wide * w, int which, int32_t v )
{
    switch ( which )
    {
`)
	for i := range cRefOrdinalWideFields {
		fmt.Fprintf(&b, "        case %d: w->f%03d = v; break;\n", i, i)
	}
	b.WriteString(`        default: break;
    }
}

/* banks_shape: 0 = every slot default (the field elides whole), 1 = the middle
   slot alone rides (a per-slot rewind inside a live field), 2 = every slot
   rides */
static void build( Outer * v, int32_t many_count, int32_t few_count, int banks_shape )
{
    int32_t i;
    const char * tag = "refordinal";
    outer_reset( v );
    v->many_count = many_count;
    for ( i = 0; i < many_count; i++ )
    {
        v->many[i].a = i + 1;
        if ( i % 3 != 0 )
        {
            /* three CONSECUTIVE wide fields per element, walking forward with
               i: the closure's 140 ids are interned progressively, so the id
               table passes 127 entries PART WAY THROUGH this array and every
               reference after that point is two LEB128 bytes where the ones
               before it were one */
            set_wide( &v->many[i].leaf, (int) ( ( i * 3 ) % 140 ), i + 1 );
            set_wide( &v->many[i].leaf, (int) ( ( i * 3 + 1 ) % 140 ), i + 2 );
            set_wide( &v->many[i].leaf, (int) ( ( i * 3 + 2 ) % 140 ), i + 3 );
        }
        /* i % 3 == 0 leaves the nested table ENTIRELY DEFAULT: it elides, and
           the walk that decided so interned its ids and gave them back */
        if ( i % 5 == 0 ) { v->many[i].z = i + 3; }
    }
    v->few_count = few_count;
    for ( i = 0; i < few_count; i++ )
    {
        v->few[i].a = 100 + i;
        if ( i % 2 == 0 ) { set_wide( &v->few[i].leaf, (int) ( ( i * 17 ) % 140 ), i + 5 ); }
    }
    for ( i = 0; i < 4; i++ )
    {
        if ( i % 2 == 0 ) { v->slots[i].a = 200 + i; }
        else { set_wide( &v->slots[i].leaf, (int) ( ( i * 23 ) % 140 ), i + 7 ); }
    }
    if ( banks_shape == 1 )
    {
        SCHEMA_TABLE_KEYED_AT( v->banks, SLOT_HIGH, SLOT_MAX ).a = 11;
        set_wide( &SCHEMA_TABLE_KEYED_AT( v->banks, SLOT_HIGH, SLOT_MAX ).leaf, 3, 12 );
    }
    else if ( banks_shape == 2 )
    {
        SCHEMA_TABLE_KEYED_AT( v->banks, SLOT_LOW, SLOT_MAX ).a = 21;
        SCHEMA_TABLE_KEYED_AT( v->banks, SLOT_HIGH, SLOT_MAX ).z = 22;
        set_wide( &SCHEMA_TABLE_KEYED_AT( v->banks, SLOT_MID, SLOT_MAX ).leaf, 130, 23 );
    }
    /* the union arm's own id, and an enum variant's, which stays on the
       general path */
    if ( many_count % 2 == 0 )
    {
        v->pick.type = PICK_TYPE_ALPHA;
        v->pick.as.alpha.a = 5;
        set_wide( &v->pick.as.alpha.leaf, 129, 6 );
    }
    else
    {
        v->pick.type = PICK_TYPE_BETA;
        v->pick.as.beta = 7;
    }
    v->grade = ( few_count % 2 == 0 ) ? SLOT_MID : SLOT_LOW;
    memcpy( v->tag, tag, strlen( tag ) );
    v->tag_length = (int32_t) strlen( tag );
}

static void one( int32_t many_count, int32_t few_count, int banks_shape )
{
    int64_t wrote, measured, again, i;
    TableReport report;

    build( &value, many_count, few_count, banks_shape );

    wrote = outer_save( &value, buffer, (int64_t) sizeof( buffer ) );
    if ( wrote < 0 ) { printf( "SAVE FAILED %d/%d/%d\n", many_count, few_count, banks_shape ); failures++; return; }

    measured = outer_measure( &value );
    if ( measured != wrote )
    {
        printf( "MEASURE DISAGREES %d/%d/%d: measure %lld save %lld\n",
                many_count, few_count, banks_shape, (long long) measured, (long long) wrote );
        failures++;
    }

    /* a load and a re-save reproduce the file, which is what says the id
       table's order and the references into it agree with the bytes */
    memset( &report, 0, sizeof( report ) );
    if ( !outer_load( &back, buffer, wrote, &report ) )
    {
        printf( "LOAD FAILED %d/%d/%d\n", many_count, few_count, banks_shape ); failures++;
    }
    else
    {
        again = outer_save( &back, twin, (int64_t) sizeof( twin ) );
        if ( again != wrote || memcmp( twin, buffer, (size_t) wrote ) != 0 )
        {
            printf( "ROUND TRIP MOVED %d/%d/%d\n", many_count, few_count, banks_shape ); failures++;
        }
    }

    /* A SECOND SAVE FROM THE SAME VALUE: the id table is a local of the save,
       so two saves of one value must agree byte for byte */
    if ( outer_save( &value, twin, (int64_t) sizeof( twin ) ) != wrote ||
         memcmp( twin, buffer, (size_t) wrote ) != 0 )
    {
        printf( "SECOND SAVE MOVED %d/%d/%d\n", many_count, few_count, banks_shape ); failures++;
    }

    /* THE OVERFLOW FLAG IS NOT A DEBUG CHECK (this driver compiles -DNDEBUG):
       a buffer one byte short answers -1, and so does an empty one. */
    if ( outer_save( &value, twin, wrote - 1 ) != -1 )
    {
        printf( "SHORT BUFFER ACCEPTED %d/%d/%d\n", many_count, few_count, banks_shape ); failures++;
    }
    if ( outer_save( &value, twin, 0 ) != -1 )
    {
        printf( "EMPTY BUFFER ACCEPTED %d/%d/%d\n", many_count, few_count, banks_shape ); failures++;
    }

    printf( "many=%d few=%d banks=%d len=%lld ", many_count, few_count, banks_shape, (long long) wrote );
    for ( i = 0; i < wrote; i++ ) { printf( "%02x", buffer[i] ); }
    printf( "\n" );
}

int main( void )
{
    static const int32_t counts[] = { 0, 1, 2, 3, 8, 63, 64, 65, 80, 96 };
    unsigned k;
    int banks_shape;
    for ( k = 0; k < sizeof( counts ) / sizeof( counts[0] ); k++ )
    {
        for ( banks_shape = 0; banks_shape < 3; banks_shape++ )
        {
            one( counts[k], 0, banks_shape );
            one( counts[k], 5, banks_shape );
            one( counts[k], 8, banks_shape );
        }
    }
    return failures == 0 ? 0 : 1;
}
`)
	return b.String()
}

// runCRefOrdinal writes the generated sources and one driver beside them,
// compiles at -O2 -DNDEBUG and answers the driver's stdout. The driver lives
// BESIDE the header: an #include "..." searches the including file's own
// directory first, and a driver written elsewhere would quietly compile
// against some other tree's copy.
func runCRefOrdinal(t *testing.T, files map[string][]byte, driver, binary, runFail string) string {
	t.Helper()
	slowtest.Gate(t, "the C compiler (cc)")
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("the generated save path is C: no cc on PATH")
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	main := filepath.Join(dir, "main.c")
	if err := os.WriteFile(main, []byte(driver), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, binary)
	// -DNDEBUG on purpose: the release build is where a write-side check may
	// compile out, and the overflow flag must not
	args := []string{"-std=c99", "-O2", "-DNDEBUG", "-Wall", "-Wextra", "-Werror", "-Wshadow", "-I", dir, main}
	for name := range files {
		if strings.HasSuffix(name, ".c") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	args = append(args, "-lm", "-o", bin)
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin).CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", runFail, err, out)
	}
	return string(out)
}

// TestCTableRefOrdinalBytes holds the generated C save path to the bytes main's
// C emitter wrote, over elided nested tables, an enum-keyed array at every
// elision shape, and an array long enough to move a reference's LEB128 width.
func TestCTableRefOrdinalBytes(t *testing.T) {
	files, err := New().Generate(unitFromSource(t, cRefOrdinalSchema()), "c", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	// the runtime the emitter grew for THIS schema has to carry the cache at
	// all, or the comparison below would pass by measuring nothing. Not while
	// PINNING: the pin is taken from the emitter that had no cache.
	if os.Getenv("SCHEMA_UPDATE_CREFORDINAL_GOLDEN") != "1" {
		header := string(files["ProbeTable.h"])
		for _, want := range []string{
			"int32_t slot[",
			"ordinal_of[",
			"void table_writer_id_at( TableWriter * w, int32_t ordinal, uint64_t id )",
			"table_writer_header_at( w, ",
		} {
			if !strings.Contains(header, want) {
				t.Fatalf("the generated save path does not carry %q — this test would prove nothing", want)
			}
		}
		// and the RUNTIME ids must still reach the general path: an enum's
		// variant is a value, not a compile-time constant of the header
		if !strings.Contains(header, "table_writer_id( w, 0x") {
			t.Fatal("no general-path table_writer_id survives: the enum identity's variant ids must not be cached by ordinal")
		}
	}
	out := runCRefOrdinal(t, files, cRefOrdinalDriver(), "crefordinal", "the save path refused its own value")
	golden := filepath.Join("testdata", "crefordinal", "golden.txt")
	if os.Getenv("SCHEMA_UPDATE_CREFORDINAL_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(golden), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(out), 0600); err != nil {
			t.Fatal(err)
		}
		t.Logf("pinned %s (%d bytes)", golden, len(out))
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read %s: %v", golden, err)
	}
	if out != string(want) {
		gotLines, wantLines := strings.Split(out, "\n"), strings.Split(string(want), "\n")
		for i := range gotLines {
			if i >= len(wantLines) {
				t.Fatalf("the save path grew a line the pin does not have: %s", gotLines[i])
			}
			if gotLines[i] != wantLines[i] {
				t.Fatalf("THE SAVED BYTES MOVED at line %d\n  pinned: %s\n     got: %s", i+1, wantLines[i], gotLines[i])
			}
		}
		t.Fatalf("the save path lost a line the pin has: %s", wantLines[len(gotLines)])
	}
}

// AN ID BOTH PATHS CAN REACH (docs/SPEC-TABLES.md §3, §5). A field's wire id
// and an enum variant's are the same hash over the same name, so a schema that
// spells one name in both places gives ONE id two call sites:
// table_writer_id_at, from the field header, and table_writer_id, from the
// enum identity's switch on a runtime value. table_writer_id_at records the
// ordinal beside the entry on BOTH outcomes, so an entry the general path
// appended still carries an ordinal the rewind can undo the cache for.
//
// AND THAT RECORDING IS AN INVARIANT THIS EMITTER CANNOT CURRENTLY BE MADE TO
// NEED, which is why it has no negative control of its own where C++'s
// tables-ref-ordinal-shared-negative-control does. In C++ an enum-keyed array
// interns its KEY before it measures the slot's element, so the element's
// ref_at only hits and a truncate can leave the cache naming a popped entry.
// In C the key is interned AFTER the slot's element probe and only for a slot
// that rides, and — the general rule — every rewind here that pops an entry is
// a FRAME PROBE immediately followed by an identical REDO of the same region,
// while the `check_default` probes that decide elision intern nothing at all.
// A general-path append and the table_writer_id_at that hits it therefore
// always sit inside one rewound region in that order, and the redo re-appends
// at the same index before any id_at can read the cache. See make/c.mk beside
// tables-c-ref-ordinal-negative-control for the evidence.
//
// What this test still holds is the SHAPE: one id with both call sites, an
// enum-keyed array whose key `alpha` is also the name of the element's only
// field, one slot all-default so it elides and the next slot live — measure
// and save agreeing, the value surviving the round trip, and the re-save
// reproducing the file.
const cRefOrdinalCollisionSchema = `package collide

enum Tag { alpha, beta }

fixed table Wide
{
    w0 int32
    w1 int32
}

fixed table Leaf
{
    alpha Wide
}

fixed table Root
{
    t     Tag
    banks [Tag]Leaf
    n     int32
}
`

const cRefOrdinalCollisionDriver = `#include <stdio.h>
#include <string.h>
#include "ProbeTable.h"

static uint8_t buffer[ 4096 ];
static uint8_t twin[ 4096 ];
static Root value;
static Root back;

int main( void )
{
    int64_t wrote, measured, again;
    TableReport report;

    root_reset( &value );
    value.t = TAG_NONE;                                                  /* nothing interns "alpha" at the top */
    SCHEMA_TABLE_KEYED_AT( value.banks, TAG_ALPHA, TAG_MAX ).alpha.w0 = 0; /* all default: the slot ELIDES, and its key and its field id are popped together */
    SCHEMA_TABLE_KEYED_AT( value.banks, TAG_BETA, TAG_MAX ).alpha.w0 = 7;  /* and this one rides, re-interning "alpha" AFTER the pop */
    value.n = 3;

    wrote = root_save( &value, buffer, (int64_t) sizeof( buffer ) );
    measured = root_measure( &value );
    if ( wrote < 0 || measured != wrote )
    {
        printf( "MEASURE/SAVE DISAGREE save %lld measure %lld\n", (long long) wrote, (long long) measured );
        return 1;
    }

    memset( &report, 0, sizeof( report ) );
    if ( !root_load( &back, buffer, wrote, &report ) )
    {
        printf( "LOAD FAILED\n" );
        return 1;
    }
    /* THE TELL: a stale slot names the following key's entry, so the field is
       written under an id no reader resolves to it and the value is lost */
    if ( SCHEMA_TABLE_KEYED_AT( back.banks, TAG_BETA, TAG_MAX ).alpha.w0 != 7 || back.n != 3 )
    {
        printf( "VALUES MOVED w0=%d n=%d\n",
                SCHEMA_TABLE_KEYED_AT( back.banks, TAG_BETA, TAG_MAX ).alpha.w0, back.n );
        return 1;
    }
    again = root_save( &back, twin, (int64_t) sizeof( twin ) );
    if ( again != wrote || memcmp( twin, buffer, (size_t) wrote ) != 0 )
    {
        printf( "ROUND TRIP MOVED\n" );
        return 1;
    }
    printf( "OK len=%lld\n", (long long) wrote );
    return 0;
}
`

// TestCTableRefOrdinalSharedId holds the shared-id shape to its bytes: one id
// reached by both call sites, an eliding keyed slot beside a live one, and the
// value still there after a load and a re-save.
func TestCTableRefOrdinalSharedId(t *testing.T) {
	files, err := New().Generate(unitFromSource(t, cRefOrdinalCollisionSchema), "c", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	// the two call sites have to be there, on ONE id, or the shape this test
	// exists for is not in the sources at all
	header := string(files["ProbeTable.h"])
	const shared = "0x8ac625bb85ed202bull" // TableWireId( "alpha" )
	if !strings.Contains(header, "table_writer_header_at( w, 5, "+shared+", ") || !strings.Contains(header, "table_writer_id( w, "+shared+" )") {
		t.Fatalf("the schema no longer gives one id both a field header and an enum variant: this test would prove nothing")
	}
	out := runCRefOrdinal(t, files, cRefOrdinalCollisionDriver, "ccollide",
		"an id the general path interned first is not undone by the rewind")
	t.Logf("%s", strings.TrimSpace(out))
}
