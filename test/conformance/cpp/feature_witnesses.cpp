// Produce the two populated, valid feature witnesses with the C++ table API.
// Existing wire and JSON pins remain independent, read-only comparisons.
#include "FeaturesTable.h"

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <iterator>
#include <string>
#include <vector>

using namespace featurewitnesses;

static void require( bool ok, const char * what )
{
    if ( !ok ) { std::fprintf( stderr, "feature witness: %s\n", what ); std::exit( 1 ); }
}

static std::vector<uint8_t> read_file( const std::string & path )
{
    std::ifstream in( path, std::ios::binary );
    require( in.good(), path.c_str() );
    return std::vector<uint8_t>( std::istreambuf_iterator<char>( in ), std::istreambuf_iterator<char>() );
}

static void write_file( const std::string & path, const std::vector<uint8_t> & bytes )
{
    std::ofstream out( path, std::ios::binary );
    require( out.good(), path.c_str() );
    out.write( (const char *) bytes.data(), (std::streamsize) bytes.size() );
    out.close();
    require( out.good(), "write output" );
}

static bool equal_text( const char * data, int64_t n, const char * expected )
{
    return data != NULL && n == (int64_t) std::strlen( expected ) &&
           std::memcmp( data, expected, (size_t) n ) == 0;
}

static void check_root( const Root * r )
{
    require( r != NULL && r->revision == 7 && r->at == INT64_C(1793622896123456789) && r->lower == 9,
             "Root scalar values" );
    require( r->tags_present && r->tags.size() == 2, "Root tags count" );
    const RootTagsEntry * tags = r->tags.Entries();
    require( tags && equal_text( tags[0].key, tags[0].key_length, "a" ) && tags[0].value == 3 &&
             equal_text( tags[1].key, tags[1].key_length, "z" ) && tags[1].value == 11, "Root tags" );
    require( r->optional_present && !r->optional.child.null(), "Root optional child" );
    const Leaf * child = (const Leaf *) ( (const uint8_t *) &r->optional.child + r->optional.child.value );
    require( child->value == 17, "Root optional child value" );
    require( r->labels_count == 2 && equal_text( r->labels[0].value, r->labels[0].value_length, "\xC3\xA9" ) &&
             equal_text( r->labels[1].value, r->labels[1].value_length, "schema" ), "Root labels" );
    require( r->optional_labels.size() == 3, "Root optional labels count" );
    const RootOptionalLabelsText * labels = r->optional_labels.Elements();
    require( labels && !labels[0].value_present && labels[1].value_present && labels[1].value_length == 0 &&
             labels[2].value_present && equal_text( labels[2].value, labels[2].value_length, "active" ),
             "Root optional labels" );
    require( r->words.size() == 1, "Root words count" );
    const TableStringView word = TableStringAt( r->words.Elements()[0].value );
    require( equal_text( word.data, word.length, "unbounded text is populated" ), "Root word" );
    require( r->by_name.size() == 1, "Root by_name count" );
    const RootByNameEntry * entry = r->by_name.Entries();
    const TableStringView key = TableStringAt( entry[0].key );
    require( entry[0].value == 23 && equal_text( key.data, key.length, "a key longer than eight bytes" ),
             "Root by_name" );
}

static void check_array( const ArrayRoot * r )
{
    require( r && r->values.size() == 3 && r->values[0] == 3 && r->values[1] == 17 && r->values[2] == -5,
             "ArrayRoot values" );
}

int main( int argc, char ** argv )
{
    require( argc == 2, "expected output directory" );
    const std::string base = "testdata/conformance/feature-witnesses/full/";
    const std::string out = argv[1];
    const std::vector<uint8_t> root_input = read_file( base + "Root.input.json" );
    RootBuilder root_builder;
    TableReport report = {};
    require( RootFromJson( root_builder, (const char *) root_input.data(), (int64_t) root_input.size(), &report ), "Root JSON parse" );
    require( !report.refused && !report.damaged && report.unknown == 0 && report.widened == 0 && report.missing_required == 0, "Root JSON report" );
    require( root_builder.Lock(), "Root lock" );
    check_root( root_builder.AsConst() );
    int64_t json_size = RootToJsonMeasure( root_builder.AsConst() );
    require( json_size > 0, "Root JSON measure" );
    std::vector<uint8_t> json( (size_t) json_size );
    require( RootToJson( root_builder.AsConst(), (char *) json.data(), json_size ) == json_size, "Root JSON write" );
    if ( json.empty() || json.back() != '\n' ) { json.push_back( '\n' ); }
    require( json == read_file( "testdata/conformance/tables/json/feature_full.json" ), "Root JSON differs from pin" );
    int64_t size = RootMeasure( root_builder );
    require( size > 0, "Root measure" );
    std::vector<uint8_t> root_wire( (size_t) size );
    require( RootSave( root_builder, root_wire.data(), size ) == size, "Root save" );
    require( root_wire == read_file( base + "Root.bin" ), "Root wire differs from pin" );
    int64_t region_size = RootLoadMeasure( root_wire.data(), size );
    require( region_size > 0, "Root load measure" );
    std::vector<uint8_t> region( (size_t) region_size );
    check_root( RootLoad( region.data(), region_size, root_wire.data(), size, &report ) );
    require( !report.refused && !report.damaged && report.unknown == 0 && report.widened == 0 && report.missing_required == 0, "Root read report" );
    write_file( out + "/Root.bin", root_wire );

    const std::vector<uint8_t> array_input = read_file( base + "ArrayRoot.input.json" );
    ArrayRootBuilder array_builder;
    report = {};
    require( ArrayRootFromJson( array_builder, (const char *) array_input.data(), (int64_t) array_input.size(), &report ), "ArrayRoot JSON parse" );
    require( !report.refused && !report.damaged && report.unknown == 0 && report.widened == 0 && report.missing_required == 0, "ArrayRoot JSON report" );
    require( array_builder.Lock(), "ArrayRoot lock" );
    check_array( array_builder.AsConst() );
    json_size = ArrayRootToJsonMeasure( array_builder.AsConst() );
    require( json_size > 0, "ArrayRoot JSON measure" );
    json.assign( (size_t) json_size, 0 );
    require( ArrayRootToJson( array_builder.AsConst(), (char *) json.data(), json_size ) == json_size, "ArrayRoot JSON write" );
    if ( json.empty() || json.back() != '\n' ) { json.push_back( '\n' ); }
    require( json == read_file( "testdata/conformance/tables/json/feature_array_root.json" ), "ArrayRoot JSON differs from pin" );
    size = ArrayRootMeasure( array_builder );
    require( size > 0, "ArrayRoot measure" );
    std::vector<uint8_t> array_wire( (size_t) size );
    require( ArrayRootSave( array_builder, array_wire.data(), size ) == size, "ArrayRoot save" );
    require( array_wire == read_file( base + "ArrayRoot.bin" ), "ArrayRoot wire differs from pin" );
    region_size = ArrayRootLoadMeasure( array_wire.data(), size );
    require( region_size > 0, "ArrayRoot load measure" );
    region.assign( (size_t) region_size, 0 );
    check_array( ArrayRootLoad( region.data(), region_size, array_wire.data(), size, &report ) );
    require( !report.refused && !report.damaged && report.unknown == 0 && report.widened == 0 && report.missing_required == 0, "ArrayRoot read report" );
    write_file( out + "/ArrayRoot.bin", array_wire );
    std::puts( "C++ populated feature witnesses match native values and wire pins" );
    return 0;
}
