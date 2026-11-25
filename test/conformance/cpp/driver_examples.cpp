// THE EXAMPLES FAMILY of the C++ conformance driver's codec rows (driver.h):
// tabledemo (tables/examples), the two scalar units and cooktorturewide.

#include "driver.h"

#include "TablesTable.h"
#include "WideTable.h"
#include "NestedTable.h"
#include "KeyedTable.h"
#include "PackTable.h"
#include "ScalarsTable.h"
#include "Scalars2Table.h"
#include "CookTortureWideTable.h"

static const Codec codecs[] = {
    CODEC( "tabledemo", tabledemo, RootConfig ),
    CODEC( "tabledemo", tabledemo, ProfileConfig ),
    CODEC( "tabledemo", tabledemo, LoadoutConfig ),
    NOFILE_CODEC( "tabledemo", tabledemo, WideBlob ),
    CODEC( "tabledemo", tabledemo, ArchiveConfig ),
    CODEC( "tabledemo", tabledemo, KeyedConfig ),
    CODEC( "tabledemo", tabledemo, PackConfig ),
    CODEC( "tabledemo", tabledemo, CountedMin ),
    CODEC( "tabledemo", tabledemo, FloatBound ),
    CODEC( "tabledemo", tabledemo, Moods ),
    CODEC( "tabledemo", tabledemo, MoodField ),
    CODEC( "tabledemo", tabledemo, MinArmFix ),
    CODEC( "tabledemo", tabledemo, MinPlainFix ),
    CODEC( "scalars", scalardemo, SimState ),
    CODEC( "tblscalars2", scalardemo2, SimState ),
};

static const VarCodec var_codecs[] = {
    VARCODEC( "cooktorturewide", cooktorturewide, TortureWideCook ),
};

const DriverTables driver_tables_examples = {
    span_of( codecs ), span_of( var_codecs ), {}, {}, {}, {}, {},
};

// A VALUE THE WRITER REFUSES (docs/SPEC-TABLES.md §16.14, W0): the line names
// a wire that reads, and the damage the driver puts into it afterwards, which no
// read can deliver: the bytes 61 FF 62 in a narrow text field, the units 0061
// D800 0062 in a wide one (testdata/conformance/tables/FORMAT.md,
// `json-write-refusal`), or `none` where the value is already past what a text
// may carry. The answer is `refused`, never a text.
bool damage_text( const std::string & unit, const std::string & root, void * value, const std::string & damage )
{
    if ( damage == "none" ) return true;
    if ( damage == "narrow:name" && unit == "tabledemo" && root == "ProfileConfig" )
    {
        tabledemo::ProfileConfig & v = *(tabledemo::ProfileConfig *) value;
        v.name[0] = 0x61; v.name[1] = (char) 0xff; v.name[2] = 0x62;
        v.name_length = 3;
        return true;
    }
    fprintf( stderr, "driver: damage %s names no text field of %s.%s\n", damage.c_str(), unit.c_str(), root.c_str() );
    return false;
}
