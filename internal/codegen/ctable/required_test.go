// The `| required` field marker (docs/SPEC-TABLES.md §2.3).
package ctable

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func runCProbe(t *testing.T, schema, probe string) {
	t.Helper()
	slowtest.Gate(t, "the C compiler")
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("cc not found")
	}
	u := unitFrom(t, schema)
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatalf("cgen: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	main := filepath.Join(dir, "main.c")
	if err := os.WriteFile(main, []byte(probe), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-I", dir, main}
	for file := range files {
		if strings.HasSuffix(file, ".c") {
			args = append(args, filepath.Join(dir, file))
		}
	}
	args = append(args, "-lm", "-o", bin)
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile failed: %v\n%s", err, out)
	}
	if out, err := exec.Command(bin).CombinedOutput(); err != nil {
		t.Fatalf("probe failed: %v\n%s", err, out)
	}
}

func TestCTableRequiredFieldRefusesWhenMissing(t *testing.T) {
	t.Parallel()
	const schema = `package probe

table T
{
    a int32
    b int32 | required
    c int32 | required
}

table OldT
{
    a int32
    b int32
    c int32
}
`
	probe := `#include <assert.h>
#include <stdint.h>
#include <string.h>
#include "ProbeTable.h"

int main( void )
{
    assert( t_table_type()->fields[0].required == 0 );
    assert( t_table_type()->fields[1].required == 1 );
    assert( t_table_type()->fields[2].required == 1 );

    /* Required int32s riding as zero ride the wire and read back. */
    TBuilder b;
    assert( t_builder_init( &b ) == 1 );
    T * root = t_builder_root( &b );
    root->a = 7;
    assert( t_builder_lock( &b ) == 1 );

    uint8_t wire[256];
    int64_t wn = t_save( NULL, (const T *) b.region, wire, sizeof( wire ) );
    assert( wn > 0 && wn == t_measure( NULL, (const T *) b.region ) );

    SCHEMA_C_ALIGN16 uint8_t region[512];
    TableReport rep;
    const T * got = t_load( region, sizeof( region ), wire, wn, &rep );
    assert( got != NULL );
    assert( got->a == 7 && got->b == 0 && got->c == 0 );
    assert( rep.missing_required == 0 && rep.refuse_reason == SCHEMA_TABLE_REFUSE_OK );
    t_builder_shutdown( &b );

    /* Older layout with a=7, b=0 (elided), c=4 -> b is missing. */
    OldTBuilder ob;
    assert( old_t_builder_init( &ob ) == 1 );
    OldT * oroot = old_t_builder_root( &ob );
    oroot->a = 7;
    oroot->c = 4;
    assert( old_t_builder_lock( &ob ) == 1 );

    uint8_t old_wire[256];
    int64_t own = old_t_save( NULL, (const OldT *) ob.region, old_wire, sizeof( old_wire ) );
    assert( own > 0 );
    old_t_builder_shutdown( &ob );

    /* Strict region load refuses with SCHEMA_TABLE_REFUSE_MISSING_REQUIRED. */
    memset( &rep, 0, sizeof( rep ) );
    got = t_load( region, sizeof( region ), old_wire, own, &rep );
    assert( got == NULL );
    assert( rep.missing_required == 1 && rep.refuse_reason == SCHEMA_TABLE_REFUSE_MISSING_REQUIRED );

    /* Builder load_builder returns 0 and sets SCHEMA_TABLE_REFUSE_MISSING_REQUIRED. */
    TBuilder lb;
    assert( t_builder_init( &lb ) == 1 );
    memset( &rep, 0, sizeof( rep ) );
    assert( t_load_builder( &lb, old_wire, own, &rep ) == 0 );
    assert( rep.refused == 1 && rep.missing_required == 1 );
    t_builder_shutdown( &lb );

    /* JSON missing b refuses with SCHEMA_TABLE_REFUSE_MISSING_REQUIRED. */
    const char * bad_json = "{\"a\":7,\"c\":4}";
    assert( t_builder_init( &lb ) == 1 );
    memset( &rep, 0, sizeof( rep ) );
    assert( t_from_json( &lb, bad_json, (int64_t) strlen( bad_json ), &rep ) == 0 );
    assert( rep.missing_required == 1 && rep.refuse_reason == SCHEMA_TABLE_REFUSE_MISSING_REQUIRED );
    t_builder_shutdown( &lb );

    return 0;
}
`
	runCProbe(t, schema, probe)
}

