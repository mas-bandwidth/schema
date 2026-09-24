/* The ACCESSOR/DESCRIPTOR AGREEMENT gate, C leg of the J1 technique
 * (docs/PORTING.md, schema#421). The generated ACCESSOR (the struct member a
 * declaration named) and the generated DESCRIPTOR (the TableBlockFieldInfo /
 * TableFieldInfo rows) are two independent derivations of one layout, so the
 * leg reads every field of the block projection, of every block row, and of
 * the cook node BOTH ways and requires agreement. A reading tier that only
 * ever walks the descriptors could read the descriptors twice and never know.
 *
 * The two tiers do NOT derive their offsets the same way, and this test is
 * shaped to match:
 *   - the BLOCK tier (TableBlockFieldInfo) carries the emitter's own number as
 *     a literal (internal/codegen/ctable/block.go, `%du` on fl.Offset), so an
 *     offset-against-offset comparison against the C compiler's `offsetof` of
 *     the emitted projection/row struct is a genuine two-way check. It is
 *     asserted here, field by field, along with the value read blind at
 *     base + offset.
 *   - the COOK tier (TableFieldInfo) carries `(uint32_t) offsetof( St, member )`
 *     (internal/codegen/ctable/codecs.go), so the C compiler computes BOTH
 *     sides and an offset-against-offset comparison there would be a tautology.
 *     What is not a tautology there is the VALUE read back blind against the
 *     named member, the pointer slot's elem_size == sizeof( TableRef ), and
 *     the delta read through the slot. The pointer field `next` is held
 *     separately as a SLOT, with its own message, because its position is what
 *     a self-relative delta is relative to.
 *
 * The same source is compiled twice, once per unit (the C leg compiles a
 * driver per directory; two units cannot be included into one translation
 * unit because they redefine the shared runtime), under the two -D flags the
 * make target passes.
 */

#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include <stddef.h>

#ifdef ACCESSOR_DESCRIPTOR_BLOCK
#include "RenderBlock.h"
#endif
#ifdef ACCESSOR_DESCRIPTOR_COOK
#include "GraphTable.h"
#endif

static int checked_total = 0;

#define FAIL(...) do { fprintf( stderr, __VA_ARGS__ ); return 1; } while ( 0 )

#ifdef ACCESSOR_DESCRIPTOR_BLOCK

static void fill_pattern( uint8_t * p, size_t n )
{
    size_t i;
    for ( i = 0; i < n; i++ ) { p[i] = (uint8_t) ( i * 37u + 11u ); }
}

static const TableBlockFieldInfo * bfield( const TableBlockInfo * info, const char * name )
{
    int32_t i;
    for ( i = 0; i < info->num_fields; i++ )
    {
        if ( strcmp( info->fields[i].name, name ) == 0 ) { return &info->fields[i]; }
    }
    return NULL;
}

/* CHECK_BLOCK asserts the block-tier OFFSET agreement (the emitter's literal
 * fl.Offset against the C compiler's offsetof of the named member) and the
 * VALUE agreement (the bytes at base + descriptor offset against the named
 * member's bytes). */
#define CHECK_BLOCK( prefix, p, member, f ) do { \
        uintptr_t off_ = (uintptr_t) &( ( p )->member ) - (uintptr_t) ( p ); \
        if ( (uint32_t) off_ != ( f )->offset ) \
            FAIL( "%s.%s: the accessor's offset is not the descriptor's\n", prefix, ( f )->name ); \
        if ( memcmp( (const void *) ( (uintptr_t) ( p ) + ( f )->offset ), \
                     (const void *) &( ( p )->member ), ( f )->size ) != 0 ) \
            FAIL( "%s.%s: the accessor and the descriptor disagree about the value\n", prefix, ( f )->name ); \
        checked_total++; \
    } while ( 0 )

#define REQUIRE_FIELD( info, nm ) do { \
        if ( bfield( ( info ), ( nm ) ) == NULL ) FAIL( "block: field %s missing from %s\n", nm, ( info )->name ); \
    } while ( 0 )

