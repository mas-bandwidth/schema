// A DEPRECATED ENUM VARIANT READS AS THE FIELD DEFAULT AND IS REFUSED ON WRITE
// (docs/SPEC-TABLES.md §2.15), on the id-table wire, form 3, the message form
// and JSON/text, in the C++ port. The bytes are written by the SAME schema with
// the variant LIVE, so they are an older writer's; the same bytes read with the
// variant live yield it (the reversed witness), which proves the test sees the
// rule rather than a reader that never lands the variant at all. The Go
// reference is internal/codegen/gotable/variantdeprecated_test.go, and the
// shared oracle (internal/tablewire, internal/tabletext) reads the bytes this
// port wrote the same way this port reads them.
package cpptable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const cppVariantDeprecatedLiveSchema = `package probe
enum Ship {
    Fighter
    Bomber
    Scout
}
fixed table Root {
    ship  Ship = Scout
    fleet [..2]Ship
}
`

const cppVariantDeprecatedSchema = `package probe
enum Ship {
    Fighter
    Bomber | deprecated
    Scout
}
fixed table Root {
    ship  Ship = Scout
    fleet [..2]Ship
}
`

const cppVariantDeprecatedPreamble = `#include <cstdio>
#include <cstring>
#include <cstdint>
#include <string>
#include <vector>
#include "ProbeTable.h"
using namespace probe;

static const char * kDir = "__DIR__";

static bool spit( const char * name, const void * data, int64_t bytes )
{
    std::string path = std::string( kDir ) + "/" + name;
    FILE * f = fopen( path.c_str(), "wb" );
    if ( f == NULL ) { return false; }
    const bool ok = fwrite( data, 1, (size_t) bytes, f ) == (size_t) bytes;
    fclose( f );
    return ok;
}

static bool slurp( const char * name, std::vector<uint8_t> & out )
{
    std::string path = std::string( kDir ) + "/" + name;
    FILE * f = fopen( path.c_str(), "rb" );
    if ( f == NULL ) { return false; }
    fseek( f, 0, SEEK_END );
    const long n = ftell( f );
    fseek( f, 0, SEEK_SET );
    out.resize( (size_t) n );
    const bool ok = n == 0 || fread( out.data(), 1, (size_t) n, f ) == (size_t) n;
    fclose( f );
    return ok;
}

static bool quiet( const TableReport & r )
{
    return r.unknown == 0 && r.clamped == 0 && r.kind_mismatch == 0 && !r.malformed;
}

#define FAIL( ... ) do { printf( __VA_ARGS__ ); printf( "\n" ); return 1; } while ( 0 )
`