func TestCTableRequiredRefusalReachesStrictCallers(t *testing.T) {
	t.Parallel()
	const schema = `package probe

table Kid
{
    x int32 | required
    y int32
}

table OldKid
{
    y int32
}

table Root
{
    a int32 | required
    kid Kid
    ptr *Kid
}

table OldRootNoA
{
    kid Kid
}

table OldRootOldKid
{
    a int32
    kid OldKid
}

table OldRootOldPtr
{
    a int32
    kid Kid
    ptr *OldKid
}
`
	probe := `#include <assert.h>
#include <stdint.h>
#include <string.h>
#include "ProbeTable.h"

static void check_missing( const uint8_t * wire, int64_t wn )
{
    SCHEMA_C_ALIGN16 uint8_t region[1024];
    TableReport rep;

    memset( &rep, 0, sizeof( rep ) );
    const Root * got = root_load( region, sizeof( region ), wire, wn, &rep );
    assert( got == NULL );
    assert( rep.missing_required == 1 && rep.refuse_reason == SCHEMA_TABLE_REFUSE_MISSING_REQUIRED );

    RootBuilder b;
    assert( root_builder_init( &b ) == 1 );
    memset( &rep, 0, sizeof( rep ) );
    assert( root_load_builder( &b, wire, wn, &rep ) == 0 );
    assert( rep.refused == 1 && rep.missing_required == 1 );
    root_builder_shutdown( &b );
}

int main( void )
{
    /* Clean root and child at zero round-trip strictly. */
    RootBuilder rb;
    assert( root_builder_init( &rb ) == 1 );
    Root * r = root_builder_root( &rb );
    r->kid.y = 9;
    assert( root_builder_lock( &rb ) == 1 );

    uint8_t wire[512];
    int64_t wn = root_save( NULL, (const Root *) rb.region, wire, sizeof( wire ) );
    assert( wn > 0 );
    root_builder_shutdown( &rb );

    SCHEMA_C_ALIGN16 uint8_t region[1024];
    TableReport rep;
    const Root * got = root_load( region, sizeof( region ), wire, wn, &rep );
    assert( got != NULL && got->a == 0 && got->kid.x == 0 && got->kid.y == 9 );
    assert( rep.missing_required == 0 && rep.refuse_reason == SCHEMA_TABLE_REFUSE_OK );

    /* 1. Root missing a. */
    OldRootNoABuilder b1;
    assert( old_root_no_a_builder_init( &b1 ) == 1 );
    old_root_no_a_builder_root( &b1 )->kid.x = 5;
    assert( old_root_no_a_builder_lock( &b1 ) == 1 );
    wn = old_root_no_a_save( NULL, (const OldRootNoA *) b1.region, wire, sizeof( wire ) );
    assert( wn > 0 );
    old_root_no_a_builder_shutdown( &b1 );
    check_missing( wire, wn );

    /* 2. By-value child Kid missing x. */
    OldRootOldKidBuilder b2;
    assert( old_root_old_kid_builder_init( &b2 ) == 1 );
    OldRootOldKid * r2 = old_root_old_kid_builder_root( &b2 );
    r2->a = 3;
    r2->kid.y = 2;
    assert( old_root_old_kid_builder_lock( &b2 ) == 1 );
    wn = old_root_old_kid_save( NULL, (const OldRootOldKid *) b2.region, wire, sizeof( wire ) );
    assert( wn > 0 );
    old_root_old_kid_builder_shutdown( &b2 );
    check_missing( wire, wn );

    /* 3. Pointer child *Kid missing x (patch OldKid node type ID in vocabulary to Kid type ID). */
    OldRootOldPtrBuilder b3;
    assert( old_root_old_ptr_builder_init( &b3 ) == 1 );
    OldRootOldPtr * r3 = old_root_old_ptr_builder_root( &b3 );
    r3->a = 3;
    r3->kid.x = 1;
    TableSink sink = { NULL, &b3.main }; OldKid * pk = old_kid_emplace( &sink, &r3->ptr );
    assert( pk != NULL );
    pk->y = 4;
    assert( old_root_old_ptr_builder_lock( &b3 ) == 1 );
    wn = old_root_old_ptr_save( NULL, (const OldRootOldPtr *) b3.region, wire, sizeof( wire ) );
    assert( wn > 0 );
    old_root_old_ptr_builder_shutdown( &b3 );
    {
        uint64_t old_id = schema_probe_old_kid_node_type_.id;
        uint64_t new_id = schema_probe_kid_node_type_.id;
        int64_t i;
        int patched = 0;
        for ( i = 0; i + 8 <= wn; i++ )
        {
            if ( memcmp( wire + i, &old_id, 8 ) == 0 )
            {
                memcpy( wire + i, &new_id, 8 );
                patched = 1;
                break;
            }
        }
        assert( patched == 1 );
    }
    check_missing( wire, wn );

    /* 4. JSON pointer child "ptr": {} missing x. */
    const char * bad_ptr_json = "{\"a\":1,\"kid\":{\"x\":2},\"ptr\":{}}";
    assert( root_builder_init( &rb ) == 1 );
    assert( root_from_json( &rb, bad_ptr_json, (int64_t) strlen( bad_ptr_json ), &rep ) == 0 );
    assert( rep.missing_required == 1 && rep.refuse_reason == SCHEMA_TABLE_REFUSE_MISSING_REQUIRED );
    root_builder_shutdown( &rb );

    return 0;
}
`
	runCProbe(t, schema, probe)
}