static int check_projection( void )
{
    const TableBlockInfo * proj = render_frame_block_type();
    RenderFrameBlockProjection p;
    int32_t i;
    if ( proj == NULL ) { FAIL( "block: RenderFrame projection descriptor missing\n" ); }
    fill_pattern( (uint8_t *) &p, sizeof p );
    for ( i = 0; i < proj->num_fields; i++ )
    {
        const TableBlockFieldInfo * f = &proj->fields[i];
        if ( strcmp( f->name, "version" ) == 0 ) { CHECK_BLOCK( "block", &p, version, f ); }
        else if ( strcmp( f->name, "cameras" ) == 0 ) { CHECK_BLOCK( "block", &p, cameras, f ); }
        else if ( strcmp( f->name, "ships" ) == 0 ) { CHECK_BLOCK( "block", &p, ships, f ); }
        else if ( strcmp( f->name, "turrets" ) == 0 ) { CHECK_BLOCK( "block", &p, turrets, f ); }
        else if ( strcmp( f->name, "missiles" ) == 0 ) { CHECK_BLOCK( "block", &p, missiles, f ); }
        else if ( strcmp( f->name, "dynamic_props" ) == 0 ) { CHECK_BLOCK( "block", &p, dynamic_props, f ); }
        else if ( strcmp( f->name, "static_props" ) == 0 ) { CHECK_BLOCK( "block", &p, static_props, f ); }
        else if ( strcmp( f->name, "cosmetic_props" ) == 0 ) { CHECK_BLOCK( "block", &p, cosmetic_props, f ); }
        else if ( strcmp( f->name, "lasers" ) == 0 ) { CHECK_BLOCK( "block", &p, lasers, f ); }
        else if ( strcmp( f->name, "explosions" ) == 0 ) { CHECK_BLOCK( "block", &p, explosions, f ); }
        else { FAIL( "block: unexpected projection field %s\n", f->name ); }
    }
    printf( "block projection RenderFrame: %d fields\n", proj->num_fields );
    return 0;
}

