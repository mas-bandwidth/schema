// THE OPTIONAL MAP IN THE C++ REFERENCE (docs/SPEC-TABLES.md §2.8): ?map[K]V
// carries a presence companion, and ABSENT, PRESENT-EMPTY and
// PRESENT-WITH-ENTRIES are three values on the wire, in JSON, through
// retain-unknown and the cook, with the descriptor naming the field optional
// and its present offset. The answers are the ones schema-go pins in
// internal/codegen/gotable/mappresence_test.go TestGoMapPresence (goldenAbsent,
// goldenEmpty, goldenEntries and the three JSON texts); this probe transcribes
// them, never a value the C++ port computed for itself.
package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const cppMapPresenceSchema = `package probe

table M
{
    id   int32
    tags ?map[string(8)]int32
}
`

// cppMapPresencePlainSchema is the same declarations without the `?`: the same
// field names, so the same wire ids, and the reader §2.8 says reads the
// optional writer's bytes as a plain map.
const cppMapPresencePlainSchema = `package probe

table M
{
    id   int32
    tags map[string(8)]int32
}
`

// cppMapPresenceGoldens are the wires and JSON texts Go's TestGoMapPresence
// pins for the same schema and the same three values.
const cppMapPresenceGoldens = `
static const char * golden_absent = "0101040100000000c03a5cb5072eb7080100000000000000";
static const char * golden_empty = "01010401000000020e020d0000c03a5cb5072eb70880856f07ef3b1cd90200000000000000";
static const char * golden_entries = "01010401000000020e0e0d010b030c01610404050000000000c03a5cb5072eb70880856f07ef3b1cd9ec105b36194ac93dea0ce83094fde47c0400000000000000";
`