func TestCRequiredExactFieldHasNoDefault(t *testing.T) {
	t.Parallel()
	const schema = `package probe

table Exact
{
    ver uint8 | min = 7, max = 7, required
}

table OldExact
{
}

table Other
{
    ver uint8
}

table Inner
{
    id uint8 | required
}

table Outer
{
    id uint8
    inner Inner
}
`
	probe := `#include <assert.h>
#include <stdint.h>
#include <string.h>
#include "ProbeTable.h"

int main( void )
{
    /* Fresh record holds ver at storage zero, outside [7,7]: save & measure refuse (-1). */
    ExactBuilder eb;
    assert( exact_builder_init( &eb ) == 1 );
    Exact * e = exact_builder_root( &eb );
    assert( e->ver == 0 );
    assert( exact_builder_lock( &eb ) == 1 );

    uint8_t wire[128];
    assert( exact_measure( NULL, (const Exact *) eb.region ) == -1 );
    assert( exact_save( NULL, (const Exact *) eb.region, wire, sizeof( wire ) ) == -1 );
    exact_builder_shutdown( &eb );

    /* Set ver = 7: saves and round-trips. */
    assert( exact_builder_init( &eb ) == 1 );
    e = exact_builder_root( &eb );
    e->ver = 7;
    assert( exact_builder_lock( &eb ) == 1 );
    int64_t wn = exact_save( NULL, (const Exact *) eb.region, wire, sizeof( wire ) );
    assert( wn > 0 && wn == exact_measure( NULL, (const Exact *) eb.region ) );
    exact_builder_shutdown( &eb );

    SCHEMA_C_ALIGN16 uint8_t region[256];
    TableReport rep;
    const Exact * got = exact_load( region, sizeof( region ), wire, wn, &rep );
    assert( got != NULL && got->ver == 7 );
    assert( rep.missing_required == 0 && rep.refuse_reason == SCHEMA_TABLE_REFUSE_OK );

    /* Wire without ver is missing_required. */
    OldExactBuilder oeb;
    assert( old_exact_builder_init( &oeb ) == 1 );
    assert( old_exact_builder_lock( &oeb ) == 1 );
    int64_t own = old_exact_save( NULL, (const OldExact *) oeb.region, wire, sizeof( wire ) );
    assert( own > 0 );
    old_exact_builder_shutdown( &oeb );

    got = exact_load( region, sizeof( region ), wire, own, &rep );
    assert( got == NULL );
    assert( rep.missing_required == 1 && rep.refuse_reason == SCHEMA_TABLE_REFUSE_MISSING_REQUIRED );

    /* JSON without ver is missing_required. */
    assert( exact_builder_init( &eb ) == 1 );
    assert( exact_from_json( &eb, "{\"hp\":5}", 8, &rep ) == 0 );
    assert( rep.missing_required == 1 && rep.refuse_reason == SCHEMA_TABLE_REFUSE_MISSING_REQUIRED );
    assert( exact_from_json( &eb, "{\"ver\":7}", 9, &rep ) == 1 );
    assert( exact_builder_root( &eb )->ver == 7 );
    exact_builder_shutdown( &eb );

    /* Cross-table: Other's ver is not required. */
    OtherBuilder otb;
    assert( other_builder_init( &otb ) == 1 );
    assert( other_from_json( &otb, "{}", 2, &rep ) == 1 );
    assert( rep.missing_required == 0 && rep.refuse_reason == SCHEMA_TABLE_REFUSE_OK );
    other_builder_shutdown( &otb );

    /* Nested: required field inside Inner is enforced at its object. */
    OuterBuilder outb;
    assert( outer_builder_init( &outb ) == 1 );
    assert( outer_from_json( &outb, "{\"inner\":{}}", 12, &rep ) == 0 );
    assert( rep.missing_required == 1 && rep.refuse_reason == SCHEMA_TABLE_REFUSE_MISSING_REQUIRED );
    assert( outer_from_json( &outb, "{\"inner\":{\"id\":5}}", 18, &rep ) == 1 );
    assert( outer_builder_root( &outb )->inner.id == 5 );
    outer_builder_shutdown( &outb );

    return 0;
}
`
	runCProbe(t, schema, probe)
}

