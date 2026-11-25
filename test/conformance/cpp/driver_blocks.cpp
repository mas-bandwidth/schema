// THE BLOCK SURFACES of the C++ conformance driver (test/conformance/README.md;
// docs/SPEC-TABLES.md §19): open, the cross-endian refusal, the row dump, the
// forgery battery and the reason a refusal names, over the block walk that
// build/conformance-cpp-block compiles too (block_walk.h).

#include "driver.h"
#include "block_walk.h"

// foreign() reverses the MAGIC word — the eight bytes at offset 0 — which is
// what that word looks like to a reader of the OTHER byte order (§19.1, §7.1).
// It makes the file foreign to WHOEVER READS IT rather than to a particular
// host, so the refusal lands on the magic check every Open puts first and the
// expectation is `refuse` for every leg on every machine.
static std::vector<uint8_t> foreign( const std::vector<uint8_t> & data )
{
    std::vector<uint8_t> out = data;
    if ( out.size() >= 8 )
    {
        for ( int i = 0; i < 4; i++ )
        {
            const uint8_t t = out[i];
            out[i] = out[7 - i];
            out[7 - i] = t;
        }
    }
    return out;
}

int surface_block( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "block" ) continue;
        std::vector<uint8_t> data;
        if ( !slurp( f[3].c_str(), data ) ) { fprintf( stderr, "driver: cannot read %s\n", f[3].c_str() ); return 1; }
        const char * verdict = open_block( f[1], data, -1 ) ? "open\n" : "refuse\n";
        if ( !spill( out, f[1], verdict, strlen( verdict ) ) ) return 1;
    }
    return 0;
}

// the cross-endian refusal over the block form: the same images with their
// magic reversed, which every leg must refuse
int surface_block_foreign( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "block" ) continue;
        std::vector<uint8_t> data;
        if ( !slurp( f[3].c_str(), data ) ) { fprintf( stderr, "driver: cannot read %s\n", f[3].c_str() ); return 1; }
        const std::vector<uint8_t> swapped = foreign( data );
        const char * verdict = open_block( f[1], swapped, -1 ) ? "open\n" : "refuse\n";
        if ( !spill( out, f[1], verdict, strlen( verdict ) ) ) return 1;
    }
    return 0;
}

int surface_forgery( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "forgery" ) continue;
        if ( f[2] != "block" ) continue; // the cook's battery is its own binary's
        std::vector<uint8_t> data;
        if ( !slurp( f[4].c_str(), data ) ) { fprintf( stderr, "driver: cannot read %s\n", f[4].c_str() ); return 1; }
        // the POINTER column is the caller's buffer: 0 an aligned base, 1..63
        // that many bytes past one. The block battery carries one such row, so
        // the base-alignment clause is held here as it is in the cook battery.
        const int lead = f[6] == "null" ? 0 : (int) strtol( f[6].c_str(), NULL, 0 );
        const char * verdict = open_block( f[3], data, (int64_t) strtoll( f[5].c_str(), NULL, 0 ), NULL, lead ) ? "open\n" : "refuse\n";
        if ( !spill( out, f[1], verdict, strlen( verdict ) ) ) return 1;
    }
    return 0;
}

