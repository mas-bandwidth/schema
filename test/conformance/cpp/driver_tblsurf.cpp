// THE 2026-11-22 SURFACES' FAMILY of the C++ conformance driver's codec rows
// (driver.h): tblsurf, the shapes unit, the packet-unions unit and the
// retain-unknown reader tblrt1, with the read-into forms the last two answer.

#include "driver.h"

#include "SurfacesTable.h"
#include "ShapesTable.h"
#include "PacketUnionsTable.h"
#include "RT1Table.h"

static const Codec codecs[] = {
    // the SHAPES unit (test/tables/shapes), opted in by `list`
    CODEC( "shapes", shapes, FixedBounds ),
    // THE 2026-11-22 SURFACES' unit: Big is past the fixed form's ceiling
    CODEC( "tblsurf", surfaces, Narrow ),
    CODEC( "tblsurf", surfaces, Wide ),
    CODEC( "tblsurf", surfaces, Arrays ),
    NOFILE_CODEC( "tblsurf", surfaces, Big ),
    CODEC( "packetunions", tblpacketunions, ShapeMsg ),
};

static const VarCodec var_codecs[] = {
    VARCODEC( "shapes", shapes, Bounds ),
    VARCODEC( "packetunions", tblpacketunions, ShapeDoc ),
    VARCODEC( "tblsurf", surfaces, Chain ),
    VARCODEC( "tblsurf", surfaces, Holder ),
    VARCODEC( "tblsurf", surfaces, Counted ),
};

static const MsgCodec msg_codecs[] = {
    MSGCODEC( "tblsurf", surfaces, Narrow ),
    MSGCODEC( "tblsurf", surfaces, Wide ),
    MSGCODEC( "tblsurf", surfaces, Arrays ),
    MSGCODEC( "tblsurf", surfaces, Big ),
    MSGCODEC( "packetunions", tblpacketunions, ShapeMsg ),
};

static const RetainCodec retain_codecs[] = {
    RETAINCODEC( "tblrt1", tblrt1, Node ),
};

static const RetainSequenceCodec retain_sequence_codecs[] = {
    RETAINSEQUENCE( "tblrt1", tblrt1, Node ),
};

static const AnnounceUnit announce_units[] = {
    ANNOUNCEUNIT( "tblsurf", surfaces ),
};

// THE RETAINING LOAD's row (README, "The `retain` form"): ONE report, which
// the driver PRE-FILLS with every member set, through NodeLoadRetain of the
// lawful file, the damaged one and the lawful one again. After each lawful
// load the report must equal, MEMBER BY MEMBER, the report the same load
// leaves in a fresh report (docs/SPEC-TABLES.md §6.5: every read zeroes a
// supplied report in full as its first act, the retain family included).
// Every member of TableReport is public, so the pre-fill sets each directly.
static const char * retain_into_step( bool ok, bool refused )
{
    if ( ok && !refused ) return "read\n";
    if ( !ok && refused ) return "refused\n";
    return ok ? "read-with-refused-report\n" : "refused-with-clean-report\n";
}

static bool retain_into_load( const std::vector<uint8_t> & wire, tblrt1::TableReport & report )
{
    int64_t need = tblrt1::NodeLoadMeasure( wire.data(), (int64_t) wire.size() );
    if ( need < 0 ) need = 0;
    std::vector<uint8_t> raw( (size_t) need + 64, 0 );
    uint8_t * region = raw.data() + ( ( 64 - ( (uintptr_t) raw.data() & 63 ) ) & 63 );
    std::vector<uint8_t> bytes( (size_t) kRetainRoomy, 0 );
    std::vector<tblrt1::TableRetain::Id> ids( (size_t) ( kRetainRoomy / 8 ) );
    tblrt1::TableRetain retain;
    retain.bytes = bytes.data();
    retain.capacity = kRetainRoomy;
    retain.ids = ids.data();
    retain.id_capacity = (int32_t) ids.size();
    return tblrt1::NodeLoadRetain( region, need, wire.data(), (int64_t) wire.size(), &retain, &report ) != NULL;
}