static int check_row( const char * what, const TableBlockInfo * info, void * base )
{
    const char * name = info != NULL ? info->name : "(null)";
    int32_t i;
    if ( info == NULL ) { FAIL( "block: row %s descriptor missing\n", what ); }
    fill_pattern( (uint8_t *) base, info->size );
    for ( i = 0; i < info->num_fields; i++ )
    {
        const TableBlockFieldInfo * f = &info->fields[i];
        if ( strcmp( name, "RenderCamera" ) == 0 )
        {
            RenderCamera * s = (RenderCamera *) base;
            if ( strcmp( f->name, "position" ) == 0 ) { CHECK_BLOCK( "block", s, position, f ); }
            else if ( strcmp( f->name, "rotation" ) == 0 ) { CHECK_BLOCK( "block", s, rotation, f ); }
            else if ( strcmp( f->name, "camera_id" ) == 0 ) { CHECK_BLOCK( "block", s, camera_id, f ); }
            else if ( strcmp( f->name, "camera_type" ) == 0 ) { CHECK_BLOCK( "block", s, camera_type, f ); }
            else if ( strcmp( f->name, "target_object_id" ) == 0 ) { CHECK_BLOCK( "block", s, target_object_id, f ); }
            else if ( strcmp( f->name, "fov" ) == 0 ) { CHECK_BLOCK( "block", s, fov, f ); }
            else { FAIL( "block: unexpected field %s.%s\n", name, f->name ); }
        }
        else if ( strcmp( name, "RenderCosmeticProp" ) == 0 )
        {
            RenderCosmeticProp * s = (RenderCosmeticProp *) base;
            if ( strcmp( f->name, "position" ) == 0 ) { CHECK_BLOCK( "block", s, position, f ); }
            else if ( strcmp( f->name, "rotation" ) == 0 ) { CHECK_BLOCK( "block", s, rotation, f ); }
            else if ( strcmp( f->name, "scale" ) == 0 ) { CHECK_BLOCK( "block", s, scale, f ); }
            else if ( strcmp( f->name, "flags" ) == 0 ) { CHECK_BLOCK( "block", s, flags, f ); }
            else if ( strcmp( f->name, "cosmetic_prop_id" ) == 0 ) { CHECK_BLOCK( "block", s, cosmetic_prop_id, f ); }
            else if ( strcmp( f->name, "prop_sequence" ) == 0 ) { CHECK_BLOCK( "block", s, prop_sequence, f ); }
            else if ( strcmp( f->name, "prop_type" ) == 0 ) { CHECK_BLOCK( "block", s, prop_type, f ); }
            else if ( strcmp( f->name, "team" ) == 0 ) { CHECK_BLOCK( "block", s, team, f ); }
            else { FAIL( "block: unexpected field %s.%s\n", name, f->name ); }
        }
        else if ( strcmp( name, "RenderDynamicProp" ) == 0 )
        {
            RenderDynamicProp * s = (RenderDynamicProp *) base;
            if ( strcmp( f->name, "position" ) == 0 ) { CHECK_BLOCK( "block", s, position, f ); }
            else if ( strcmp( f->name, "rotation" ) == 0 ) { CHECK_BLOCK( "block", s, rotation, f ); }
            else if ( strcmp( f->name, "flags" ) == 0 ) { CHECK_BLOCK( "block", s, flags, f ); }
            else if ( strcmp( f->name, "object_id" ) == 0 ) { CHECK_BLOCK( "block", s, object_id, f ); }
            else if ( strcmp( f->name, "object_sequence" ) == 0 ) { CHECK_BLOCK( "block", s, object_sequence, f ); }
            else if ( strcmp( f->name, "prop_type" ) == 0 ) { CHECK_BLOCK( "block", s, prop_type, f ); }
            else if ( strcmp( f->name, "team" ) == 0 ) { CHECK_BLOCK( "block", s, team, f ); }
            else { FAIL( "block: unexpected field %s.%s\n", name, f->name ); }
        }
        else if ( strcmp( name, "RenderExplosion" ) == 0 )
        {
            RenderExplosion * s = (RenderExplosion *) base;
            if ( strcmp( f->name, "position" ) == 0 ) { CHECK_BLOCK( "block", s, position, f ); }
            else if ( strcmp( f->name, "rotation" ) == 0 ) { CHECK_BLOCK( "block", s, rotation, f ); }
            else if ( strcmp( f->name, "t" ) == 0 ) { CHECK_BLOCK( "block", s, t, f ); }
            else if ( strcmp( f->name, "explosion_id" ) == 0 ) { CHECK_BLOCK( "block", s, explosion_id, f ); }
            else if ( strcmp( f->name, "parent_object_id" ) == 0 ) { CHECK_BLOCK( "block", s, parent_object_id, f ); }
            else if ( strcmp( f->name, "explosion_type" ) == 0 ) { CHECK_BLOCK( "block", s, explosion_type, f ); }
            else if ( strcmp( f->name, "team" ) == 0 ) { CHECK_BLOCK( "block", s, team, f ); }
            else { FAIL( "block: unexpected field %s.%s\n", name, f->name ); }
        }
        else if ( strcmp( name, "RenderLaser" ) == 0 )
        {
            RenderLaser * s = (RenderLaser *) base;
            if ( strcmp( f->name, "start" ) == 0 ) { CHECK_BLOCK( "block", s, start, f ); }
            else if ( strcmp( f->name, "finish" ) == 0 ) { CHECK_BLOCK( "block", s, finish, f ); }
            else if ( strcmp( f->name, "t" ) == 0 ) { CHECK_BLOCK( "block", s, t, f ); }
            else if ( strcmp( f->name, "laser_id" ) == 0 ) { CHECK_BLOCK( "block", s, laser_id, f ); }
            else if ( strcmp( f->name, "laser_type" ) == 0 ) { CHECK_BLOCK( "block", s, laser_type, f ); }
            else if ( strcmp( f->name, "team" ) == 0 ) { CHECK_BLOCK( "block", s, team, f ); }
            else { FAIL( "block: unexpected field %s.%s\n", name, f->name ); }
        }
        else if ( strcmp( name, "RenderMissile" ) == 0 )
        {
            RenderMissile * s = (RenderMissile *) base;
            if ( strcmp( f->name, "position" ) == 0 ) { CHECK_BLOCK( "block", s, position, f ); }
            else if ( strcmp( f->name, "rotation" ) == 0 ) { CHECK_BLOCK( "block", s, rotation, f ); }
            else if ( strcmp( f->name, "flags" ) == 0 ) { CHECK_BLOCK( "block", s, flags, f ); }
            else if ( strcmp( f->name, "object_id" ) == 0 ) { CHECK_BLOCK( "block", s, object_id, f ); }
            else if ( strcmp( f->name, "object_sequence" ) == 0 ) { CHECK_BLOCK( "block", s, object_sequence, f ); }
            else if ( strcmp( f->name, "missile_type" ) == 0 ) { CHECK_BLOCK( "block", s, missile_type, f ); }
            else if ( strcmp( f->name, "team" ) == 0 ) { CHECK_BLOCK( "block", s, team, f ); }
            else { FAIL( "block: unexpected field %s.%s\n", name, f->name ); }
        }
        else if ( strcmp( name, "RenderQuaternion" ) == 0 )
        {
            RenderQuaternion * s = (RenderQuaternion *) base;
            if ( strcmp( f->name, "x" ) == 0 ) { CHECK_BLOCK( "block", s, x, f ); }
            else if ( strcmp( f->name, "y" ) == 0 ) { CHECK_BLOCK( "block", s, y, f ); }
            else if ( strcmp( f->name, "z" ) == 0 ) { CHECK_BLOCK( "block", s, z, f ); }
            else if ( strcmp( f->name, "w" ) == 0 ) { CHECK_BLOCK( "block", s, w, f ); }
            else { FAIL( "block: unexpected field %s.%s\n", name, f->name ); }
        }
        else if ( strcmp( name, "RenderShip" ) == 0 )
        {
            RenderShip * s = (RenderShip *) base;
            if ( strcmp( f->name, "position" ) == 0 ) { CHECK_BLOCK( "block", s, position, f ); }
            else if ( strcmp( f->name, "rotation" ) == 0 ) { CHECK_BLOCK( "block", s, rotation, f ); }
            else if ( strcmp( f->name, "flags" ) == 0 ) { CHECK_BLOCK( "block", s, flags, f ); }
            else if ( strcmp( f->name, "object_id" ) == 0 ) { CHECK_BLOCK( "block", s, object_id, f ); }
            else if ( strcmp( f->name, "target_object_id" ) == 0 ) { CHECK_BLOCK( "block", s, target_object_id, f ); }
            else if ( strcmp( f->name, "thrust" ) == 0 ) { CHECK_BLOCK( "block", s, thrust, f ); }
            else if ( strcmp( f->name, "object_sequence" ) == 0 ) { CHECK_BLOCK( "block", s, object_sequence, f ); }
            else if ( strcmp( f->name, "ship_type" ) == 0 ) { CHECK_BLOCK( "block", s, ship_type, f ); }
            else if ( strcmp( f->name, "team" ) == 0 ) { CHECK_BLOCK( "block", s, team, f ); }
            else if ( strcmp( f->name, "has_target_lock" ) == 0 ) { CHECK_BLOCK( "block", s, has_target_lock, f ); }
            else if ( strcmp( f->name, "predicted_explode" ) == 0 ) { CHECK_BLOCK( "block", s, predicted_explode, f ); }
            else { FAIL( "block: unexpected field %s.%s\n", name, f->name ); }
        }
        else if ( strcmp( name, "RenderStaticProp" ) == 0 )
        {
            RenderStaticProp * s = (RenderStaticProp *) base;
            if ( strcmp( f->name, "position" ) == 0 ) { CHECK_BLOCK( "block", s, position, f ); }
            else if ( strcmp( f->name, "rotation" ) == 0 ) { CHECK_BLOCK( "block", s, rotation, f ); }
            else if ( strcmp( f->name, "scale" ) == 0 ) { CHECK_BLOCK( "block", s, scale, f ); }
            else if ( strcmp( f->name, "flags" ) == 0 ) { CHECK_BLOCK( "block", s, flags, f ); }
            else if ( strcmp( f->name, "static_prop_id" ) == 0 ) { CHECK_BLOCK( "block", s, static_prop_id, f ); }
            else if ( strcmp( f->name, "prop_type" ) == 0 ) { CHECK_BLOCK( "block", s, prop_type, f ); }
            else if ( strcmp( f->name, "team" ) == 0 ) { CHECK_BLOCK( "block", s, team, f ); }
            else { FAIL( "block: unexpected field %s.%s\n", name, f->name ); }
        }
        else if ( strcmp( name, "RenderTurret" ) == 0 )
        {
            RenderTurret * s = (RenderTurret *) base;
            if ( strcmp( f->name, "rotation" ) == 0 ) { CHECK_BLOCK( "block", s, rotation, f ); }
            else if ( strcmp( f->name, "flags" ) == 0 ) { CHECK_BLOCK( "block", s, flags, f ); }
            else if ( strcmp( f->name, "object_id" ) == 0 ) { CHECK_BLOCK( "block", s, object_id, f ); }
            else if ( strcmp( f->name, "parent_object_id" ) == 0 ) { CHECK_BLOCK( "block", s, parent_object_id, f ); }
            else if ( strcmp( f->name, "turret_index" ) == 0 ) { CHECK_BLOCK( "block", s, turret_index, f ); }
            else if ( strcmp( f->name, "target_object_id" ) == 0 ) { CHECK_BLOCK( "block", s, target_object_id, f ); }
            else if ( strcmp( f->name, "object_sequence" ) == 0 ) { CHECK_BLOCK( "block", s, object_sequence, f ); }
            else if ( strcmp( f->name, "team" ) == 0 ) { CHECK_BLOCK( "block", s, team, f ); }
            else if ( strcmp( f->name, "has_target_lock" ) == 0 ) { CHECK_BLOCK( "block", s, has_target_lock, f ); }
            else { FAIL( "block: unexpected field %s.%s\n", name, f->name ); }
        }
        else if ( strcmp( name, "RenderVector3" ) == 0 )
        {
            RenderVector3 * s = (RenderVector3 *) base;
            if ( strcmp( f->name, "x" ) == 0 ) { CHECK_BLOCK( "block", s, x, f ); }
            else if ( strcmp( f->name, "y" ) == 0 ) { CHECK_BLOCK( "block", s, y, f ); }
            else if ( strcmp( f->name, "z" ) == 0 ) { CHECK_BLOCK( "block", s, z, f ); }
            else { FAIL( "block: unexpected field %s.%s\n", name, f->name ); }
        }
        else { FAIL( "block: unknown row type %s\n", name ); }
    }
    printf( "block row %s: %d fields\n", name, info->num_fields );
    return 0;
}

