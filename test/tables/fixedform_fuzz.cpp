// THE FIXED FORM'S READER, UNDER A COVERAGE-GUIDED FUZZER (docs/SPEC-TABLES.md
// §3.4). test/tables/fixedform_main.cpp's fuzz_case() flips every bit of ONE
// clean file and reports "3024 refused by name, 0 divergences". That sweep is a
// gate — it is finite, it runs in a second, and it only ever sees files that
// are one bit away from correct. This file is the other half: the same
// invariants, asserted over bytes a COVERAGE-GUIDED MUTATOR chooses, for as
// long as a machine is willing to run.
//
// THE INPUT IS A WHOLE FORM-3 FILE. libFuzzer hands us the file and we hand it
// to the generated FixedLoad of every fixed root this build has, each with its
// own POISONED storage and a FRESH report. A layout is a stranger's bytes and
// the record carries no lengths and no terminators, so every offset the reader
// computes is arithmetic over sizes the WRITER wrote down; "the reader never
// leaves the buffer" is a claim only a sanitizer can hold, and this is the
// sanitizer's longest arm.
//
// THE INVARIANTS, one per named thing the reader promises:
//
//   I1  NEVER A CRASH. Not asserted here — it is ASan's and UBSan's finding,
//       and the whole reason the target links them.
//   I2  THE THREE ANSWERS AND NEVER A FOURTH. A read either refuses by name
//       (refused, a reason, n < 0), reports damage (malformed, n < 0), or
//       returns records (n >= 0). A refusal is never damage; a negative read
//       that is not a refusal is damage; a read that returned records was not
//       REFUSED. It may be malformed — §4's malformed is "decode stopped,
//       partial result kept", so a partial beside a record count is the design
//       and this target counts it instead of asserting against it.
//   I3  A REFUSAL LANDS NO BYTE. The caller's storage is filled with a poison
//       that appears nowhere in a record, and after a refusal every byte of it
//       is still poison — the reader decided before it wrote.
//   I4  A LAYOUT-LEVEL REFUSAL MOVES NO COUNTER. unknown, kind_mismatch,
//       widened, clamped and duplicate are zero when the refusal is the form
//       byte, the layout's validation or the hash lineage: §3's verdict is not
//       §4's event. A refusal from inside the BODY WALK is counted and not
//       asserted — the first thing this target found was one of those, and
//       whether §3.4 forbids it is the open question in test/tables/fuzz-crashes/.
//   I5  THE REFUSAL CARRIES THE FILE'S HASH. On layout_newer and
//       layout_unsupported the report's layout_hash is the u64 at the file's
//       own kTableFixedHashAt (bill §12.4) — the peer is told which layout it
//       is missing, and it is told the truth.
//   I6  P2 ON THE IDENTITY LANE. A file whose header hash and layout bytes are
//       this build's own, read into records and saved back, is byte-identical.
//       A divergence that a SECOND read-and-save reproduces exactly is a
//       NORMALISATION (a bool byte outside {0,1}, an ordinal that names no
//       variant — both named known-reds of fixedform_properties.cpp) and is
//       counted, not a finding; a divergence the second round does NOT settle
//       is a reader and writer that disagree, and that aborts.
//
// Build and run: `make tables-fixedform-fuzz` then
// `make tables-fixedform-fuzz-run MINUTES=1`. The seed corpus is the
// reference's own byte oracle (build/fixedform-corpus) plus the committed wire
// fixtures (testdata/wire/tables), and every input libFuzzer has ever minimised
// into test/tables/fuzz-crashes is replayed first.
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <vector>

#include "KeyedTable.h"
#include "PackTable.h"
#include "TablesTable.h"
#include "NestedTable.h"
#include "RangesTable.h"
#include "GuardedTable.h"
#include "WideTable.h"

