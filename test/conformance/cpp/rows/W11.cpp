// W11: bytes(N) is layout kind 14 (docs/FIXED-FORM-ALGORITHM.md:54).
//
// LAW (the file ./repo holds is the law; this comment is the pointer):
// "`bytes(N)` is walked as an ARRAY OF `u8` - kind `14` with one synthetic
// child at kind `6`, size `1` - never as a text kind; only `string(N)` (`12`)
// and `wstring(N)` (`33`) are text kinds in a layout."
//
// PRODUCTION PATH (named call site): the schema compiler emits
// tabledemo::ProfileConfigFixedLayout (tabledemo declares
// `icon bytes(16)` in tables/examples/Tables.schema:94, inside fixed table
// ProfileConfig), and every fixed-form read reaches it through
// tabledemo::TableFixedEntryAt / tabledemo::TableFixedParseLayout in
// build/tables-generated/examples/TablesTable.h - the same header
// test/conformance/cpp/main.cpp includes via -Ibuild/tables-generated/examples
// (CONFORMANCE_INCLUDES, Makefile:4651), and the same parse-then-compile path
// ProfileConfigFixedLoad takes (TablesTable.h:13444: ParseLayout then
// TableFixedCompile). This test calls that actual path, not just a helper:
// ParseLayout over the emitted bytes, then EntryAt over the view.
//
// BINDING CONSTRAINT / BOUNDARY: the synthetic child is EXACTLY kind 6
// (u8) size 1 children 0, the parent has EXACTLY 1 child, and the parent's
// size is N+4 (the 4-byte count included): icon bytes(16) rides size 20.
// A scheduling/store/identity reading of (d) does not apply here; the
// boundary is the child triple and the +4.
//
// NEGATIVE CONTROL (behavioural, compiles): flip the bytes entry's kind
// byte 14->12 in a copy of the emitted layout (or in the hand-built vector
// below) and this test goes RED while still compiling; restore and it is
// GREEN. Recorded in RESULT.md as control:. A deletion that only fails to
// compile proves nothing, so the control keeps every include and call.

#include <cstdint>
#include <cstdio>
#include <vector>

#include "TablesTable.h"

static int failures = 0;

static void check( bool ok, const char * what )
{
    std::printf( "%s: %s\n", ok ? "PASS" : "FAIL", what );
    if ( !ok ) { failures++; }
}

static void put_entry( std::vector<uint8_t> & layout, uint64_t id, uint8_t kind, uint32_t size, uint32_t children )
{
    uint8_t entry[17];
    for ( int i = 0; i < 8; ++i ) { entry[i] = (uint8_t) ( id >> ( 8 * i ) ); }
    entry[8] = kind;
    for ( int i = 0; i < 4; ++i ) { entry[9 + i] = (uint8_t) ( size >> ( 8 * i ) ); }
    for ( int i = 0; i < 4; ++i ) { entry[13 + i] = (uint8_t) ( children >> ( 8 * i ) ); }
    layout.insert( layout.end(), entry, entry + 17 );
}

