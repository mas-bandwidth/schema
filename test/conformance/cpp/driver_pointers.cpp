// THE POINTERED FAMILY of the C++ conformance driver's codec rows (driver.h):
// graphdemo (docs/SPEC-TABLES.md §6.2), the feature witnesses, streamdemo,
// the byte-buffer unit blobdemo (§2.5) and FL-19's map form, every root a
// region root; and the one cook-write sharing recipe, blobdemo's Catalog.

#include "driver.h"

#include "GraphTable.h"
#include "FeaturesTable.h"
#include "StreamTable.h"
#include "AssetsTable.h"
#include "MoodMapTable.h"

static const VarCodec var_codecs[] = {
    VARCODEC( "tblmoodmap", tblmoodmap, MoodMap ),
    VARCODEC( "graphdemo", graphdemo, Scene ),
    VARCODEC( "graphdemo", graphdemo, ListNode ),
    VARCODEC( "streamdemo", streamdemo, Feed ),
    VARCODEC( "blobdemo", blobdemo, Catalog ),
    VARCODEC( "featurewitnesses", featurewitnesses, Root ),
    VARCODEC( "featurewitnesses", featurewitnesses, ArrayRoot ),
};

const DriverTables driver_tables_pointers = {
    {}, span_of( var_codecs ), {}, {}, {}, {}, {},
};

// THE ONE SHARING RECIPE the corpus carries: blobdemo's Catalog with `alias`
// naming the very blob `thumb` names, set on the builder after the text read
bool cook_shared( const std::string & out, const std::string & name, const std::vector<std::string> & inst,
                  const std::vector<uint8_t> & text, const std::string & source, const std::string & alias )
{
    if ( inst[2] != "blobdemo" || inst[3] != "Catalog" || source != "thumb" || alias != "alias" )
    {
        fprintf( stderr, "driver: no sharing recipe for %s.%s %s:%s\n", inst[2].c_str(), inst[3].c_str(), source.c_str(), alias.c_str() );
        return false;
    }
    blobdemo::CatalogBuilder builder;
    blobdemo::TableReport report;
    if ( !blobdemo::CatalogFromJson( builder, (const char *) text.data(), (int64_t) text.size(), &report ) ) return false;
    blobdemo::Catalog * root = builder.GetRoot();
    if ( root == NULL || root->thumb.value == 0 ) return false;
    root->alias = root->thumb;
    const int64_t need = blobdemo::CatalogCookMeasure( builder );
    if ( need <= 0 ) return false;
    std::vector<uint8_t> file( (size_t) need );
    for ( int big = 0; big < 2; big++ )
    {
        if ( !blobdemo::CatalogCook( builder, file.data(), (uint64_t) need,
                                     big ? blobdemo::TableByteOrder::Big : blobdemo::TableByteOrder::Little ) ) return false;
        if ( !spill( out, big ? name + "-be" : name, file.data(), (size_t) need ) ) return false;
    }
    return true;
}
