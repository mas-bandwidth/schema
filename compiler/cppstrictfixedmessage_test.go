// THE CPP FIXED AND MESSAGE READS ARE STRICT BY THEIR PLAIN NAMES
// (docs/SPEC-TABLES.md §6.5). The plain FixedLoad answers -1 and LoadMessages
// answers ok == false on a read that is clamped, kind-mismatched or malformed,
// and the value they wrote is reset; <T>FixedLoadTolerant and
// <T>LoadMessagesTolerant are the named twins with the tolerant answer.
//
// THE ORACLE IS GO (schema-go d808c3f54). Every expected answer below is the
// one internal/codegen/gotable/strictdefault_test.go TestGoDefaultReadIsStrict
// pins for the same schema text and the same input bytes, with the case cited
// beside it; the clamp rows are the ones
// internal/codegen/gotable/fixedversioning_count_clamp_ends_test.go pins.
package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpptable"
	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const cppStrictFixedNew = `package probe
fixed table Fix { n int32 = 1 | min = 0, max = 10
 m int32 = 2 }
`

const cppStrictFixedOld = `package probeold
fixed table Fix { n float32
 m int32 = 2 }
`

func TestCPPFixedAndMessageReadsAreStrict(t *testing.T) {
	t.Parallel()
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("the generated read path is C++: no c++ on PATH")
	}
	dir := t.TempDir()
	oldDir := filepath.Join(dir, "old")
	newDir := filepath.Join(dir, "new")
	for _, d := range []string{oldDir, newDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// THE OLDER LAYOUT IS THE KIND MISMATCH: the writer's n is a float32 and
	// the reader's is an int32 (Go TestGoDefaultReadIsStrict, the fixed
	// kind_mismatch row, whose `older` schema is this one).
	oldU := unitFromSource(t, cppStrictFixedOld)
	oldFix := oldU.Tables["Fix"]
	if oldFix == nil {
		t.Fatal("the older unit declares no Fix")
	}
	oldEntries := ir.TableFixedWalkRoot(oldFix)
	oldLayout := ir.TableFixedLayoutBytes(oldEntries)
	lock := &lockfile.Unit{Tables: []lockfile.Table{{
		Decl: lockfile.DeclFixedTable,
		Name: "Fix",
		Lineage: []lockfile.LineageEntry{{
			Wire:   ir.TableFixedLayoutHash(oldLayout, oldFix),
			Layout: oldLayout,
			Record: ir.TableFixedTypeBytes(oldFix),
		}},
	}}}

	newU := unitFromSource(t, cppStrictFixedNew)
	oldFiles, err := New().Generate(oldU, "cpp", Options{})
	if err != nil {
		t.Fatalf("generate the older unit: %v", err)
	}
	newFiles, err := New().Generate(newU, "cpp", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	lineage, err := cpptable.GenerateLineage(newU, lock)
	if err != nil {
		t.Fatalf("generate lineage: %v", err)
	}
	for name, data := range lineage {
		newFiles[name] = data
	}
	writeCPPUnit(t, oldDir, oldFiles)
	writeCPPUnit(t, newDir, newFiles)

	oldFixed := filepath.Join(dir, "old.fixed")
	oldMessage := filepath.Join(dir, "old.message")
	oldAnnounce := filepath.Join(dir, "old.announce")
	writer := filepath.Join(dir, "writer.cpp")
	if err := os.WriteFile(writer, []byte(cppStrictOldWriter), 0o600); err != nil {
		t.Fatal(err)
	}
	runCPP(t, cxx, oldDir, writer, filepath.Join(dir, "writer"), oldFixed, oldMessage, oldAnnounce)

	reader := filepath.Join(dir, "reader.cpp")
	if err := os.WriteFile(reader, []byte(cppStrictReader), 0o600); err != nil {
		t.Fatal(err)
	}
	out := runCPP(t, cxx, newDir, reader, filepath.Join(dir, "reader"), oldFixed, oldMessage, oldAnnounce)
	if len(out) != 0 {
		t.Fatalf("the strict probe reported failures:\n%s", out)
	}
}

func writeCPPUnit(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func runCPP(t *testing.T, cxx, include, source, binary string, args ...string) []byte {
	t.Helper()
	serialize, err := filepath.Abs("../../serialize")
	if err != nil {
		t.Fatal(err)
	}
	build := exec.Command(cxx, "-std=c++17", "-O1", "-I"+include, "-I"+serialize, source, "-o", binary)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("compile %s: %v\n%s", filepath.Base(source), err, out)
	}
	out, err := exec.Command(binary, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("run %s: %v\n%s", filepath.Base(binary), err, out)
	}
	return out
}

const cppStrictOldWriter = `#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include "ProbeTable.h"

using namespace probeold;

static void write_file( const char * path, const uint8_t * data, int64_t bytes )
{
    FILE * f = fopen( path, "wb" );
    if ( f == NULL ) { printf( "cannot open %s\n", path ); exit( 1 ); }
    if ( fwrite( data, 1, (size_t) bytes, f ) != (size_t) bytes ) { printf( "short write %s\n", path ); exit( 1 ); }
    fclose( f );
}

int main( int argc, char ** argv )
{
    if ( argc != 4 ) { return 1; }
    static uint8_t fixed[1 << 16];
    static uint8_t batch[1 << 16];
    Fix value;
    FixReset( value );
    value.n = 1.5f;
    value.m = 2;
    const int64_t fixed_bytes = FixFixedSave( &value, 1, fixed, sizeof( fixed ) );
    if ( fixed_bytes < 0 ) { printf( "the older record did not save\n" ); return 1; }
    write_file( argv[1], fixed, fixed_bytes );

    static uint8_t announcement[1 << 16];
    const int64_t announced = Announce( announcement, sizeof( announcement ) );
    write_file( argv[3], announcement, announced );
    static TableMessageEntry entries[ kTableMessageEntriesHere ];
    TableVocabulary vocabulary( entries, kTableMessageEntriesHere );
    if ( !AnnounceRead( vocabulary, announcement, announced, NULL ) ) { printf( "the older announcement was refused\n" ); return 1; }
    TableReport report;
    const int64_t batch_bytes = FixSaveMessages( &value, 1, batch, sizeof( batch ), &report );
    if ( batch_bytes < 0 ) { printf( "the older batch did not save\n" ); return 1; }
    write_file( argv[2], batch, batch_bytes );
    return 0;
}
`

const cppStrictReader = `#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include "ProbeTable.h"

using namespace probe;

static int failures = 0;

static void check( bool ok, const char * what )
{
    if ( !ok ) { printf( "FAIL %s\n", what ); failures++; }
}

static int64_t read_file( const char * path, uint8_t * out, int64_t capacity )
{
    FILE * f = fopen( path, "rb" );
    if ( f == NULL ) { printf( "cannot open %s\n", path ); exit( 1 ); }
    const int64_t n = (int64_t) fread( out, 1, (size_t) capacity, f );
    fclose( f );
    return n;
}

static void defaults( Fix & v ) { FixReset( v ); }

static bool is_default( const Fix & v ) { return v.n == 1 && v.m == 2; }

static bool clean_report( const TableReport & r )
{
    return !r.refused && !r.malformed && r.clamped == 0 && r.kind_mismatch == 0 && r.widened == 0;
}

static void forge_int32( uint8_t * at, int32_t value )
{
    at[0] = (uint8_t) ( value & 0xFF );
    at[1] = (uint8_t) ( ( value >> 8 ) & 0xFF );
    at[2] = (uint8_t) ( ( value >> 16 ) & 0xFF );
    at[3] = (uint8_t) ( ( value >> 24 ) & 0xFF );
}

static void announce_to_itself( TableVocabulary & vocabulary )
{
    static uint8_t announcement[1 << 16];
    const int64_t announced = Announce( announcement, sizeof( announcement ) );
    if ( !AnnounceRead( vocabulary, announcement, announced, NULL ) ) { printf( "the announcement was refused\n" ); exit( 1 ); }
}

int main( int argc, char ** argv )
{
    if ( argc != 4 ) { return 1; }
    static uint8_t old_fixed[1 << 16];
    static uint8_t old_message[1 << 16];
    static uint8_t old_announcement[1 << 16];
    static uint8_t wire[1 << 16];
    const int64_t old_fixed_bytes = read_file( argv[1], old_fixed, sizeof( old_fixed ) );
    const int64_t old_message_bytes = read_file( argv[2], old_message, sizeof( old_message ) );
    const int64_t old_announcement_bytes = read_file( argv[3], old_announcement, sizeof( old_announcement ) );

    static TableFixedEntry plan[256];
    Fix value;
    Fix got;
    TableReport r;

    // CLEAN: the plain read is the read, and it says nothing (Go
    // TestGoDefaultReadIsStrict, the fixed clean row).
    defaults( value );
    value.n = 3;
    value.m = 4;
    const int64_t wire_bytes = FixFixedSave( &value, 1, wire, sizeof( wire ) );
    defaults( got );
    r = TableReport();
    int64_t n = FixFixedLoad( &got, 1, wire, wire_bytes, plan, 256, NULL, &r );
    check( n == 1 && got.n == 3 && got.m == 4 && clean_report( r ), "clean FixedLoad" );

    // CLAMPED: bits 3..6 of the copied body are 100, past max = 10 (Go
    // TestGoDefaultReadIsStrict, the fixed clamped row; its forged body is
    // field(4, {100,0,0,0}) and the tolerant read keeps m's default 2).
    Fix clamped_value;
    defaults( clamped_value );
    clamped_value.m = 2;
    static uint8_t clamped[1 << 16];
    const int64_t clamped_bytes = FixFixedSave( &clamped_value, 1, clamped, sizeof( clamped ) );
    const int64_t body_at = FixFixedMeasure( 1 ) - FixFixedRecordBytes + 8;
    forge_int32( clamped + body_at, 100 );

    defaults( got );
    r = TableReport();
    n = FixFixedLoad( &got, 1, clamped, clamped_bytes, plan, 256, NULL, &r );
    check( n == -1 && r.clamped == 1 && is_default( got ), "clamped FixedLoad refuses and resets every value it wrote" );

    defaults( got );
    TableReport tr = TableReport();
    n = FixFixedLoadTolerant( &got, 1, clamped, clamped_bytes, plan, 256, NULL, &tr );
    check( n == 1 && tr.clamped == 1 && got.n == 10 && got.m == 2,
           "clamped FixedLoadTolerant keeps the recovered record (Go, the tolerant half of the clamped row)" );

    // KIND MISMATCH: the older float32 layout, through the lineage plan (Go
    // TestGoDefaultReadIsStrict, the fixed kind_mismatch row).
    defaults( got );
    r = TableReport();
    n = FixFixedLoad( &got, 1, old_fixed, old_fixed_bytes, plan, 256, NULL, &r );
    check( n == -1 && r.kind_mismatch == 1 && is_default( got ),
           "kind-mismatched FixedLoad refuses and resets every value it wrote" );

    defaults( got );
    tr = TableReport();
    n = FixFixedLoadTolerant( &got, 1, old_fixed, old_fixed_bytes, plan, 256, NULL, &tr );
    check( n == 1 && tr.kind_mismatch == 1 && is_default( got ),
           "kind-mismatched FixedLoadTolerant keeps the recovered record (Go, the tolerant half of that row)" );

    // MALFORMED: one byte short (Go TestGoDefaultReadIsStrict, the fixed
    // truncated row).
    defaults( got );
    r = TableReport();
    n = FixFixedLoad( &got, 1, wire, wire_bytes - 1, plan, 256, NULL, &r );
    check( n == -1 && r.malformed && is_default( got ), "truncated FixedLoad refuses" );

    // LOADMESSAGES: a clean batch, a forged n past max (Go's forged[2] bits
    // 3..6), a kind-mismatched older batch, and one cut short (Go
    // TestGoDefaultReadIsStrict's fixed LoadMessages rows).
    static TableMessageEntry entries[ kTableMessageEntriesHere ];
    TableVocabulary vocabulary( entries, kTableMessageEntriesHere );
    announce_to_itself( vocabulary );
    // THE PEER'S ANNOUNCEMENT IS THE VOCABULARY A RECEIVER RESOLVES WITH: the
    // older writer's kinds are what say the reader's c is a mismatch (Go
    // runMessagePair, messageform_versioning_test.go).
    static TableMessageEntry old_entries[ kTableMessageEntriesHere ];
    TableVocabulary old_vocabulary( old_entries, kTableMessageEntriesHere );
    if ( !AnnounceRead( old_vocabulary, old_announcement, old_announcement_bytes, NULL ) )
    {
        printf( "the reader refused the older announcement\n" );
        return 1;
    }

    defaults( value );
    value.n = 3;
    value.m = 4;
    static uint8_t message[1 << 16];
    const int64_t message_bytes = FixSaveMessages( &value, 1, message, sizeof( message ), NULL );

    defaults( got );
    int64_t count = 1;
    r = TableReport();
    bool ok = FixLoadMessages( &got, &count, vocabulary, message, message_bytes, &r );
    check( ok && count == 1 && got.n == 3 && got.m == 4 && clean_report( r ), "clean LoadMessages" );

    Fix zero;
    defaults( zero );
    zero.n = 0;
    zero.m = 4;
    static uint8_t forged[1 << 16];
    const int64_t forged_bytes = FixSaveMessages( &zero, 1, forged, sizeof( forged ), NULL );
    forged[2] = (uint8_t) ( ( forged[2] & ~0x78 ) | 0x78 );

    defaults( got );
    count = 1;
    r = TableReport();
    ok = FixLoadMessages( &got, &count, vocabulary, forged, forged_bytes, &r );
    check( !ok && count == 0 && r.clamped == 1 && is_default( got ),
           "clamped LoadMessages refuses with count 0 and resets every value it wrote" );

    defaults( got );
    count = 1;
    tr = TableReport();
    ok = FixLoadMessagesTolerant( &got, &count, vocabulary, forged, forged_bytes, &tr );
    check( ok && count == 1 && tr.clamped == 1 && got.n == 10,
           "clamped LoadMessagesTolerant keeps the recovered value (Go, the tolerant half of the clamped row)" );

    defaults( got );
    count = 1;
    r = TableReport();
    ok = FixLoadMessages( &got, &count, old_vocabulary, old_message, old_message_bytes, &r );
    check( !ok && count == 0 && r.kind_mismatch == 1 && is_default( got ),
           "kind-mismatched LoadMessages refuses with count 0 and resets every value it wrote" );

    defaults( got );
    count = 1;
    tr = TableReport();
    ok = FixLoadMessagesTolerant( &got, &count, old_vocabulary, old_message, old_message_bytes, &tr );
    check( ok && count == 1 && tr.kind_mismatch == 1 && got.n == 1 && got.m == 2,
           "kind-mismatched LoadMessagesTolerant keeps the recovered value (Go, the tolerant half of that row)" );

    defaults( got );
    count = 1;
    r = TableReport();
    ok = FixLoadMessages( &got, &count, vocabulary, message, 1, &r );
    check( !ok && r.malformed, "truncated LoadMessages refuses" );

    if ( failures != 0 ) { printf( "%d strict read row(s) failed\n", failures ); return 1; }
    return 0;
}
`
