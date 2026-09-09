// THE ORDINAL SLOT CACHE, HELD TO MAIN'S BYTES (docs/SPEC-TABLES.md §3).
//
// Every id a generated field header names is a COMPILE-TIME CONSTANT of the
// unit, so the emitter can hand TableIds::ref the id's ordinal in the unit's
// vocabulary (ir.TableWireIds) beside the id itself. ref_at answers a repeat
// from slot[ordinal] and falls through to ref on a miss; the APPEND still
// happens only in ref, so FIRST-USE ORDER — which is the trailer's order — is
// the order it always was.
//
// THAT IS THE CLAIM, AND THE ONLY PROOF WORTH HAVING IS THE BYTES.
// testdata/refordinal/golden.txt was produced by main's emitter, which has no
// cache at all, over this schema and these values (regenerate with
// SCHEMA_UPDATE_REFORDINAL_GOLDEN=1, which is how it was pinned); this test
// generates the header with THIS emitter, saves the same values and compares.
//
// WHAT THE VALUES REACH FOR, and why each one is here:
//
//   - ELIDED NESTED TABLES: every third element's `leaf` is entirely default,
//     which is the elision that INTERNS ids and then TRUNCATES them back
//     (internal/codegen/cpptable/codecs.go, the nested-table measure and the
//     nested-table write). A cached slot that survived a truncate would hand
//     back a reference to a POPPED entry, and the file would name the wrong
//     id at that header — this is the interaction the cache could break;
//   - AN ENUM-KEYED ARRAY OF TABLES, held at every shape: all slots default
//     (pairs == 0, the whole field elides after interning its own id and its
//     keys), some slots default (a per-slot truncate inside a field that does
//     ride) and none default. The keyed truncate is the one that pops a key
//     interned through the general path beside elements interned through the
//     cache, so both kinds of entry are popped in one call;
//   - A UNION FIELD, whose arm name is a compile-time id of its own, and an
//     ENUM FIELD, whose variant id is a RUNTIME value and stays on ref;
//   - A NESTED TABLE OF 140 FIELDS, so the id table crosses 127 entries part
//     way through the array and a reference's LEB128 spelling grows from one
//     byte to two WHILE the elements are being written. A cache that answered
//     with a stale index would move a length there, not only an id.
//
// The driver also holds the save to its own contract at each value: Save's
// length is Measure's, a load and a re-save reproduce the bytes, and an
// undersized buffer answers -1 rather than a short file — the last under
// -DNDEBUG, because the `overflow` flag is not a debug check.
package compiler

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// refOrdinalWideFields is the nested table's field count. It is above 127 on
// purpose: the id table's references are LEB128, so a table closure that
// interns more than 127 entries is the only place a reference's WIDTH moves.
const refOrdinalWideFields = 140