const cppVariantDeprecatedLiveMain = cppVariantDeprecatedPreamble + `
int main()
{
    Root v;
    RootReset( v );
    v.ship = Ship::Bomber;
    v.fleet_count = 1;
    v.fleet[0] = Ship::Bomber;

    // THE ID-TABLE WIRE (form 1).
    const int64_t n = RootMeasure( v );
    if ( n < 0 ) FAIL( "live measure refused" );
    std::vector<uint8_t> wire( (size_t) n );
    if ( RootSave( v, wire.data(), n ) != n ) FAIL( "live save" );
    if ( !spit( "wire", wire.data(), n ) ) FAIL( "write wire" );

    // FORM 3.
    const int64_t fn = RootFixedMeasure( 1 );
    std::vector<uint8_t> fixed( (size_t) fn );
    if ( RootFixedSave( &v, 1, fixed.data(), fn ) != fn ) FAIL( "live fixed save" );
    if ( !spit( "fixed", fixed.data(), fn ) ) FAIL( "write fixed" );

    // THE MESSAGE FORM, and the announcement that names its vocabulary.
    const int64_t mn = RootMeasureMessages( &v, 1, NULL );
    if ( mn < 0 ) FAIL( "live message measure refused" );
    std::vector<uint8_t> message( (size_t) mn );
    if ( RootSaveMessages( &v, 1, message.data(), mn, NULL ) != mn ) FAIL( "live message save" );
    if ( !spit( "message", message.data(), mn ) ) FAIL( "write message" );
    std::vector<uint8_t> announcement( (size_t) AnnounceMeasure() );
    if ( Announce( announcement.data(), AnnounceMeasure() ) != AnnounceMeasure() ) FAIL( "announce" );
    if ( !spit( "announce", announcement.data(), AnnounceMeasure() ) ) FAIL( "write announce" );

    // JSON/TEXT.
    const int64_t jn = RootToJsonMeasure( v );
    if ( jn < 0 ) FAIL( "live json measure refused" );
    std::vector<char> text( (size_t) jn );
    if ( RootToJson( v, text.data(), jn ) != jn ) FAIL( "live to json" );
    if ( !spit( "json", text.data(), jn ) ) FAIL( "write json" );

    // THE REVERSED WITNESS: the same bytes read with Bomber LIVE yield Bomber
    // on every form, and count nothing.
    {
        Root w; TableReport r;
        if ( !RootLoad( w, wire.data(), n, &r ) || !quiet( r ) || w.ship != Ship::Bomber || w.fleet_count != 1 || w.fleet[0] != Ship::Bomber )
            FAIL( "live wire witness: ship=%d fleet0=%d count=%d unknown=%d", (int) w.ship, (int) w.fleet[0], (int) w.fleet_count, (int) r.unknown );
    }
    {
        Root records[1]; TableReport r;
        static TableFixedEntry plan[4096];
        if ( RootFixedLoad( records, 1, fixed.data(), fn, plan, 4096, NULL, &r ) != 1 || !quiet( r ) || records[0].ship != Ship::Bomber || records[0].fleet[0] != Ship::Bomber )
            FAIL( "live fixed witness: ship=%d fleet0=%d", (int) records[0].ship, (int) records[0].fleet[0] );
    }
    {
        static TableMessageEntry entries[kTableMessageEntriesHere];
        TableVocabulary vocabulary( entries, kTableMessageEntriesHere );
        TableReport r;
        if ( !AnnounceRead( vocabulary, announcement.data(), AnnounceMeasure(), &r ) ) FAIL( "live announce read" );
        Root decoded[1]; int64_t count = 1; r = TableReport();
        if ( !RootLoadMessages( decoded, &count, vocabulary, message.data(), mn, &r ) || count != 1 || !quiet( r ) || decoded[0].ship != Ship::Bomber || decoded[0].fleet[0] != Ship::Bomber )
            FAIL( "live message witness: ship=%d fleet0=%d", (int) decoded[0].ship, (int) decoded[0].fleet[0] );
    }
    {
        Root jv; TableReport r;
        if ( !RootFromJson( jv, text.data(), jn, &r ) || !quiet( r ) || jv.ship != Ship::Bomber || jv.fleet[0] != Ship::Bomber )
            FAIL( "live json witness: ship=%d fleet0=%d", (int) jv.ship, (int) jv.fleet[0] );
    }
    printf( "LIVE OK\n" );
    return 0;
}
`