func TestCRequiredInOffBranchAndNestedGuards(t *testing.T) {
	t.Parallel()
	const schema = `package probe

table Current
{
    active bool
    paused bool
    if active
    {
        req int32 | required
        if !paused
        {
            nested_req int32 | required
        }
    }
}

table Legacy
{
    active bool
    paused bool
    if active
    {
        x int32
    }
}
`
	probe := `#include <assert.h>
#include <stdint.h>
#include <string.h>
#include "ProbeTable.h"

int main( void )
{
    uint8_t wire[256];
    SCHEMA_C_ALIGN16 uint8_t region[512];
    TableReport rep;

    /* 1. Outer branch OFF (active=0): neither req nor nested_req is demanded. */
    CurrentBuilder cb;
    assert( current_builder_init( &cb ) == 1 );
    assert( current_builder_lock( &cb ) == 1 );
    int64_t wn = current_save( NULL, (const Current *) cb.region, wire, sizeof( wire ) );
    assert( wn > 0 );
    current_builder_shutdown( &cb );

    const Current * got = current_load( region, sizeof( region ), wire, wn, &rep );
    assert( got != NULL && got->active == 0 );
    assert( rep.missing_required == 0 && rep.refuse_reason == SCHEMA_TABLE_REFUSE_OK );

    /* 2. Outer branch ON, inner branch OFF (active=1, paused=1): req is required, nested_req is NOT. */
    assert( current_builder_init( &cb ) == 1 );
    Current * c = current_builder_root( &cb );
    c->active = 1;
    c->paused = 1;
    c->req = 0; /* rides at zero */
    assert( current_builder_lock( &cb ) == 1 );
    wn = current_save( NULL, (const Current *) cb.region, wire, sizeof( wire ) );
    assert( wn > 0 );
    current_builder_shutdown( &cb );

    got = current_load( region, sizeof( region ), wire, wn, &rep );
    assert( got != NULL && got->active == 1 && got->paused == 1 && got->req == 0 );
    assert( rep.missing_required == 0 && rep.refuse_reason == SCHEMA_TABLE_REFUSE_OK );

    /* JSON with active=true, paused=true, req=0 (no nested_req): accepted! */
    const char * json_paused = "{\"active\":true,\"paused\":true,\"req\":0}";
    assert( current_builder_init( &cb ) == 1 );
    assert( current_from_json( &cb, json_paused, (int64_t) strlen( json_paused ), &rep ) == 1 );
    assert( rep.missing_required == 0 );
    current_builder_shutdown( &cb );

    /* JSON with active=true, paused=false, req=0 (missing nested_req): refused! */
    const char * json_unpaused_missing = "{\"active\":true,\"paused\":false,\"req\":0}";
    assert( current_builder_init( &cb ) == 1 );
    assert( current_from_json( &cb, json_unpaused_missing, (int64_t) strlen( json_unpaused_missing ), &rep ) == 0 );
    assert( rep.missing_required == 1 && rep.refuse_reason == SCHEMA_TABLE_REFUSE_MISSING_REQUIRED );
    current_builder_shutdown( &cb );

    /* Legacy wire with active=true, paused=true (missing req): refused! */
    LegacyBuilder lb;
    assert( legacy_builder_init( &lb ) == 1 );
    Legacy * lg = legacy_builder_root( &lb );
    lg->active = 1;
    lg->paused = 1;
    lg->x = 5;
    assert( legacy_builder_lock( &lb ) == 1 );
    wn = legacy_save( NULL, (const Legacy *) lb.region, wire, sizeof( wire ) );
    assert( wn > 0 );
    legacy_builder_shutdown( &lb );

    got = current_load( region, sizeof( region ), wire, wn, &rep );
    assert( got == NULL );
    assert( rep.missing_required == 1 && rep.refuse_reason == SCHEMA_TABLE_REFUSE_MISSING_REQUIRED );

    return 0;
}
`
	runCProbe(t, schema, probe)
}

