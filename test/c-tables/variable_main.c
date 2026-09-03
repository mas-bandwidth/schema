/* THE VARIABLE-LENGTH CLASS, end to end (docs/SPEC-TABLES.md §2, §6, §9):
   build through the arena, Lock, save to
   the tolerant wire, size a region from the wire alone, load into it, and walk
   the graph back out. Nothing in the conformance corpus reaches this path —
   its instances are all fixed — so it is exercised here. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "GraphTable.h"

static int failures = 0;
static void check( int ok, const char * what )
{
    if ( !ok ) { printf( "FAILED: %s\n", what ); failures++; }
}

int main( void )
{
    SceneBuilder builder;
    Scene * root;
    TableSink sink;
    TableCtx arena_ctx;
    int64_t wire_bytes, wrote, region_bytes;
    uint8_t * wire;
    uint8_t * region;
    const Scene * loaded;
    TableReport report;
    int i, walked;

    check( SceneBuilderInit( &builder ), "builder init" );
    root = SceneBuilderRoot( &builder );
    check( root != NULL, "the builder has a root" );
    if ( root == NULL ) { return 1; } /* everything below writes through it */

    memcpy( root->name, "world", 5 );
    root->name_length = 5;
    root->version = 7;

    /* a chain of ListNodes through the arena, each emplaced into the previous
       node's own slot — the shape only a pointer edge can express */
    sink.region = NULL;
    sink.worker = &builder.main;
    {
        TableRef * slot = &root->head;
        for ( i = 0; i < 5; i++ )
        {
            ListNode * node = ListNodeEmplace( &sink, slot );
            check( node != NULL, "emplace a list node" );
            if ( node == NULL ) { return 1; }
            node->value = i + 1;
            memcpy( node->name, "n", 1 );
            node->name_length = 1;
            slot = &node->next;
        }
    }
    /* a fixed table something points at, and a by-value nested variable table */
    {
        Settings * settings = SettingsEmplace( &sink, &root->settings );
        check( settings != NULL, "emplace settings" );
        if ( settings != NULL ) { settings->quality = 3; }
    }
    root->ground.depth = 2;
    {
        ListNode * head = ListNodeEmplace( &sink, &root->ground.head );
        check( head != NULL, "emplace the ground layer's head" );
        if ( head != NULL ) { head->value = 42; }
    }

    /* the wire, straight out of the MUTABLE form: the ctx names the arena */
    arena_ctx.arena = &builder.arena;
    wire_bytes = SceneMeasure( &arena_ctx, root );
    check( wire_bytes > 0, "measure the mutable form" );
    wire = (uint8_t *) malloc( (size_t) wire_bytes );
    wrote = SceneSave( &arena_ctx, root, wire, wire_bytes );
    check( wrote == wire_bytes, "save the mutable form at exactly Measure's answer" );

    /* LOCK: the segmented arena becomes one exact-packed region */
    check( SceneBuilderLock( &builder ), "lock" );
    check( builder.region != NULL && builder.region_bytes > 0, "lock produced a region" );
    {
        /* the CONST form saves the same bytes: one structure, two forms */
        const Scene * packed = (const Scene *) (const void *) builder.region;
        int64_t packed_bytes = SceneMeasure( NULL, packed );
        uint8_t * twin;
        check( packed_bytes == wire_bytes, "the locked region measures what the arena did" );
        twin = (uint8_t *) malloc( (size_t) ( packed_bytes > 0 ? packed_bytes : 1 ) );
        check( SceneSave( NULL, packed, twin, packed_bytes ) == packed_bytes, "save the locked form" );
        check( memcmp( twin, wire, (size_t) wire_bytes ) == 0,
               "the locked region and the arena write the SAME BYTES" );
        free( twin );
    }

    /* LOAD: size the region from the wire's framing alone, then decode into it */
    region_bytes = SceneLoadMeasure( wire, wire_bytes );
    check( region_bytes >= (int64_t) sizeof( Scene ), "LoadMeasure sized a region" );
    region = (uint8_t *) malloc( (size_t) region_bytes );
    memset( &report, 0, sizeof( report ) );
    loaded = SceneLoad( region, region_bytes, wire, wire_bytes, &report );
    check( loaded != NULL, "load into the caller's region" );
    check( !report.malformed && report.unknown == 0 && report.kind_mismatch == 0 && report.clamped == 0,
           "the report is silent over bytes this build wrote" );

    /* walk the graph back out through the region's own self-relative derefs */
    if ( loaded != NULL )
    {
        const ListNode * node = ListNodeAt( NULL, &loaded->head );
        check( loaded->version == 7, "the root's scalar survived" );
        check( loaded->name_length == 5 && memcmp( loaded->name, "world", 5 ) == 0, "the root's string survived" );
        walked = 0;
        while ( node != NULL )
        {
            walked++;
            check( node->value == walked, "the chain's values are in order" );
            node = ListNodeAt( NULL, &node->next );
        }
        check( walked == 5, "the whole chain came back" );
        {
            const Settings * settings = SettingsAt( NULL, &loaded->settings );
            const ListNode * ground = ListNodeAt( NULL, &loaded->ground.head );
            check( settings != NULL && settings->quality == 3, "the pointed-at fixed table survived" );
            check( ground != NULL && ground->value == 42, "the by-value nested variable table's pointer survived" );
        }
    }

    /* the TOOL's path: the same tolerant decode into a fresh builder */
    {
        SceneBuilder again;
        check( SceneBuilderInit( &again ), "second builder init" );
        memset( &report, 0, sizeof( report ) );
        check( SceneLoadBuilder( &again, wire, wire_bytes, &report ), "load into a builder" );
        check( !report.malformed, "the builder load is silent" );
        {
            TableCtx ctx2;
            ctx2.arena = &again.arena;
            check( SceneMeasure( &ctx2, SceneBuilderRoot( &again ) ) == wire_bytes,
                   "the reloaded builder measures the same wire" );
        }
        SceneBuilderShutdown( &again );
    }

    free( region );
    free( wire );
    SceneBuilderShutdown( &builder );

    if ( failures != 0 ) { printf( "%d failure(s)\n", failures ); return 1; }
    printf( "variable-length class: build -> lock -> wire -> region -> walk, all agree (%lld wire bytes, %lld region bytes)\n",
            (long long) wire_bytes, (long long) region_bytes );
    return 0;
}