const cppVariantDeprecatedReadMain = cppVariantDeprecatedPreamble + `
int main()
{
    std::vector<uint8_t> wire, fixed, message, announcement, text;
    if ( !slurp( "wire", wire ) || !slurp( "fixed", fixed ) || !slurp( "message", message ) || !slurp( "announce", announcement ) || !slurp( "json", text ) )
        FAIL( "read the live writer's files" );

    // THE ID-TABLE WIRE: the field reads its declared default (Scout), the
    // array element reads the ELEMENT default (None), and nothing counts.
    {
        Root v; TableReport r;
        if ( !RootLoad( v, wire.data(), (int64_t) wire.size(), &r ) ) FAIL( "deprecated wire refused" );
        if ( !quiet( r ) ) FAIL( "deprecated wire counted an event: unknown=%d clamped=%d", (int) r.unknown, (int) r.clamped );
        if ( v.ship != Ship::Scout || v.fleet_count != 1 || v.fleet[0] != Ship::None )
            FAIL( "deprecated wire: ship=%d fleet0=%d count=%d", (int) v.ship, (int) v.fleet[0], (int) v.fleet_count );
    }
    // FORM 3.
    {
        Root records[1]; TableReport r;
        static TableFixedEntry plan[4096];
        const int64_t got = RootFixedLoad( records, 1, fixed.data(), (int64_t) fixed.size(), plan, 4096, NULL, &r );
        if ( got != 1 ) FAIL( "deprecated fixed: %lld", (long long) got );
        if ( !quiet( r ) ) FAIL( "deprecated fixed counted an event: unknown=%d clamped=%d", (int) r.unknown, (int) r.clamped );
        if ( records[0].ship != Ship::Scout || records[0].fleet[0] != Ship::None )
            FAIL( "deprecated fixed: ship=%d fleet0=%d", (int) records[0].ship, (int) records[0].fleet[0] );
    }
    // THE MESSAGE FORM.
    {
        static TableMessageEntry entries[kTableMessageEntriesHere];
        TableVocabulary vocabulary( entries, kTableMessageEntriesHere );
        TableReport r;
        if ( !AnnounceRead( vocabulary, announcement.data(), (int64_t) announcement.size(), &r ) ) FAIL( "deprecated announce read" );
        Root decoded[1]; int64_t count = 1; r = TableReport();
        if ( !RootLoadMessages( decoded, &count, vocabulary, message.data(), (int64_t) message.size(), &r ) || count != 1 ) FAIL( "deprecated message refused" );
        if ( !quiet( r ) ) FAIL( "deprecated message counted an event: unknown=%d", (int) r.unknown );
        if ( decoded[0].ship != Ship::Scout || decoded[0].fleet[0] != Ship::None )
            FAIL( "deprecated message: ship=%d fleet0=%d", (int) decoded[0].ship, (int) decoded[0].fleet[0] );
    }
    // JSON/TEXT.
    {
        Root jv; TableReport r;
        if ( !RootFromJson( jv, (const char *) text.data(), (int64_t) text.size(), &r ) ) FAIL( "deprecated json refused" );
        if ( !quiet( r ) ) FAIL( "deprecated json counted an event: unknown=%d", (int) r.unknown );
        if ( jv.ship != Ship::Scout || jv.fleet[0] != Ship::None )
            FAIL( "deprecated json: ship=%d fleet0=%d", (int) jv.ship, (int) jv.fleet[0] );
    }

    // EVERY WRITER REFUSES A VALUE HOLDING THE DEPRECATED VARIANT.
    static uint8_t buffer[1 << 16];
    {
        Root bad; RootReset( bad ); bad.ship = Ship::Bomber;
        if ( RootMeasure( bad ) != -1 ) FAIL( "Measure accepted a deprecated variant" );
        if ( RootSave( bad, buffer, sizeof( buffer ) ) != -1 ) FAIL( "Save accepted a deprecated variant" );
        if ( RootFixedSave( &bad, 1, buffer, sizeof( buffer ) ) != -1 ) FAIL( "FixedSave accepted a deprecated variant" );
        if ( RootMeasureMessages( &bad, 1, NULL ) != -1 ) FAIL( "MeasureMessages accepted a deprecated variant" );
        if ( RootSaveMessages( &bad, 1, buffer, sizeof( buffer ), NULL ) != -1 ) FAIL( "SaveMessages accepted a deprecated variant" );
        if ( RootToJsonMeasure( bad ) != -1 ) FAIL( "ToJsonMeasure accepted a deprecated variant" );
        if ( RootToJson( bad, (char *) buffer, sizeof( buffer ) ) != -1 ) FAIL( "ToJson accepted a deprecated variant" );
        if ( RootCook( bad, buffer, sizeof( buffer ), TableByteOrder::Little ) ) FAIL( "Cook accepted a deprecated variant" );
    }
    // An ARRAY element is the caller's value too, on every writer.
    {
        Root arr; RootReset( arr ); arr.fleet_count = 1; arr.fleet[0] = Ship::Bomber;
        if ( RootMeasure( arr ) != -1 ) FAIL( "Measure accepted a deprecated array element" );
        if ( RootSave( arr, buffer, sizeof( buffer ) ) != -1 ) FAIL( "Save accepted a deprecated array element" );
        if ( RootFixedSave( &arr, 1, buffer, sizeof( buffer ) ) != -1 ) FAIL( "FixedSave accepted a deprecated array element" );
        if ( RootMeasureMessages( &arr, 1, NULL ) != -1 ) FAIL( "MeasureMessages accepted a deprecated array element" );
        if ( RootSaveMessages( &arr, 1, buffer, sizeof( buffer ), NULL ) != -1 ) FAIL( "SaveMessages accepted a deprecated array element" );
        if ( RootToJson( arr, (char *) buffer, sizeof( buffer ) ) != -1 ) FAIL( "ToJson accepted a deprecated array element" );
        if ( RootCook( arr, buffer, sizeof( buffer ), TableByteOrder::Little ) ) FAIL( "Cook accepted a deprecated array element" );
    }
    // A LIVE VARIANT STILL WRITES under the deprecated schema: the refusal is
    // the deprecated variant's alone.
    {
        Root ok; RootReset( ok ); ok.ship = Ship::Fighter; ok.fleet_count = 1; ok.fleet[0] = Ship::Scout;
        if ( RootSave( ok, buffer, sizeof( buffer ) ) <= 0 ) FAIL( "Save refused a live variant" );
        if ( RootFixedSave( &ok, 1, buffer, sizeof( buffer ) ) <= 0 ) FAIL( "FixedSave refused a live variant" );
        if ( RootSaveMessages( &ok, 1, buffer, sizeof( buffer ), NULL ) <= 0 ) FAIL( "SaveMessages refused a live variant" );
        if ( RootToJson( ok, (char *) buffer, sizeof( buffer ) ) <= 0 ) FAIL( "ToJson refused a live variant" );
        if ( !RootCook( ok, buffer, sizeof( buffer ), TableByteOrder::Little ) ) FAIL( "Cook refused a live variant" );
    }
    printf( "DEPRECATED OK\n" );
    return 0;
}
`