// block-reason: the REASON BlockOpen names beside its false, one word per
// forgery a `refusal` line asks for, spelled as the enum's own value name so
// the manifest pins the first failing clause (docs/SPEC-TABLES.md §7, §19.2)
static const char * reason_name( blockdemo::TableRefuseReason reason )
{
    switch ( reason )
    {
        case blockdemo::ok: return "ok";
        case blockdemo::not_a_cook: return "not_a_cook";
        case blockdemo::foreign_order: return "foreign_order";
        case blockdemo::wrong_build_version: return "wrong_build_version";
        case blockdemo::reserved_not_zero: return "reserved_not_zero";
        case blockdemo::bad_alignment: return "bad_alignment";
        case blockdemo::truncated: return "truncated";
        case blockdemo::unaligned_base: return "unaligned_base";
        case blockdemo::bad_layout: return "bad_layout";
        case blockdemo::unknown_form: return "unknown_form";
        case blockdemo::count_over_length: return "count_over_length";
        case blockdemo::count_over_extent_cap: return "count_over_extent_cap";
        case blockdemo::blob_over_size_cap: return "blob_over_size_cap";
        case blockdemo::data_cycle: return "data_cycle";
        case blockdemo::invalid_value: return "invalid_value";
        case blockdemo::allocation_failed: return "allocation_failed";
        case blockdemo::wire_damaged: return "wire_damaged";
        case blockdemo::malformed: return "malformed";
        case blockdemo::out_of_range: return "out_of_range";
        case blockdemo::kind_mismatch: return "kind_mismatch";
        case blockdemo::base64_damaged: return "base64_damaged";
        case blockdemo::base64_not_canonical: return "base64_not_canonical";
        case blockdemo::integrity_mismatch: return "integrity_mismatch";
        case blockdemo::non_finite_float: return "non_finite_float";
        case blockdemo::blob_shared: return "blob_shared";
        case blockdemo::previous_form: return "previous_form";
        case blockdemo::newer_form: return "newer_form";
        case blockdemo::message_form_as_file: return "message_form_as_file";
        case blockdemo::layout_unknown: return "layout_unknown";
        case blockdemo::layout_unsupported: return "layout_unsupported";
        case blockdemo::layout_newer: return "layout_newer";
        case blockdemo::layout_malformed: return "layout_malformed";
        case blockdemo::layout_count_mismatch: return "layout_count_mismatch";
        case blockdemo::layout_kind_unknown: return "layout_kind_unknown";
        case blockdemo::layout_kind_invalid: return "layout_kind_invalid";
        case blockdemo::layout_size_mismatch: return "layout_size_mismatch";
        case blockdemo::layout_tree_unclosed: return "layout_tree_unclosed";
        case blockdemo::layout_too_deep: return "layout_too_deep";
        case blockdemo::layout_record_too_large: return "layout_record_too_large";
        case blockdemo::plan_too_large: return "plan_too_large";
        case blockdemo::no_layout: return "no_layout";
        case blockdemo::batch_too_large: return "batch_too_large";
        case blockdemo::text_ill_formed: return "text_ill_formed";
        case blockdemo::count_over_bound: return "count_over_bound";
        case blockdemo::length_over_bound: return "length_over_bound";
        case blockdemo::missing_required: return "missing_required";
    }
    return "?";
}

int surface_block_reason( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & r = manifest_lines[i].field;
        if ( r[0] != "refusal" || r.size() < 3 || r[2] != "block" ) continue;
        const std::vector<std::string> * forgery = NULL;
        for ( size_t j = 0; j < manifest_lines.size(); j++ )
        {
            const std::vector<std::string> & f = manifest_lines[j].field;
            if ( f[0] == "forgery" && f[1] == r[1] ) forgery = &f;
        }
        if ( forgery == NULL ) { fprintf( stderr, "driver: refusal %s names no forgery\n", r[1].c_str() ); return 1; }
        const std::vector<std::string> & f = *forgery;
        std::vector<uint8_t> data;
        if ( !slurp( f[4].c_str(), data ) ) { fprintf( stderr, "driver: cannot read %s\n", f[4].c_str() ); return 1; }
        blockdemo::TableRefuseReason reason = blockdemo::ok;
        const int lead = f[6] == "null" ? 0 : (int) strtol( f[6].c_str(), NULL, 0 );
        const bool opened = open_block( f[3], data, (int64_t) strtoll( f[5].c_str(), NULL, 0 ), &reason, lead );
        std::string answer = std::string( opened ? "ok" : reason_name( reason ) ) + "\n";
        if ( !spill( out, r[1], answer.data(), answer.size() ) ) return 1;
    }
    return 0;
}

// block-dump and emit-block-forgeries are the walk's own (block_walk.h), which
// the block pin writer compiles as they stand
int surface_block_dump_answer( const std::string & out )
{
    return surface_block_dump( out );
}

int emit_block_forgeries_answer()
{
    return emit_block_forgeries();
}
