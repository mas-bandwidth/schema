// R32: retire for real — a retired version is refused by name, once, idempotently.
//
// SPEC §5.2: per lineage entry a RETIRED mark (schema lock --retire T@<hash>)
// gives COMPILE the floor and layout_unsupported. §11.4: below the floor ->
// layout_unsupported (upgrade the client); outside lineage altogether ->
// layout_newer (ship the reader). Once: one hash in one index; idempotently:
// the refusal is deterministic, named, and never a counter or partial decode.
//
// THIS TEST USES the three VOLD/VMID/VNEW schema fixtures that ship together
// under build/tables-generated/vnum/. The vnew Floor has floor=1 (hardcoded
// from fixtureFloor in internal/codegen/cpptable/lineage.go), so lineage[0]
// (vold_floor hash) sits BELOW the floor. A vold-floor file loaded against the
// vnew reader MUST return layout_unsupported.
//
// Run: c++ -std=c++17 -Wall -Ibuild/tables-generated/vnum R32.cpp -o build/rows-cpp-R32 && ./build/rows-cpp-R32

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cstdint>
#include <vector>

// The three evolution tiers share the same table name ("Floored") but carry
// different layouts. VOLD is the oldest, VNEW is the identity (current).
// Their headers live side-by-side in the generated tree.
#include "VOLD_floorTable.h"  // namespace vold_floor: layout[0], no extra fields
#include "VMID_floorTable.h"  // namespace vmid_floor: layout[1], +b field
#include "VNEW_floorTable.h"  // namespace vnew_floor: layout[2], +b+c; floor=1

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

static void die( const char * msg ) { fprintf( stderr, "R32 FAIL: %s\n", msg ); exit( 1 ); }

// save one fixed-form record into buf; returns byte count or -1 on error.
static int64_t save_one( uint8_t * buf, size_t cap,
                         const vold_floor::Floored & rec,
                         int64_t( *measure )( int64_t ),
                         int64_t( *save )( const vold_floor::Floored *, int64_t, uint8_t *, int64_t ) )
{
    int64_t need = measure( 1 );
    if ( need <= 0 || (size_t) need > cap ) return -1;
    int64_t wrote = save( &rec, 1, buf, need );
    return wrote == need ? (int64_t) wrote : -1;
}

// ---------------------------------------------------------------------------
// assertions: exit 0 green / exit 1 red, one printed line per assertion
// ---------------------------------------------------------------------------