static int check_block( void )
{
    const TableBlockInfo * proj = render_frame_block_type();
    const TableBlockInfo * camera, * ship, * turret, * missile, * dyn, * stat, * cosm, * laser, * explo, * vec3, * quat;

    /* assert the block reads back cleanly FIRST: begin one with zero counts and
     * open the extent it produced, so a broken fixture cannot pass this row. */
    {
        RenderFrameBlockStorage storage;
        RenderFrameBlock block, reopened;
        RenderFrameCounts counts;
        TableBlockAllocator alloc = table_block_default_allocator();
        memset( &counts, 0, sizeof counts );
        memset( &storage, 0, sizeof storage );
        memset( &block, 0, sizeof block );
        if ( !render_frame_block_storage_create( &storage, &alloc ) ) { FAIL( "block: storage create failed\n" ); }
        if ( !render_frame_block_begin( &block, &storage, &counts, NULL ) ) { FAIL( "block: begin failed\n" ); }
        if ( !render_frame_block_open( &reopened, block.base, render_frame_block_bytes( &block ) ) )
        { FAIL( "block: the just-written block did not open cleanly\n" ); }
        render_frame_block_storage_destroy( &storage );
    }

    if ( check_projection() != 0 ) { return 1; }

    if ( proj == NULL ) { FAIL( "block: projection missing\n" ); }
    REQUIRE_FIELD( proj, "cameras" );
    REQUIRE_FIELD( proj, "ships" );
    REQUIRE_FIELD( proj, "turrets" );
    REQUIRE_FIELD( proj, "missiles" );
    REQUIRE_FIELD( proj, "dynamic_props" );
    REQUIRE_FIELD( proj, "static_props" );
    REQUIRE_FIELD( proj, "cosmetic_props" );
    REQUIRE_FIELD( proj, "lasers" );
    REQUIRE_FIELD( proj, "explosions" );

    camera  = bfield( proj, "cameras" )->element;
    ship    = bfield( proj, "ships" )->element;
    turret  = bfield( proj, "turrets" )->element;
    missile = bfield( proj, "missiles" )->element;
    dyn     = bfield( proj, "dynamic_props" )->element;
    stat    = bfield( proj, "static_props" )->element;
    cosm    = bfield( proj, "cosmetic_props" )->element;
    laser   = bfield( proj, "lasers" )->element;
    explo   = bfield( proj, "explosions" )->element;
    vec3    = bfield( camera, "position" )->element;
    quat    = bfield( camera, "rotation" )->element;

    {
        RenderCamera s; if ( check_row( "RenderCamera", camera, &s ) != 0 ) { return 1; }
        RenderCosmeticProp c; if ( check_row( "RenderCosmeticProp", cosm, &c ) != 0 ) { return 1; }
        RenderDynamicProp d; if ( check_row( "RenderDynamicProp", dyn, &d ) != 0 ) { return 1; }
        RenderExplosion e; if ( check_row( "RenderExplosion", explo, &e ) != 0 ) { return 1; }
        RenderLaser l; if ( check_row( "RenderLaser", laser, &l ) != 0 ) { return 1; }
        RenderMissile m; if ( check_row( "RenderMissile", missile, &m ) != 0 ) { return 1; }
        RenderQuaternion q; if ( check_row( "RenderQuaternion", quat, &q ) != 0 ) { return 1; }
        RenderShip sh; if ( check_row( "RenderShip", ship, &sh ) != 0 ) { return 1; }
        RenderStaticProp sp; if ( check_row( "RenderStaticProp", stat, &sp ) != 0 ) { return 1; }
        RenderTurret tu; if ( check_row( "RenderTurret", turret, &tu ) != 0 ) { return 1; }
        RenderVector3 v; if ( check_row( "RenderVector3", vec3, &v ) != 0 ) { return 1; }
    }

    printf( "block total: %d fields checked\n", checked_total );
    return 0;
}