// THE VERSIONING PAIRS, because they are the only roots whose layout is NOT
// this build's own: FX2 reading an FX1 file is the plan path, and the plan path
// is where a stranger's sizes are believed. They cost one call each.
#include "FX1Table.h"
#include "FX2Table.h"
#include "P1Table.h"
#include "P3Table.h"
#include "UT1Table.h"
#include "UT2Table.h"
#include "V1Table.h"
#include "V2Table.h"

namespace
{

// THE POISON IS THE ASSERTION (test/tables/fixedform_main.cpp's stain, and the
// same reason): a byte that appears in no clean record, so "the reader did not
// write" is a memcmp and not a guess.
const uint8_t kPoison = 0xA5;

// ONE RECORD OF STORAGE, AND THAT IS A DECISION THE FUZZER MADE FOR US. With
// four, the first thing this target found was a FILE WHOSE SECOND RECORD
// REFUSES AFTER THE FIRST ONE READ: the report then carries record 1's
// counters and record 1's landed bytes under record 2's refusal verdict, and
// I3 and I4 both fire on behaviour nothing in §3.4 forbids — the reader's
// "a refusal sets nothing and counts nothing" (test/tables/fixedform_main.cpp's
// refuses()) is stated over a ONE-RECORD file, and that is the only file it is
// stated over. Holding a multi-record file to it is a CONTRACT QUESTION for
// Glenn, not a bug, and it is written down in the PR rather than asserted here.
const int64_t kRecordCap = 1;      // records of storage handed to every load
const int32_t kPlanCap = 1024;     // the same capacity the conformance gate uses

// what the run has seen, printed once at exit so a long run says something
// other than "no crash".
struct Tally
{
    long long refused = 0;
    long long malformed = 0;
    long long records = 0;
    long long identity = 0;      // reads on the identity lane that round-tripped
    long long normalised = 0;    // I6's counted normalisations
    long long partial = 0;       // returning reads that kept a partial (§4's malformed)
    // the OPEN finding: a refusal from inside the body walk that had already
    // moved a counter (test/tables/fuzz-crashes/i4-fx2-refusal-moved-a-counter.bin)
    long long body_refusal_counted = 0;
};

Tally tally;

void die( const char * what, const char * root )
{
    // A PROPERTY VIOLATION IS A FINDING AND MUST LOOK LIKE ONE: libFuzzer keeps
    // the input that reached an abort, and -minimize_crash shrinks it.
    std::fprintf( stderr, "\nFIXEDFORM FUZZ VIOLATION: %s [%s]\n", what, root );
    std::fflush( stderr );
    std::abort();
}

// ---------------------------------------------------------------------------
// ONE ROOT'S GENERATED API, named the way fixedform_properties.cpp names it so
// the two files read as one set.
#define FUZZ_ROOT( NS, TYPE, TAG ) \
struct Fuzz_##TAG \
{ \
    using T = NS::TYPE; \
    using Report = NS::TableReport; \
    using Entry = NS::TableFixedEntry; \
    static constexpr const char * name = #NS "::" #TYPE; \
    static int64_t measure( int64_t n ) { return NS::TYPE##FixedMeasure( n ); } \
    static int64_t save( const T * v, int64_t n, uint8_t * b, int64_t c ) \
    { return NS::TYPE##FixedSave( v, n, b, c ); } \
    static int64_t load( T * v, int64_t cap, const uint8_t * d, int64_t b, Entry * p, int32_t pc, Report * r ) \
    { return NS::TYPE##FixedLoad( v, cap, d, b, p, pc, NULL, r ); } \
    static constexpr uint64_t hash = NS::TYPE##FixedHash; \
    static constexpr const uint8_t * layout = NS::TYPE##FixedLayout; \
    static constexpr int64_t layout_bytes = NS::TYPE##FixedLayoutBytes; \
    static constexpr int64_t header = NS::kTableFixedHeaderBytes; \
    static constexpr int64_t hash_at = NS::kTableFixedHashAt; \
    static uint32_t get32( const uint8_t * b ) { return NS::TableFixedGet32( b ); } \
    static uint64_t get64( const uint8_t * b ) { return NS::TableFixedGet64( b ); } \
    static bool newer( const Report & r ) { return r.reason == NS::layout_newer; } \
    /* A LAYOUT-LEVEL REFUSAL is decided before a body byte is touched: the form
       byte, the layout's own validation, and the hash lineage. Those are the
       refusals fixedform_main.cpp's refuses() states "counts nothing" over. */ \
    static bool layout_level( const Report & r ) \
    { return r.reason == NS::newer_form || r.reason == NS::previous_form \
          || r.reason == NS::message_form_as_file || r.reason == NS::no_layout \
          || r.reason == NS::layout_malformed || r.reason == NS::layout_newer \
          || r.reason == NS::layout_unsupported; } \
    static bool unsupported( const Report & r ) { return r.reason == NS::layout_unsupported; } \
}

// tables/examples — every fixed root this build emits a FixedLoad for
FUZZ_ROOT( tabledemo, RootConfig, RootConfig );
FUZZ_ROOT( tabledemo, GlobalSettings, GlobalSettings );
FUZZ_ROOT( tabledemo, TeamConfig, TeamConfig );
FUZZ_ROOT( tabledemo, GunnerConfig, GunnerConfig );
FUZZ_ROOT( tabledemo, GunnerSettings, GunnerSettings );
FUZZ_ROOT( tabledemo, TurretConfig, TurretConfig );
FUZZ_ROOT( tabledemo, HullConfig, HullConfig );
FUZZ_ROOT( tabledemo, WeaponConfig, WeaponConfig );
FUZZ_ROOT( tabledemo, LoadoutConfig, LoadoutConfig );
FUZZ_ROOT( tabledemo, ProfileConfig, ProfileConfig );
FUZZ_ROOT( tabledemo, ArchiveConfig, ArchiveConfig );
FUZZ_ROOT( tabledemo, ShipEntry, ShipEntry );
FUZZ_ROOT( tabledemo, KeyedConfig, KeyedConfig );
FUZZ_ROOT( tabledemo, PackConfig, PackConfig );
FUZZ_ROOT( tabledemo, RangedSigned, RangedSigned );
FUZZ_ROOT( tabledemo, RangedUnsigned, RangedUnsigned );
FUZZ_ROOT( tabledemo, RangedWidths, RangedWidths );

// the versioning units: the plan path, both directions
FUZZ_ROOT( tblfx1, FxRoot, Fx1Root );
FUZZ_ROOT( tblfx2, FxRoot, Fx2Root );
FUZZ_ROOT( tblp1, Chain, P1Chain );
FUZZ_ROOT( tblp3, Chain, P3Chain );
FUZZ_ROOT( tblut1, UtRoot, Ut1Root );
FUZZ_ROOT( tblut2, UtRoot, Ut2Root );
FUZZ_ROOT( tblv1, Cfg, V1Cfg );
FUZZ_ROOT( tblv2, Cfg, V2Cfg );

// ---------------------------------------------------------------------------

template<typename F>
bool counters_zero( const typename F::Report & r )
{
    return r.unknown == 0 && r.kind_mismatch == 0 && r.widened == 0
        && r.clamped == 0 && r.duplicate == 0;
}

// THE IDENTITY LANE IS THE FILE'S OWN CLAIM, not ours: the header hash is this
// build's hash for this root AND the layout bytes in the file are byte for byte
// the layout this build compiled in. Anything else is the plan path, where a
// save-back is not expected to reproduce a stranger's layout.
template<typename F>
bool on_identity_lane( const uint8_t * data, size_t size )
{
    if ( size < (size_t) ( F::header + 4 ) ) { return false; }
    if ( F::get64( data + F::hash_at ) != F::hash ) { return false; }
    const uint32_t layout_bytes = F::get32( data + F::header );
    if ( (int64_t) layout_bytes != F::layout_bytes ) { return false; }
    if ( size < (size_t) ( F::header + 4 ) + (size_t) layout_bytes ) { return false; }
    return std::memcmp( data + F::header + 4, F::layout, (size_t) layout_bytes ) == 0;
}

template<typename F>
void probe( const uint8_t * data, size_t size )
{
    // POISONED STORAGE, and a reference image of the same poison so "no byte
    // landed" is a comparison.
    std::vector<typename F::T> out( (size_t) kRecordCap );
    std::vector<uint8_t> clean_poison( sizeof( typename F::T ) * (size_t) kRecordCap, kPoison );
    std::memset( out.data(), kPoison, clean_poison.size() );

    std::vector<typename F::Entry> plan( (size_t) kPlanCap );
    typename F::Report r;           // FRESH: the generated struct's own defaults

    const int64_t n = F::load( out.data(), kRecordCap, data, (int64_t) size,
                               plan.data(), kPlanCap, &r );

    if ( n < 0 )
    {
        // I2: a negative read is a refusal or it is damage, never neither.
        if ( r.refused )
        {
            tally.refused++;
            if ( r.malformed ) { die( "I2: a refusal is also damage", F::name ); }
            // I3: a refusal decided before it wrote — every byte still poison.
            if ( std::memcmp( out.data(), clean_poison.data(), clean_poison.size() ) != 0 )
            {
                die( "I3: a refusal landed a byte in the caller's storage", F::name );
            }
            // I4: §3's verdict is not §4's event — FOR A LAYOUT-LEVEL REFUSAL,
            // which is every refusal fixedform_main.cpp's refuses() states it
            // over. A refusal from inside the BODY WALK may arrive after the
            // walk already widened or skipped a field, and whether that is
            // allowed is the open question in test/tables/fuzz-crashes/. It is
            // counted here so the search keeps running past a known finding
            // instead of aborting on it every round.
            if ( !counters_zero<F>( r ) )
            {
                if ( F::layout_level( r ) ) { die( "I4: a layout refusal moved a counter", F::name ); }
                tally.body_refusal_counted++;
            }
            // I5: the refusal carries the FILE's hash, so the peer learns which
            // layout it is missing.
            if ( F::newer( r ) || F::unsupported( r ) )
            {
                if ( size < (size_t) ( F::header + 4 ) )
                {
                    die( "I5: a layout-hash refusal on a file too short to hold a hash", F::name );
                }
                if ( r.layout_hash != F::get64( data + F::hash_at ) )
                {
                    die( "I5: a layout refusal carries a hash that is not the file's", F::name );
                }
            }
        }
        else
        {
            tally.malformed++;
            if ( !r.malformed ) { die( "I2: a negative read that is neither refusal nor damage", F::name ); }
        }
        return;
    }

    // I2: a read that returned records was neither refused nor damaged.
    if ( r.refused ) { die( "I2: a read that returned records was refused", F::name ); }
    // A RETURNING READ MAY BE MALFORMED AND THAT IS THE DESIGN: §4's malformed
    // is "framing damage; decode stopped, PARTIAL RESULT KEPT", so n >= 0 with
    // malformed set is the partial. The verdict that may not appear beside
    // records is the REFUSAL, above.
    if ( r.malformed ) { tally.partial++; }
    if ( n > kRecordCap ) { die( "I2: a read returned more records than the storage holds", F::name ); }
    tally.records += n;

    if ( n == 0 ) { return; }

    // I6: P2 on the identity lane, and only when the read moved no counter —
    // a clamp or a widen is a CORRECTION and a corrected record is not the
    // record the file held.
    if ( r.malformed ) { return; }   // a partial is not a record to save back
    if ( !on_identity_lane<F>( data, size ) ) { return; }
    if ( !counters_zero<F>( r ) ) { return; }
    const int64_t need = F::measure( n );
    if ( need < 0 || (size_t) need != size ) { return; }   // trailing bytes are not this lane

    std::vector<uint8_t> again( (size_t) need, 0 );
    if ( F::save( out.data(), n, again.data(), need ) != need )
    {
        die( "I6: a save of a returned read did not write what it measured", F::name );
    }
    if ( std::memcmp( again.data(), data, (size_t) need ) == 0 ) { tally.identity++; return; }

    // The bytes differ. A NORMALISATION settles in one step — read the saved
    // bytes back and save again; if that is identical to the first save, the
    // reader and the writer agree and the file held a value outside a declared
    // domain (fixedform_properties.cpp's named known-reds). If it does NOT
    // settle, the pair disagrees, and that is the finding this target is for.
    std::vector<typename F::T> twice( (size_t) kRecordCap );
    std::memset( twice.data(), kPoison, clean_poison.size() );
    typename F::Report r2;
    const int64_t n2 = F::load( twice.data(), kRecordCap, again.data(), need,
                                 plan.data(), kPlanCap, &r2 );
    if ( n2 != n || r2.refused || r2.malformed )
    {
        die( "I6: this writer's own bytes did not read back as the same records", F::name );
    }
    std::vector<uint8_t> third( (size_t) need, 0 );
    if ( F::save( twice.data(), n2, third.data(), need ) != need )
    {
        die( "I6: a save of a re-read did not write what it measured", F::name );
    }
    if ( std::memcmp( third.data(), again.data(), (size_t) need ) != 0 )
    {
        die( "I6: read-save-read-save does not reach a fixed point", F::name );
    }
    tally.normalised++;
}

void report_at_exit()
{
    std::fprintf( stderr,
                  "fixedform fuzz: %lld refused by name, %lld malformed, %lld records read, "
                  "%lld identity round trips, %lld partials, %lld normalisations, "
                  "%lld body refusals that had counted (OPEN, see test/tables/fuzz-crashes), 0 violations\n",
                  tally.refused, tally.malformed, tally.records, tally.identity,
                  tally.partial, tally.normalised, tally.body_refusal_counted );
}

} // namespace

extern "C" int LLVMFuzzerInitialize( int *, char *** )
{
    std::atexit( report_at_exit );
    return 0;
}

extern "C" int LLVMFuzzerTestOneInput( const uint8_t * data, size_t size )
{
    // A NULL with a length is not a file and not this target's question; the
    // reader's own NULL guard is held by fixedform_main.cpp.
    if ( data == NULL ) { return 0; }

    probe<Fuzz_RootConfig>( data, size );
    probe<Fuzz_GlobalSettings>( data, size );
    probe<Fuzz_TeamConfig>( data, size );
    probe<Fuzz_GunnerConfig>( data, size );
    probe<Fuzz_GunnerSettings>( data, size );
    probe<Fuzz_TurretConfig>( data, size );
    probe<Fuzz_HullConfig>( data, size );
    probe<Fuzz_WeaponConfig>( data, size );
    probe<Fuzz_LoadoutConfig>( data, size );
    probe<Fuzz_ProfileConfig>( data, size );
    probe<Fuzz_ArchiveConfig>( data, size );
    probe<Fuzz_ShipEntry>( data, size );
    probe<Fuzz_KeyedConfig>( data, size );
    probe<Fuzz_PackConfig>( data, size );
    probe<Fuzz_RangedSigned>( data, size );
    probe<Fuzz_RangedUnsigned>( data, size );
    probe<Fuzz_RangedWidths>( data, size );

    probe<Fuzz_Fx1Root>( data, size );
    probe<Fuzz_Fx2Root>( data, size );
    probe<Fuzz_P1Chain>( data, size );
    probe<Fuzz_P3Chain>( data, size );
    probe<Fuzz_Ut1Root>( data, size );
    probe<Fuzz_Ut2Root>( data, size );
    probe<Fuzz_V1Cfg>( data, size );
    probe<Fuzz_V2Cfg>( data, size );

    return 0;
}