const cppMapPresenceProbe = `#include "ProbeTable.h"
#include <cctype>
#include <cstdio>
#include <cstdlib>
#include <cstring>
using namespace probe;
#define CHECK(x) do { if (!(x)) { fprintf(stderr,"line %d: %s\n",__LINE__,#x); return 1; } } while(0)
` + cppMapPresenceGoldens + `
static bool hexes( const uint8_t * data, int64_t n, const char * hex )
{
    if ( (int64_t) strlen( hex ) != n * 2 ) { return false; }
    for ( int64_t i = 0; i < n; i++ )
    {
        const int hi = hex[i*2] <= '9' ? hex[i*2]-'0' : hex[i*2]-'a'+10;
        const int lo = hex[i*2+1] <= '9' ? hex[i*2+1]-'0' : hex[i*2+1]-'a'+10;
        if ( data[i] != (uint8_t) ( hi * 16 + lo ) ) { return false; }
    }
    return true;
}
static const M * build( MBuilder & b, int shape )
{
    M * v = b.GetRoot();
    v->id = 1;
    if ( shape >= 1 ) { v->tags_present = true; }
    if ( shape == 2 )
    {
        int32_t * p = MTagsInsert( b.main, v->tags, "a" );
        if ( p == NULL ) { return NULL; }
        *p = 5;
    }
    if ( !b.Lock() ) { return NULL; }
    return b.AsConst();
}
static int describe( const M * v )
{
    if ( !v->tags_present ) { return v->tags.count == 0 ? 0 : -1; }
    if ( v->tags.count == 0 ) { return 1; }
    const int32_t * p = v->tags.Find( "a" );
    if ( v->tags.count != 1 || p == NULL || *p != 5 ) { return -2; }
    return 2;
}
static void compact( char * dst, const char * src )
{
    bool in = false; size_t j = 0;
    for ( size_t i = 0; src[i] != 0; i++ )
    {
        const char c = src[i];
        if ( c == '"' ) { in = !in; }
        if ( !in && isspace( (unsigned char) c ) ) { continue; }
        dst[j++] = c;
    }
    dst[j] = 0;
}
int main()
{
    const char * goldens[3] = { golden_absent, golden_empty, golden_entries };
    const char * jsons[3] = { "{\"id\":1}", "{\"id\":1,\"tags\":{}}", "{\"id\":1,\"tags\":{\"a\":5}}" };
    alignas( 64 ) static uint8_t wire[1<<16];
    alignas( 64 ) static uint8_t twin[1<<16];
    alignas( 64 ) static uint8_t cooked[1<<16];
    for ( int shape = 0; shape < 3; shape++ )
    {
        MBuilder b;
        const M * v = build( b, shape );
        CHECK( v != NULL );
        const int64_t n = MSave( v, wire, sizeof( wire ) );
        CHECK( n >= 0 && hexes( wire, n, goldens[shape] ) );
        CHECK( MMeasure( v ) == n );
        // THE BINARY WIRE round trip reads back the same one of three
        uint8_t * region = (uint8_t *) malloc( 1 << 16 );
        CHECK( region != NULL );
        TableReport report = {};
        const M * loaded = MLoad( region, 1 << 16, wire, n, &report );
        CHECK( loaded != NULL && !report.malformed && !report.refused );
        CHECK( describe( loaded ) == shape );
        CHECK( MSave( loaded, twin, sizeof( twin ) ) == n && memcmp( twin, wire, (size_t) n ) == 0 );
        free( region );
        // THE JSON TEXT: absent omits the key, empty is {} (Go TestGoMapPresence)
        const int64_t jn = MToJsonMeasure( v );
        CHECK( jn >= 0 );
        char text[4096], packed[4096];
        CHECK( MToJson( v, text, sizeof( text ) ) == jn );
        compact( packed, text );
        CHECK( strcmp( packed, jsons[shape] ) == 0 );
        MBuilder jb;
        TableReport jr = {};
        CHECK( MFromJson( jb, text, jn, &jr ) );
        CHECK( !jr.malformed && !jr.refused && jr.unknown == 0 && jr.kind_mismatch == 0 && jr.duplicate == 0 );
        {
            // A BUILDER'S MAP IS SEGMENTED until Lock: the builder's own Find
            // (MTagsFind) is the lookup here, not the const form's FindEntry.
            int readback = jb.GetRoot()->tags_present ? ( jb.GetRoot()->tags.count == 0 ? 1 : 2 ) : ( jb.GetRoot()->tags.count == 0 ? 0 : -1 );
            if ( readback == 2 ) { const int32_t * p = MTagsFind( jb.arena, jb.GetRoot()->tags, "a" ); if ( jb.GetRoot()->tags.count != 1 || p == NULL || *p != 5 ) { readback = -2; } }
            CHECK( readback == shape );
        }
        CHECK( jb.Lock() );
        CHECK( MSave( jb.AsConst(), twin, sizeof( twin ) ) == n && memcmp( twin, wire, (size_t) n ) == 0 );
        // RETAIN-UNKNOWN keeps the presence and writes the same bytes back
        uint8_t store[8192];
        TableRetain::Id ids[64];
        TableRetain retain;
        retain.bytes = store; retain.capacity = sizeof( store );
        retain.ids = ids; retain.id_capacity = 64;
        const int64_t need = MLoadMeasure( wire, n );
        CHECK( need > 0 );
        uint8_t * kept_region = (uint8_t *) malloc( (size_t) need );
        CHECK( kept_region != NULL );
        TableReport rr = {};
        const M * kept = MLoadRetain( kept_region, need, wire, n, &retain, &rr );
        CHECK( kept != NULL && !rr.malformed && !rr.refused );
        CHECK( describe( kept ) == shape );
        const int64_t rn = MSaveRetain( kept, &retain, twin, sizeof( twin ), &rr );
        CHECK( rn == n && memcmp( twin, wire, (size_t) n ) == 0 );
        free( kept_region );
        // THE COOKED FORM keeps the presence
        const int64_t cn = MCookMeasure( v );
        CHECK( cn > 0 && cn <= (int64_t) sizeof( cooked ) );
        CHECK( MCook( v, cooked, (uint64_t) cn, TableByteOrder::Little ) );
        const M * opened = MOpen( cooked, (uint64_t) cn );
        CHECK( opened != NULL && describe( opened ) == shape );
    }
    // THE VIEW (§8.1): the descriptor names the field optional, its present
    // companion where the struct holds it (Go TestGoMapPresence's descriptor case)
    const TableFieldInfo * f = &MTableType()->fields[1];
    CHECK( strcmp( f->name, "tags" ) == 0 );
    CHECK( f->optional && f->present_offset == (uint32_t) offsetof( M, tags_present ) );
    // A JSON null IS ABSENT (§16.2), and writes the absent wire
    {
        MBuilder nb;
        TableReport nr = {};
        const char * null_text = "{\"id\":1,\"tags\":null}";
        CHECK( MFromJson( nb, null_text, (int64_t) strlen( null_text ), &nr ) );
        CHECK( describe( nb.GetRoot() ) == 0 );
        CHECK( nb.Lock() );
        const int64_t n = MSave( nb.AsConst(), wire, sizeof( wire ) );
        CHECK( n >= 0 && hexes( wire, n, golden_absent ) );
    }
    // a repeated key whose last value is null leaves the map absent
    {
        MBuilder rb;
        TableReport rr = {};
        const char * repeated = "{\"id\":1,\"tags\":{\"a\":5},\"tags\":null}";
        CHECK( MFromJson( rb, repeated, (int64_t) strlen( repeated ), &rr ) );
        CHECK( rr.duplicate == 1 );
        CHECK( describe( rb.GetRoot() ) == 0 );
    }
    return 0;
}
`