func refOrdinalSchema() string {
	var b strings.Builder
	b.WriteString("package refordinal\n\n")
	b.WriteString("enum Slot { Low, High, Mid }\n\n")
	b.WriteString("// 140 fields, so the id table crosses 127 entries mid-array\n")
	b.WriteString("table Wide\n{\n")
	for i := range refOrdinalWideFields {
		fmt.Fprintf(&b, "    f%03d int32\n", i)
	}
	b.WriteString("}\n\n")
	b.WriteString("table Inner\n{\n    a    int32\n    leaf Wide\n    z    int32\n}\n\n")
	b.WriteString("union Pick\n{\n    alpha Inner\n    beta  int32\n}\n\n")
	b.WriteString("table Outer\n{\n")
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

func refOrdinalDriver() string {
	var b strings.Builder
	b.WriteString(`#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "ProbeTable.h"

using namespace refordinal;

static int failures = 0;
static uint8_t buffer[ 1 << 18 ];
static uint8_t twin[ 1 << 18 ];
static Outer value;
static Outer back;

static void set_wide( Wide & w, int which, int32_t v )
{
    switch ( which )
    {
`)
	for i := range refOrdinalWideFields {
		fmt.Fprintf(&b, "        case %d: w.f%03d = v; break;\n", i, i)
	}
	b.WriteString(`        default: break;
    }
}

// banks_shape: 0 = every slot default (pairs == 0, the field elides whole),
// 1 = the middle slot alone rides (a per-slot truncate inside a live field),
// 2 = every slot rides
static void build( Outer & v, int32_t many_count, int32_t few_count, int banks_shape )
{
    OuterReset( v );
    v.many_count = many_count;
    for ( int32_t i = 0; i < many_count; i++ )
    {
        v.many[i].a = i + 1;
        if ( i % 3 != 0 )
        {
            // three CONSECUTIVE wide fields per element, walking forward with
            // i: the closure's 140 ids are interned progressively, so the id
            // table passes 127 entries PART WAY THROUGH this array and every
            // reference after that point is two LEB128 bytes where the ones
            // before it were one
            set_wide( v.many[i].leaf, ( i * 3 ) % 140, i + 1 );
            set_wide( v.many[i].leaf, ( i * 3 + 1 ) % 140, i + 2 );
            set_wide( v.many[i].leaf, ( i * 3 + 2 ) % 140, i + 3 );
        }
        // i % 3 == 0 leaves the nested table ENTIRELY DEFAULT: it elides, and
        // the walk that decided so interned its ids and gave them back
        if ( i % 5 == 0 ) { v.many[i].z = i + 3; }
    }
    v.few_count = few_count;
    for ( int32_t i = 0; i < few_count; i++ )
    {
        v.few[i].a = 100 + i;
        if ( i % 2 == 0 ) { set_wide( v.few[i].leaf, ( i * 17 ) % 140, i + 5 ); }
    }
    for ( int32_t i = 0; i < 4; i++ )
    {
        if ( i % 2 == 0 ) { v.slots[i].a = 200 + i; }
        else { set_wide( v.slots[i].leaf, ( i * 23 ) % 140, i + 7 ); }
    }
    if ( banks_shape == 1 )
    {
        v.banks[ Slot::High ].a = 11;
        set_wide( v.banks[ Slot::High ].leaf, 3, 12 );
    }
    else if ( banks_shape == 2 )
    {
        v.banks[ Slot::Low ].a = 21;
        v.banks[ Slot::High ].z = 22;
        set_wide( v.banks[ Slot::Mid ].leaf, 130, 23 );
    }
    // the union arm's own id, and an enum variant's, which stays on ref
    if ( many_count % 2 == 0 )
    {
        v.pick.type = PickType::Alpha;
        v.pick.alpha.a = 5;
        set_wide( v.pick.alpha.leaf, 129, 6 );
    }
    else
    {
        v.pick.type = PickType::Beta;
        v.pick.beta = 7;
    }
    v.grade = ( few_count % 2 == 0 ) ? Slot::Mid : Slot::Low;
    const char * tag = "refordinal";
    memcpy( v.tag, tag, strlen( tag ) );
    v.tag_length = (int32_t) strlen( tag );
}

static void one( int32_t many_count, int32_t few_count, int banks_shape )
{
    build( value, many_count, few_count, banks_shape );

    const int64_t wrote = OuterSave( value, buffer, (int64_t) sizeof( buffer ) );
    if ( wrote < 0 ) { printf( "SAVE FAILED %d/%d/%d\n", many_count, few_count, banks_shape ); failures++; return; }

    const int64_t measured = OuterMeasure( value );
    if ( measured != wrote )
    {
        printf( "MEASURE DISAGREES %d/%d/%d: measure %lld save %lld\n",
                many_count, few_count, banks_shape, (long long) measured, (long long) wrote );
        failures++;
    }

    // a load and a re-save reproduce the file, which is what says the id
    // table's order and the references into it agree with the bytes
    TableReport report;
    if ( !OuterLoad( back, buffer, wrote, &report ) )
    {
        printf( "LOAD FAILED %d/%d/%d\n", many_count, few_count, banks_shape ); failures++;
    }
    else
    {
        const int64_t again = OuterSave( back, twin, (int64_t) sizeof( twin ) );
        if ( again != wrote || memcmp( twin, buffer, (size_t) wrote ) != 0 )
        {
            printf( "ROUND TRIP MOVED %d/%d/%d\n", many_count, few_count, banks_shape ); failures++;
        }
    }

    // A SECOND SAVE FROM THE SAME LIVE TableIds-FREE STATE: the cache is a
    // local of Save, so two saves of one value must agree byte for byte
    if ( OuterSave( value, twin, (int64_t) sizeof( twin ) ) != wrote ||
         memcmp( twin, buffer, (size_t) wrote ) != 0 )
    {
        printf( "SECOND SAVE MOVED %d/%d/%d\n", many_count, few_count, banks_shape ); failures++;
    }

    // THE OVERFLOW FLAG IS NOT A DEBUG CHECK (this driver compiles -DNDEBUG):
    // a buffer one byte short answers -1, and so does an empty one.
    if ( OuterSave( value, twin, wrote - 1 ) != -1 )
    {
        printf( "SHORT BUFFER ACCEPTED %d/%d/%d\n", many_count, few_count, banks_shape ); failures++;
    }
    if ( OuterSave( value, twin, 0 ) != -1 )
    {
        printf( "EMPTY BUFFER ACCEPTED %d/%d/%d\n", many_count, few_count, banks_shape ); failures++;
    }

    printf( "many=%d few=%d banks=%d len=%lld ", many_count, few_count, banks_shape, (long long) wrote );
    for ( int64_t i = 0; i < wrote; i++ ) { printf( "%02x", buffer[i] ); }
    printf( "\n" );
}

int main()
{
    static const int32_t counts[] = { 0, 1, 2, 3, 8, 63, 64, 65, 80, 96 };
    for ( unsigned k = 0; k < sizeof( counts ) / sizeof( counts[0] ); k++ )
    {
        for ( int banks_shape = 0; banks_shape < 3; banks_shape++ )
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

// TestCppTableRefOrdinalBytes holds the generated save path to the bytes main's
// emitter wrote, over elided nested tables, an enum-keyed array at every
// elision shape, and an array long enough to move a reference's LEB128 width.
func TestCppTableRefOrdinalBytes(t *testing.T) {
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("the generated save path is C++: no c++ on PATH")
	}
	files, err := New().Generate(unitFromSource(t, refOrdinalSchema()), "cpp", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	// the header the emitter grew for THIS schema has to carry the cache at
	// all, or the comparison below would pass by measuring nothing. Not while
	// PINNING: the pin is taken from the emitter that had no cache.
	if os.Getenv("SCHEMA_UPDATE_REFORDINAL_GOLDEN") != "1" {
		header := string(files["ProbeTable.h"])
		for _, want := range []string{
			"int32_t slot[ kCapacity ]",
			"ordinal_of[ kCapacity ]",
			"uint64_t ref_at( int32_t ordinal, uint64_t id )",
			"ids.ref_at(",
		} {
			if !strings.Contains(header, want) {
				t.Fatalf("the generated save path does not carry %q — this test would prove nothing", want)
			}
		}
		// and the RUNTIME ids must still reach the general path: an enum's
		// variant is a value, not a compile-time constant of the header
		if !strings.Contains(header, "ids.ref( 0x") {
			t.Fatal("no general-path ref survives: the enum identity's variant ids must not be cached by ordinal")
		}
	}
	main := filepath.Join(dir, "main.cpp")
	if err := os.WriteFile(main, []byte(refOrdinalDriver()), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "refordinal")
	// -DNDEBUG on purpose: the release build is where a write-side check may
	// compile out, and the overflow flag must not
	args := []string{
		"-std=c++17", "-O2", "-DNDEBUG", "-Wall", "-Wextra", "-Werror", "-Wshadow",
		"-ffp-contract=off", "-I", dir, main, "-o", bin,
	}
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	golden := filepath.Join("testdata", "refordinal", "golden.txt")
	if os.Getenv("SCHEMA_UPDATE_REFORDINAL_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(golden), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, out, 0600); err != nil {
			t.Fatal(err)
		}
		t.Logf("pinned %s (%d bytes)", golden, len(out))
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read %s: %v", golden, err)
	}
	if string(out) != string(want) {
		gotLines, wantLines := strings.Split(string(out), "\n"), strings.Split(string(want), "\n")
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
// spells one name in both places gives ONE id two call sites: ref_at, from the
// field header, and ref, from TableEnumRef's switch on a runtime value.
//
// That id can therefore be interned by ref FIRST — an enum-keyed array interns
// its key before it measures the slot's element — and the ref_at that follows
// only HITS. If the hit did not record the ordinal beside the entry, truncate
// would have nothing to clear when the slot elides, and the NEXT slot's field
// header would answer out of a stale cache: it would name the entry the popped
// one was replaced by, which is the following KEY. The file stays
// self-consistent and the value is silently gone.
//
// The schema below is the smallest shape that reaches it: an enum-keyed array
// whose key `alpha` is also the name of the element's only field, one slot
// all-default so it elides, and the next slot live.
const refOrdinalCollisionSchema = `package collide

enum Tag { alpha, beta }

table Wide
{
    w0 int32
    w1 int32
}

table Leaf
{
    alpha Wide
}

table Root
{
    t     Tag
    banks [Tag]Leaf
    n     int32
}
`

const refOrdinalCollisionDriver = `#include <stdio.h>
#include <string.h>
#include "ProbeTable.h"

using namespace collide;

static uint8_t buffer[ 4096 ];
static uint8_t twin[ 4096 ];

int main()
{
    Root value;
    RootReset( value );
    value.t = Tag::None;                    // nothing interns "alpha" at the top
    value.banks[ Tag::alpha ].alpha.w0 = 0; // all default: the slot ELIDES, and its key and its field id are popped together
    value.banks[ Tag::beta  ].alpha.w0 = 7; // and this one rides, re-interning "alpha" AFTER the pop
    value.n = 3;

    const int64_t wrote = RootSave( value, buffer, (int64_t) sizeof( buffer ) );
    const int64_t measured = RootMeasure( value );
    if ( wrote < 0 || measured != wrote )
    {
        printf( "MEASURE/SAVE DISAGREE save %lld measure %lld\n", (long long) wrote, (long long) measured );
        return 1;
    }

    Root back;
    TableReport report;
    if ( !RootLoad( back, buffer, wrote, &report ) )
    {
        printf( "LOAD FAILED\n" );
        return 1;
    }
    // THE TELL: a stale slot names the following key's entry, so the field is
    // written under an id no reader resolves to it and the value is lost
    if ( back.banks[ Tag::beta ].alpha.w0 != 7 || back.n != 3 )
    {
        printf( "VALUES MOVED w0=%d n=%d\n", back.banks[ Tag::beta ].alpha.w0, back.n );
        return 1;
    }
    const int64_t again = RootSave( back, twin, (int64_t) sizeof( twin ) );
    if ( again != wrote || memcmp( twin, buffer, (size_t) wrote ) != 0 )
    {
        printf( "ROUND TRIP MOVED\n" );
        return 1;
    }
    printf( "OK len=%lld\n", (long long) wrote );
    return 0;
}
`

// TestCppTableRefOrdinalSharedId holds ref_at to recording the ordinal on the
// HIT path as well as the miss, which is what lets truncate undo a cache entry
// for an id the general path interned first.
func TestCppTableRefOrdinalSharedId(t *testing.T) {
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("the generated save path is C++: no c++ on PATH")
	}
	files, err := New().Generate(unitFromSource(t, refOrdinalCollisionSchema), "cpp", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	// the two call sites have to be there, on ONE id, or the shape this test
	// exists for is not in the header at all
	header := string(files["ProbeTable.h"])
	const shared = "0x8ac625bb85ed202bull" // TableWireId( "alpha" )
	if !strings.Contains(header, "ids.ref_at( 5, "+shared+" )") || !strings.Contains(header, "ids.ref( "+shared+" )") {
		t.Fatalf("the schema no longer gives one id both a field header and an enum variant: this test would prove nothing")
	}
	// the driver must live BESIDE the header: an #include "..." searches the
	// including file's own directory first, and a driver written elsewhere
	// would quietly compile against some other tree's copy
	main := filepath.Join(dir, "main.cpp")
	if err := os.WriteFile(main, []byte(refOrdinalCollisionDriver), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "collide")
	args := []string{
		"-std=c++17", "-O2", "-DNDEBUG", "-Wall", "-Wextra", "-Werror", "-Wshadow",
		"-ffp-contract=off", "-I", dir, main, "-o", bin,
	}
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("an id the general path interned first is not undone by truncate: %v\n%s", err, out)
	}
	t.Logf("%s", strings.TrimSpace(string(out)))
}