template <typename R>
static std::string retain_into_compare( const R & r, const R & fresh )
{
    std::string kept;
    char line[512];
#define RETAIN_INTO_MEMBER( m )                                                                                   \
    if ( r.m != fresh.m )                                                                                         \
    {                                                                                                             \
        snprintf( line, sizeof( line ), "kept " #m " %lld want %lld\n", (long long) r.m, (long long) fresh.m );    \
        kept += line;                                                                                             \
    }
    RETAIN_INTO_MEMBER( unknown )
    RETAIN_INTO_MEMBER( widened )
    RETAIN_INTO_MEMBER( missing_required )
    RETAIN_INTO_MEMBER( damaged )
    RETAIN_INTO_MEMBER( refused )
    RETAIN_INTO_MEMBER( reason.value )
    RETAIN_INTO_MEMBER( damage_reason )
    RETAIN_INTO_MEMBER( retained )
    RETAIN_INTO_MEMBER( retain_lost )
    RETAIN_INTO_MEMBER( refuse_reason )
    RETAIN_INTO_MEMBER( non_finite_floats )
    RETAIN_INTO_MEMBER( build_version_remote )
    RETAIN_INTO_MEMBER( build_version_own )
    RETAIN_INTO_MEMBER( layout_hash )
#undef RETAIN_INTO_MEMBER
    if ( strcmp( r.missing_required_fields, fresh.missing_required_fields ) != 0 )
    {
        kept += std::string( "kept missing_required_fields \"" ) + r.missing_required_fields + "\" want \"" +
                fresh.missing_required_fields + "\"\n";
    }
    return kept.empty() ? "zeroed\n" : kept;
}

// THE ZEROING PROTOCOL shared by the `retain` and `region` forms: a fresh
// report from the lawful load, then ONE pre-filled report through the lawful
// load, the damaged one and the lawful one again, compared after each lawful
// load. Every member of TableReport is public, so the pre-fill sets each one.
template <typename R, typename RR, typename Load>
static int read_into_zeroing( const char * what, const std::vector<uint8_t> & lawful, const std::vector<uint8_t> & damaged,
                              std::string & answer, Load load )
{
    R fresh;
    if ( !load( lawful, fresh ) || fresh.refused )
    {
        fprintf( stderr, "driver: read-into: the lawful %s load refused into a fresh report\n", what );
        return 2;
    }
    R report;
    report.unknown = 7;
    report.widened = 7;
    report.missing_required = 7;
    snprintf( report.missing_required_fields, sizeof( report.missing_required_fields ), "stale" );
    report.damaged = true;
    report.refused = true;
    report.reason = RR::malformed;
    report.damage_reason = RR::integrity_mismatch;
    report.retained = 7;
    report.retain_lost = 7;
    report.refuse_reason = RR::malformed;
    report.non_finite_floats = 7;
    report.build_version_remote = 7;
    report.build_version_own = 7;
    report.layout_hash = 7;
    auto step = [&]( const std::vector<uint8_t> & in ) {
        try
        {
            const bool ok = load( in, report );
            answer += retain_into_step( ok, read_into_refused( report ) );
        }
        catch ( ... )
        {
            answer += "threw\n";
        }
    };
    step( lawful );
    answer += retain_into_compare( report, fresh );
    step( damaged );
    step( lawful );
    answer += retain_into_compare( report, fresh );
    return 0;
}

static int read_into_retain( const std::string & form, const std::vector<uint8_t> & lawful, const std::vector<uint8_t> & damaged,
                             int64_t count, const std::vector<uint8_t> &, std::string & answer )
{
    if ( form != "retain" || count != 1 ) return 1;
    return read_into_zeroing<tblrt1::TableReport, tblrt1::TableRefuseReason>( "retaining", lawful, damaged, answer, retain_into_load );
}

// THE PLAIN REGION READ (README, "The `region` form"): surfaces::CountedLoad
// into a 64-aligned region of its measured size, under the protocol above.
static bool region_into_load( const std::vector<uint8_t> & wire, surfaces::TableReport & report )
{
    int64_t need = surfaces::CountedLoadMeasure( wire.data(), (int64_t) wire.size() );
    if ( need < 0 ) need = 0;
    std::vector<uint8_t> raw( (size_t) need + 64, 0 );
    uint8_t * region = raw.data() + ( ( 64 - ( (uintptr_t) raw.data() & 63 ) ) & 63 );
    return surfaces::CountedLoad( region, need, wire.data(), (int64_t) wire.size(), &report ) != NULL;
}

int read_into_region( const std::vector<uint8_t> & lawful, const std::vector<uint8_t> & damaged, int64_t count, std::string & answer )
{
    if ( count != 1 ) return 1;
    return read_into_zeroing<surfaces::TableReport, surfaces::TableRefuseReason>( "region", lawful, damaged, answer, region_into_load );
}

static const ReadIntoCodec read_into_codecs[] = {
    { "tblrt1", "Node", read_into_retain },
    READINTO_VAR( "tblsurf", surfaces, Counted ),
    READINTO_FIXED( "tblsurf", surfaces, Narrow ),
};

const DriverTables driver_tables_tblsurf = {
    span_of( codecs ), span_of( var_codecs ), span_of( msg_codecs ), span_of( retain_codecs ),
    span_of( retain_sequence_codecs ), span_of( announce_units ), span_of( read_into_codecs ),
};