const cppMapPresencePlainProbe = `#include "ProbeTable.h"
#include <cstdio>
#include <cstring>
using namespace probe;
#define CHECK(x) do { if (!(x)) { fprintf(stderr,"line %d: %s\n",__LINE__,#x); return 1; } } while(0)
` + cppMapPresenceGoldens + `
static bool hexes( const uint8_t * data, int64_t n, const char * hex )
{
    if ( (int64_t) strlen( hex ) != n * 2 ) { return false; }
    for ( int64_t i = 0; i < n; i++ )
    {
        const int hi = hex[i*2] <= '9' ? hex[i*2]-'0' : hex[i*2]-'a'+10;
        const int lo = hex[i*2+1] <= '9' ? hex[i*2+1]-'0' : hex[i*2+1]-'a'+10;
        if ( data[i] != (uint8_t) ( hi * 16 + lo ) ) { return false; }
    }
    return true;
}
int main()
{
    // a reader built WITHOUT the question mark reads the three silently as plain maps
    const char * goldens[3] = { golden_absent, golden_empty, golden_entries };
    const int32_t counts[3] = { 0, 0, 1 };
    alignas( 64 ) static uint8_t wire[1<<16];
    for ( int shape = 0; shape < 3; shape++ )
    {
        uint8_t * region = (uint8_t *) malloc( 1 << 16 );
        CHECK( region != NULL );
        const int64_t n = (int64_t) strlen( goldens[shape] ) / 2;
        for ( int64_t i = 0; i < n; i++ )
        {
            const int hi = goldens[shape][i*2] <= '9' ? goldens[shape][i*2]-'0' : goldens[shape][i*2]-'a'+10;
            const int lo = goldens[shape][i*2+1] <= '9' ? goldens[shape][i*2+1]-'0' : goldens[shape][i*2+1]-'a'+10;
            wire[i] = (uint8_t) ( hi * 16 + lo );
        }
        TableReport report = {};
        const M * got = MLoad( region, 1 << 16, wire, n, &report );
        CHECK( got != NULL && !report.malformed && !report.refused );
        CHECK( got->id == 1 && got->tags.count == counts[shape] );
        if ( counts[shape] == 1 )
        {
            const int32_t * p = got->tags.Find( "a" );
            CHECK( p != NULL && *p == 5 );
        }
        free( region );
    }
    // THE PLAIN WRITER: an empty plain map elides, and one with the entry writes
    // the optional map's bytes (Go TestPlainReader)
    for ( int shape = 0; shape < 2; shape++ )
    {
        MBuilder b;
        M * v = b.GetRoot();
        v->id = 1;
        if ( shape == 1 ) { *MTagsInsert( b.main, v->tags, "a" ) = 5; }
        CHECK( b.Lock() );
        const int64_t n = MSave( b.AsConst(), wire, sizeof( wire ) );
        CHECK( n >= 0 && hexes( wire, n, shape == 0 ? golden_absent : golden_entries ) );
    }
    return 0;
}
`

// TestCPPMapPresence holds the C++ reference to the three values Go's
// TestGoMapPresence pins: the wires, the JSON, retain-unknown, the cook, the
// descriptor, and the plain reader's silent reading of all three.
func TestCPPMapPresence(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C++ compiler")
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("the generated C++ is compiled and run: no c++ on PATH")
	}
	run := func(name, schema, driver string) {
		t.Helper()
		files, err := New().Generate(unitFromSource(t, schema), "cpp", Options{})
		if err != nil {
			t.Fatalf("%s: generate: %v", name, err)
		}
		dir := t.TempDir()
		for file, data := range files {
			if err := os.WriteFile(filepath.Join(dir, file), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		main := filepath.Join(dir, "main.cpp")
		if err := os.WriteFile(main, []byte(driver), 0600); err != nil {
			t.Fatal(err)
		}
		bin := filepath.Join(dir, "probe")
		args := []string{"-std=c++17", "-O1", "-Wall", "-Wextra", "-Werror", "-I", dir, main}
		for file := range files {
			if strings.HasSuffix(file, ".cpp") {
				args = append(args, filepath.Join(dir, file))
			}
		}
		args = append(args, "-o", bin)
		if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
			t.Fatalf("%s: compile: %v\n%s", name, err, out)
		}
		if out, err := exec.Command(bin).CombinedOutput(); err != nil {
			t.Fatalf("%s: run: %v\n%s", name, err, out)
		}
	}
	run("optional map", cppMapPresenceSchema, cppMapPresenceProbe)
	run("plain reader", cppMapPresencePlainSchema, cppMapPresencePlainProbe)
}