func TestDeprecatedVariantReadsDefaultWriteRefused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	out, err := runCppGeneratedEdited(t, cppVariantDeprecatedLiveSchema, strings.Replace(cppVariantDeprecatedLiveMain, "__DIR__", dir, 1), nil)
	if err != nil || !strings.Contains(string(out), "LIVE OK") {
		t.Fatalf("live writer and reversed witness: %v\n%s", err, out)
	}
	out, err = runCppGeneratedEdited(t, cppVariantDeprecatedSchema, strings.Replace(cppVariantDeprecatedReadMain, "__DIR__", dir, 1), func(files map[string][]byte) {
		header := string(files["Probe.h"])
		if !strings.Contains(header, `Bomber [[deprecated("retired schema variant")]] = 2`) {
			t.Fatal("C++ retired constant lacks deprecation annotation at its original ordinal")
		}
		if strings.Contains(header, `Fighter [[deprecated`) || strings.Contains(header, `Scout [[deprecated`) {
			t.Fatal("C++ live constant was marked deprecated")
		}
	})
	if err != nil || !strings.Contains(string(out), "DEPRECATED OK") {
		t.Fatalf("deprecated reader and writers: %v\n%s", err, out)
	}

	// THE ORACLE AGREES: internal/tablewire and internal/tabletext read the
	// bytes the C++ port wrote as the field default, with nothing counted, so
	// this port and the Go reference read one older writer the same way.
	t.Run("oracle", func(t *testing.T) {
		t.Parallel()
		u := cppUnitFrom(t, cppVariantDeprecatedSchema)
		m := tabletext.NewModel(u)
		scout := uint64(tabletext.EnumValue(u.Enums["Ship"], "Scout"))
		quiet := func(r tabletext.Report) bool {
			return r.Unknown == 0 && r.Clamped == 0 && r.KindMismatch == 0 && !r.Malformed
		}

		wire, err := os.ReadFile(filepath.Join(dir, "wire"))
		if err != nil {
			t.Fatal(err)
		}
		inst := m.New(m.Lookup("Root"))
		var report tabletext.Report
		if ok, err := tablewire.Decode(m, inst, wire, &report); !ok || err != nil || !quiet(report) {
			t.Fatalf("oracle wire: %v %v %+v", ok, err, report)
		}
		if inst.Fields[0].Cell.U != scout || inst.Fields[1].Elems[0].U != 0 {
			t.Fatalf("oracle wire: ship=%d fleet0=%d", inst.Fields[0].Cell.U, inst.Fields[1].Elems[0].U)
		}

		text, err := os.ReadFile(filepath.Join(dir, "json"))
		if err != nil {
			t.Fatal(err)
		}
		inst = m.New(m.Lookup("Root"))
		report = tabletext.Report{}
		if !m.Read(inst, text, &report) || !quiet(report) {
			t.Fatalf("oracle text: %+v", report)
		}
		if inst.Fields[0].Cell.U != scout || inst.Fields[1].Elems[0].U != 0 {
			t.Fatalf("oracle text: ship=%d fleet0=%d", inst.Fields[0].Cell.U, inst.Fields[1].Elems[0].U)
		}

		message, err := os.ReadFile(filepath.Join(dir, "message"))
		if err != nil {
			t.Fatal(err)
		}
		announcement, err := os.ReadFile(filepath.Join(dir, "announce"))
		if err != nil {
			t.Fatal(err)
		}
		var vocab tablewire.Vocabulary
		report = tabletext.Report{}
		if err := vocab.AnnounceRead(announcement, &report); err != nil {
			t.Fatalf("oracle announcement: %v %+v", err, report)
		}
		inst = m.New(m.Lookup("Root"))
		report = tabletext.Report{}
		if n, ok, err := tablewire.DecodeMessages(m, []*tabletext.Instance{inst}, message, &vocab, &report); err != nil || !ok || n != 1 || !quiet(report) {
			t.Fatalf("oracle message: %d %v %v %+v", n, ok, err, report)
		}
		if inst.Fields[0].Cell.U != scout || inst.Fields[1].Elems[0].U != 0 {
			t.Fatalf("oracle message: ship=%d fleet0=%d", inst.Fields[0].Cell.U, inst.Fields[1].Elems[0].U)
		}
	})
}