int main()
{
    // ---- CELL ASSERTION (green case) ----
    //
    // Build a well-formed form-3 file whose header hash is vold_floor's
    // wire hash (lineage[0]). Load it against the vnew_floor reader whose
    // floor = 1. Since lineage_at = 0 < floor_at = 1, LOAD must refuse
    // with reason == layout_unsupported — the retired-version law.

    {
        vold_floor::Floored rec;
        vold_floor::FlooredReset( rec );
        rec.a = 42;

        uint8_t wire[ 256 ];
        int64_t wlen = save_one( wire, sizeof( wire ), rec,
                                 vold_floor::FlooredFixedMeasure,
                                 vold_floor::FlooredFixedSave );
        if ( wlen < 0 ) die( "cannot save vold_floor record" );

        // --- load against the vnew_floor reader (floor=1) ---
        vnew_floor::Floored back = {};
        vnew_floor::FlooredReset( back );

        vnew_floor::TableReport report;
        std::vector<vnew_floor::TableFixedEntry> plan( 4096 );

        int64_t n = vnew_floor::FlooredFixedLoad( &back, 1, wire, wlen,
                                                   plan.data(), (int32_t) plan.size(),
                                                   NULL, &report );

        // GREEN: refuses layout_unsupported (the floor check bites at index 0)
        // Only checks the core assertions: refused + correct reason. The
        // spec says a refusal decodes NOTHING, so zero counters is what matters.
        bool ok = ( n < 0
                 && report.refused
                 && report.reason == vnew_floor::layout_unsupported
                 && report.unknown == 0
                 && report.kind_mismatch == 0
                 && report.widened == 0
                 && report.clamped == 0
                 && !report.malformed );

        if ( !ok ) {
            printf( "GREEN: load of lineage[0] against floor=1 => n=%d refused=%d reason=%d unknown=%d km=%d wid=%d clamp=%d malformed=%d\n",
                    (int) n, (int) report.refused, (int) report.reason,
                    report.unknown, report.kind_mismatch, report.widened,
                    report.clamped, (int) report.malformed );
            die( "cell assertion FAILED: retired version not refused by name" );
        }
        printf( "GREEN: ok — retired version (lineage[0]) refused layout_unsupported at floor=1\n" );
    }

    // ---- NEGATIVE CONTROL: layout_newer vs layout_unsupported ----
    //
    // Corrupt one byte of the header's hash so it no longer matches lineage[0].
    // The loader will find NO matching lineage entry (lineage_at = -1) and
    // return layout_newer, NOT layout_unsupported. This proves the cell
    // assertion hit the FLOOR check specifically, not some generic failure.
    {
        vold_floor::Floored rec;
        vold_floor::FlooredReset( rec );
        rec.a = 42;

        uint8_t wire[ 256 ];
        int64_t wlen = save_one( wire, sizeof( wire ), rec,
                                 vold_floor::FlooredFixedMeasure,
                                 vold_floor::FlooredFixedSave );
        if ( wlen < 0 ) die( "cannot save for control" );

        // Corrupt the hash at offset 8 (header hash field): flip one bit.
        // The full header is kTableFixedHeaderBytes=16 bytes; hash sits at 8.
        wire[ 8 ] ^= 0x01;

        vnew_floor::Floored back = {};
        vnew_floor::FlooredReset( back );

        vnew_floor::TableReport report;
        std::vector<vnew_floor::TableFixedEntry> plan( 4096 );

        int64_t n = vnew_floor::FlooredFixedLoad( &back, 1, wire, wlen,
                                                   plan.data(), (int32_t) plan.size(),
                                                   NULL, &report );

        // If the hash no longer matches any lineage entry, the reader says
        // layout_newer — it never gets to the floor check at all.
        bool ok = ( n < 0
                 && report.refused
                 && report.reason == vnew_floor::layout_newer );

        if ( !ok ) {
            printf( "CONTROL: corrupted-hash load => n=%d refused=%d reason=%d\n",
                    (int) n, (int) report.refused, (int) report.reason );
            die( "negative control FAILED: corrupted hash did not yield layout_newer" );
        }
        printf( "CONTROL: ok — corrupted hash yields layout_newer, confirming cell hits floor-specific path\n" );
    }

    // ---- IDEMPOTENCY CHECK ----
    //
    // A retired version is refused ONCE and IDENTICALLY each time. Loading
    // the same file twice yields the same refusal both times.

    {
        vold_floor::Floored rec;
        vold_floor::FlooredReset( rec );
        rec.a = 99;

        uint8_t wire[ 256 ];
        int64_t wlen = save_one( wire, sizeof( wire ), rec,
                                 vold_floor::FlooredFixedMeasure,
                                 vold_floor::FlooredFixedSave );
        if ( wlen < 0 ) die( "cannot save for idempotency" );

        // Load twice with floor still at 1.
        vnew_floor::TableReport r1, r2;
        std::vector<vnew_floor::TableFixedEntry> plan( 4096 );

        vnew_floor::Floored b1 = {};
        vnew_floor::FlooredReset( b1 );
        vnew_floor::Floored b2 = {};
        vnew_floor::FlooredReset( b2 );

        int64_t n1 = vnew_floor::FlooredFixedLoad( &b1, 1, wire, wlen,
                                                    plan.data(), (int32_t) plan.size(),
                                                    NULL, &r1 );
        int64_t n2 = vnew_floor::FlooredFixedLoad( &b2, 1, wire, wlen,
                                                    plan.data(), (int32_t) plan.size(),
                                                    NULL, &r2 );

        bool ok = ( n1 < 0 && n2 < 0
                 && r1.refused && r2.refused
                 && r1.reason == vnew_floor::layout_unsupported
                 && r2.reason == vnew_floor::layout_unsupported
                 && r1.unknown == r2.unknown
                 && r1.kind_mismatch == r2.kind_mismatch
                 && r1.widened == r2.widened
                 && r1.clamped == r2.clamped
                 && r1.malformed == r2.malformed );

        if ( !ok ) {
            printf( "IDEMPOTENT: n1=%d n2=%d r1.refused=%d r2.refused=%d r1.reason=%d r2.reason=%d\n",
                    (int) n1, (int) n2, (int) r1.refused, (int) r2.refused,
                    (int) r1.reason, (int) r2.reason );
            die( "idempotency FAILED: identical input produced different outcomes" );
        }
        printf( "IDEMPOTENT: ok — two loads yield identical layout_unsupported refusal\n" );
    }

    printf( "PASS\n" );
    return 0;
}