#endif /* ACCESSOR_DESCRIPTOR_BLOCK */

#ifdef ACCESSOR_DESCRIPTOR_COOK

/* CHECK_COOK_VALUE reads the field blind at base + descriptor offset and holds
 * it against the named member. On the cook tier this is the non-tautological
 * check: the descriptor's offset is itself offsetof, so the bytes actually read
 * back are what is held against the named accessor. */
#define CHECK_COOK_VALUE( prefix, base, member, f ) do { \
        if ( memcmp( (const void *) ( (uintptr_t) ( base ) + ( f )->offset ), \
                     (const void *) &( member ), ( f )->elem_size ) != 0 ) \
            FAIL( "%s.%s: the accessor and the descriptor disagree about the value\n", prefix, ( f )->name ); \
        checked_total++; \
    } while ( 0 )

static int check_cook( void )
{
    const TableTypeInfo * info = list_node_table_type();
    ListNode node;
    int32_t i;
    if ( info == NULL || info->num_fields != 3 ) { FAIL( "cook: ListNode descriptor missing or wrong\n" ); }
    list_node_reset( &node );
    node.value = 7;
    memcpy( node.name, "hello", 6 );
    node.name_length = 5;
    node.next.value = 0;

    for ( i = 0; i < info->num_fields; i++ )
    {
        const TableFieldInfo * f = &info->fields[i];
        if ( strcmp( f->name, "value" ) == 0 )
        {
            CHECK_COOK_VALUE( "cook", &node, node.value, f );
        }
        else if ( strcmp( f->name, "name" ) == 0 )
        {
            CHECK_COOK_VALUE( "cook", &node, node.name, f );
        }
        else if ( strcmp( f->name, "next" ) == 0 )
        {
            /* a pointer's storage IS the reference slot: its position is what a
             * self-relative delta is relative to, so it is held separately. */
            if ( (uint32_t) offsetof( ListNode, next ) != f->offset )
                FAIL( "cook.next: the slot accessor's offset is not the descriptor's\n" );
            if ( f->elem_size != (uint32_t) sizeof( TableRef ) )
                FAIL( "cook.next: the slot accessor's elem_size is not sizeof( TableRef )\n" );
            CHECK_COOK_VALUE( "cook", &node, node.next, f );
        }
        else { FAIL( "cook: unexpected field %s\n", f->name ); }
    }
    printf( "cook ListNode: %d fields\n", info->num_fields );
    return 0;
}

#endif /* ACCESSOR_DESCRIPTOR_COOK */

int main( void )
{
#ifdef ACCESSOR_DESCRIPTOR_BLOCK
    if ( check_block() != 0 ) { return 1; }
#endif
#ifdef ACCESSOR_DESCRIPTOR_COOK
    if ( check_cook() != 0 ) { return 1; }
#endif
    return 0;
}