func TestCRequiredKeyedArrayAndEmptyNestedTable(t *testing.T) {
	t.Parallel()
	const schema = `package probe

enum Key
{
    Alpha
    Beta
}

table Keyed
{
    values [Key]int32 | required
}

table Other
{
    ver uint8
}

table RequiredEmpty
{
    value Other | required
}
`
	probe := `#include <assert.h>
#include <stdint.h>
#include <string.h>
#include "ProbeTable.h"

int main( void )
{
    /* 1. Required enum-keyed array at all-zero still writes the keyed header (count 0). */
    KeyedBuilder kb;
    assert( keyed_builder_init( &kb ) == 1 );
    Keyed * k = keyed_builder_root( &kb );
    assert( keyed_builder_lock( &kb ) == 1 );

    uint8_t kwire[128];
    (void) k;
    int64_t klen = keyed_save( NULL, (const Keyed *) kb.region, kwire, sizeof( kwire ) );
    assert( klen == keyed_measure( NULL, (const Keyed *) kb.region ) );
    assert( klen == 23 );
    const uint8_t want_keyed_prefix[] = { 0x01, 0x01, 0x10, 0x02, 0x04, 0x00, 0x00 };
    const uint8_t want_keyed_suffix[] = { 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00 };
    assert( memcmp( kwire, want_keyed_prefix, sizeof( want_keyed_prefix ) ) == 0 );
    assert( memcmp( kwire + 15, want_keyed_suffix, sizeof( want_keyed_suffix ) ) == 0 );
    keyed_builder_shutdown( &kb );

    SCHEMA_C_ALIGN16 uint8_t region[512];
    TableReport rep;
    const Keyed * kgot = keyed_load( region, sizeof( region ), kwire, klen, &rep );
    assert( kgot != NULL );
    assert( rep.missing_required == 0 && rep.refuse_reason == SCHEMA_TABLE_REFUSE_OK );

    /* 2. Required by-value nested table at default/zero still writes the 0-field nested body. */
    RequiredEmptyBuilder reb;
    assert( required_empty_builder_init( &reb ) == 1 );
    RequiredEmpty * re = required_empty_builder_root( &reb );
    assert( required_empty_builder_lock( &reb ) == 1 );

    uint8_t rwire[128];
    (void) re;
    int64_t rlen = required_empty_save( NULL, (const RequiredEmpty *) reb.region, rwire, sizeof( rwire ) );
    assert( rlen == required_empty_measure( NULL, (const RequiredEmpty *) reb.region ) );
    const uint8_t want_empty[] = {
        0x01, 0x01, 0x0d, 0x01, 0x00, 0x00,
        0xea, 0x0c, 0xe8, 0x30, 0x94, 0xfd, 0xe4, 0x7c,
        0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00
    };
    assert( rlen == (int64_t) sizeof( want_empty ) );
    assert( memcmp( rwire, want_empty, sizeof( want_empty ) ) == 0 );
    required_empty_builder_shutdown( &reb );

    const RequiredEmpty * rgot = required_empty_load( region, sizeof( region ), rwire, rlen, &rep );
    assert( rgot != NULL && rgot->value.ver == 0 );
    assert( rep.missing_required == 0 && rep.refuse_reason == SCHEMA_TABLE_REFUSE_OK );

    return 0;
}
`
	runCProbe(t, schema, probe)
}