int main()
{
    // 1. The emitted layout for a table carrying bytes(N) parses as a §1 layout.
    const tabledemo::TableFixedLayoutView emitted = {
        tabledemo::ProfileConfigFixedLayout,
        (int32_t) ( ( tabledemo::ProfileConfigFixedLayoutBytes - 4 ) / 17 )
    };
    {
        tabledemo::TableFixedLayoutView parsed;
        tabledemo::TableMessageReason why = tabledemo::layout_malformed;
        const bool ok = tabledemo::TableFixedParseLayout(
            tabledemo::ProfileConfigFixedLayout,
            (int64_t) tabledemo::ProfileConfigFixedLayoutBytes, parsed, why );
        check( ok, "W11: ProfileConfigFixedLayout parses (TableFixedParseLayout)" );
        check( parsed.count == emitted.count, "W11: parsed count equals emitted count" );
    }

    // 2. The law on the emitted bytes: one kind-14 entry with exactly one
    //    child at kind 6 size 1 children 0, never a text kind (12/33).
    int bytes_hits = 0;
    int32_t bytes_at = -1;
    int32_t bytes_size = 0;
    for ( int32_t i = 0; i < emitted.count; ++i )
    {
        const tabledemo::TableFixedLayoutEntry e = tabledemo::TableFixedEntryAt( emitted, i );
        if ( e.kind == 14 && e.children == 1 && i + 1 < emitted.count )
        {
            const tabledemo::TableFixedLayoutEntry el = tabledemo::TableFixedEntryAt( emitted, i + 1 );
            if ( el.kind == 6 && el.size == 1 && el.children == 0 )
            {
                bytes_hits++;
                bytes_at = i;
                bytes_size = (int32_t) e.size;
                check( e.kind != 12 && e.kind != 33, "W11: bytes(N) entry is not a text kind (12/33)" );
            }
        }
    }
    check( bytes_hits == 1, "W11: exactly one bytes(N)-as-array-of-u8 entry (icon bytes(16))" );
    check( bytes_at >= 0 && bytes_size == 20, "W11: bytes(16) entry size is N+4 = 20 (count included)" );

    // 3. The text-kind control in the SAME layout: name string(32) rides
    //    kind 12, so this test distinguishes text from bytes and is not vacuous.
    bool saw_string12 = false;
    for ( int32_t i = 0; i < emitted.count; ++i )
    {
        const tabledemo::TableFixedLayoutEntry e = tabledemo::TableFixedEntryAt( emitted, i );
        if ( e.kind == 12 && e.children == 0 ) { saw_string12 = true; }
    }
    check( saw_string12, "W11: control: string(N) in the same layout is kind 12 (text)" );

    // 4. The smallest byte vector from the law (§1): a root table with one
    //    bytes(4) field. Derivation: layout := u32 count, count * Entry;
    //    Entry := id u64, kind u8, size u32, children u32 (17 bytes).
    //    count = 3: root kind 13 size 8 children 1 (8 = 4 count + 4 bytes),
    //    field kind 14 size 8 children 1, synthetic child id 0 kind 6
    //    size 1 children 0. Sizes per §1.2: leaf 6 admits 1; array 14 admits
    //    size>=4 with (size-4)%elem==0 ((8-4)%1==0); table 13 size == sum (8).
    std::vector<uint8_t> minimal;
    minimal.resize( 4 );
    tabledemo::TableFixedPut32( minimal.data(), 3u );
    put_entry( minimal, 0x0102030405060708ull, 13u, 8u, 1u );
    put_entry( minimal, 0x1112131415161718ull, 14u, 8u, 1u );
    put_entry( minimal, 0u, 6u, 1u, 0u );
    {
        tabledemo::TableFixedLayoutView parsed;
        tabledemo::TableMessageReason why = tabledemo::layout_malformed;
        const bool ok = tabledemo::TableFixedParseLayout(
            minimal.data(), (int64_t) minimal.size(), parsed, why );
        check( ok, "W11: minimal bytes(4) layout parses (count + 3 entries)" );
        if ( ok )
        {
            const tabledemo::TableFixedLayoutEntry root = tabledemo::TableFixedEntryAt( parsed, 0 );
            const tabledemo::TableFixedLayoutEntry field = tabledemo::TableFixedEntryAt( parsed, 1 );
            const tabledemo::TableFixedLayoutEntry elem = tabledemo::TableFixedEntryAt( parsed, 2 );
            check( root.kind == 13, "W11: minimal root is kind 13 (table)" );
            check( field.kind == 14 && field.children == 1, "W11: minimal bytes(4) is kind 14 with one child" );
            check( elem.kind == 6 && elem.size == 1 && elem.children == 0,
                   "W11: minimal synthetic child is kind 6 size 1 (u8, the boundary)" );
            check( field.kind != 12 && field.kind != 33, "W11: minimal bytes(4) is never a text kind" );
        }
        else
        {
            check( false, "W11: minimal root is kind 13 (table)" );
            check( false, "W11: minimal bytes(4) is kind 14 with one child" );
            check( false, "W11: minimal synthetic child is kind 6 size 1 (u8, the boundary)" );
            check( false, "W11: minimal bytes(4) is never a text kind" );
        }
    }

    // 5. Behavioural negative shape, still compiling: the same minimal
    //    vector with the field respelled as text kind 12 must NOT satisfy
    //    the bytes predicate (proves the predicate bites on the kind byte).
    {
        std::vector<uint8_t> broken = minimal;
        broken[4 + 17 * 1 + 8] = 12u; // field kind 14 -> 12, everything else identical
        tabledemo::TableFixedLayoutView view = { broken.data(), 3 };
        const tabledemo::TableFixedLayoutEntry field = tabledemo::TableFixedEntryAt( view, 1 );
        const tabledemo::TableFixedLayoutEntry elem = tabledemo::TableFixedEntryAt( view, 2 );
        const bool bites = !( field.kind == 14 && field.children == 1 &&
                              elem.kind == 6 && elem.size == 1 && elem.children == 0 );
        check( bites, "W11: control: kind 14->12 breaks the bytes predicate (still compiles)" );
    }

    if ( failures != 0 ) { std::printf( "W11: %d assertion(s) failed\n", failures ); return 1; }
    std::printf( "W11: bytes(N) is layout kind 14 with synthetic u8 child - green\n" );
    return 0;
}
