// R6 — a table past §3.4's 65536 ceiling is not a fixed-form root (cpp leg).
//
// LAW (docs/SPEC-TABLES.md §3.4 "65536 BYTES OF RECORD BODY"; governing rule
// docs/FIXED-FORM-ALGORITHM.md §5.9 #48): a fixed table whose record body is
// past 65536 bytes has NO FORM EMITTED and is NAMED — it keeps form 1, which
// it never lost. Such a table is NOT a fixed-form root, so COMPILE consults
// no lineage for it and parses no entry of it, even when the lock carries one
// (tables/examples/schema.lock carries one for WideBlob, 280012-byte body).
//
// WHAT THIS ASSERTS, and where the production path runs:
//   1. The ceiling constant every reader enforces
//      (tabledemo::kTableFixedRecordMaxBytes, emitted from
//      internal/codegen/cpptable/fixedruntime.go) is 65536.
//   2. The production layout gate — TableFixedParseLayout, which every
//      <Root>FixedLoad (e.g. WeaponConfigFixedLoad) calls before compiling a
//      stranger's plan — refuses a root past the ceiling under the name
//      layout_record_too_large, with nothing parsed (out.bytes == NULL).
//   3. The boundary: a root of EXACTLY 65536 bytes is NOT refused under that
//      name (it falls through to the tree walk instead). Past, not at.
//   4. The in-tree table past the ceiling (WideBlob) keeps form 1: its
//      generated form-1 Save/Load round-trips. The class compiles; only the
//      fixed form is withheld.
//   5. No fixed form is emitted for WideBlob: its generated header carries no
//      WideBlobFixed* symbol at all — no body size, no hash, no layout, no
//      plan, and no lineage/known-layout entry — even though the lock carries
//      one. Positive control beside it: WeaponConfig (a small fixed table)
//      DOES carry WeaponConfigFixedBodyBytes, so the scan is not vacuous.
//
// VECTORS: the tree has no corpus bytes for this cell (a past-ceiling table
// has no fixed-form file to pin), so the layout vectors are built from the law
// here. Derivation: TableFixedLayoutBytes (ir/fixedform.go) writes a u32
// entry count then 17-byte entries of id u64LE + kind u8 + size u32LE +
// children u32LE. Kind 13 is a table. The root-size gate is step 2 of
// TableFixedParseLayout, BEFORE the tree walk, so a one-entry layout with
// children == 0 still exercises exactly the gate under test.
//
// BUILD/RUN (from ./repo):
//   c++ -std=c++17 -Wall -Wextra -Werror -Wshadow -ffp-contract=off -pthread \
//     -Ibuild/tables-generated/examples test/conformance/cpp/rows/R6.cpp \
//     build/tables-generated/examples/WideTable.cpp -o build/rows-cpp-R6 && \
//     ./build/rows-cpp-R6
// (include dir and flags are the Makefile's build/conformance-cpp rule,
// narrowed to the Wide unit this cell is about; WideTable.h needs no
// serialize.h, so the missing ../serialize checkout does not apply here.)
// Exit 0 green / exit 1 red, one printed line per assertion. Run from ./repo:
// assertions 5 scan build/tables-generated/ by relative path.

#include <cstdint>
#include <cstdio>
#include <cstring>
#include <string>

#include "WideTable.h"

namespace {

int failures = 0;
int checks = 0;

void check( bool ok, const char * name, const char * detail )
{
    ++checks;
    if ( ok ) {
        std::printf( "PASS %s: %s\n", name, detail );
        return;
    }
    ++failures;
    std::printf( "FAIL %s: %s\n", name, detail );
}

void put32( uint8_t * at, uint32_t value )
{
    at[0] = (uint8_t) value;
    at[1] = (uint8_t) ( value >> 8 );
    at[2] = (uint8_t) ( value >> 16 );
    at[3] = (uint8_t) ( value >> 24 );
}

// One-entry layout: count u32LE, then id u64LE + kind u8 + size u32LE +
// children u32LE. root_size is the root entry's stated record-body size.
void build_root_layout( uint8_t * out, uint32_t root_size )
{
    put32( out + 0, 1u );
    for ( int i = 0; i < 8; ++i ) { out[4 + i] = (uint8_t) ( 0x10 + i ); }
    out[12] = 13; // kind 13: a table, the only legal root kind
    put32( out + 13, root_size );
    put32( out + 17, 0u ); // children: none; the size gate fires first
}

bool file_has( const char * path, const char * needle, std::string * error )
{
    std::FILE * file = std::fopen( path, "rb" );
    if ( file == NULL ) {
        *error = "cannot open ";
        *error += path;
        return false;
    }
    std::string text;
    char chunk[4096];
    std::size_t got = 0;
    while ( ( got = std::fread( chunk, 1, sizeof( chunk ), file ) ) > 0 ) {
        text.append( chunk, got );
    }
    std::fclose( file );
    return text.find( needle ) != std::string::npos;
}

} // namespace

int main()
{
    // 1. THE CEILING IS 65536 — the production constant, not a copy of it.
    check( tabledemo::kTableFixedRecordMaxBytes == 65536u,
        "ceiling-is-65536", "kTableFixedRecordMaxBytes == 65536" );

    // 2. PAST THE CEILING: no fixed-form root, refused BY NAME, nothing parsed.
    // WideBlob's own body is 280012 bytes (tables/examples/schema.lock); 70000
    // stands in for any size past the ceiling in the smallest vector the law
    // allows: one root entry, no children.
    {
        uint8_t layout[4 + 17];
        build_root_layout( layout, 70000u );
        tabledemo::TableFixedLayoutView view;
        tabledemo::TableMessageReason why = tabledemo::newer_form;
        const bool parsed = tabledemo::TableFixedParseLayout(
            layout, (int64_t) sizeof( layout ), view, why );
        check( !parsed, "past-ceiling-refused", "root size 70000 does not parse" );
        check( why == tabledemo::layout_record_too_large,
            "past-ceiling-named", "reason is layout_record_too_large" );
        check( view.bytes == NULL && view.count == 0,
            "past-ceiling-parses-nothing", "out view is empty on refusal" );
    }

    // 3. THE BOUNDARY: exactly 65536 is NOT past it, so it is NOT refused
    // under that name — it falls through to the tree walk, which then faults
    // the childless root's size against its (empty) children instead.
    {
        uint8_t layout[4 + 17];
        build_root_layout( layout, 65536u );
        tabledemo::TableFixedLayoutView view;
        tabledemo::TableMessageReason why = tabledemo::newer_form;
        const bool parsed = tabledemo::TableFixedParseLayout(
            layout, (int64_t) sizeof( layout ), view, why );
        check( !parsed, "at-ceiling-still-invalid", "childless root 65536 does not parse" );
        check( why != tabledemo::layout_record_too_large,
            "at-ceiling-not-too-large", "reason is not layout_record_too_large" );
        check( why == tabledemo::layout_size_mismatch,
            "at-ceiling-size-mismatch", "reason is layout_size_mismatch from the tree walk" );
    }

    // 4. WIDEBLOB KEEPS FORM 1: the class compiles, form-1 Save/Load round-trips.
    {
        static tabledemo::WideBlob blob;
        static tabledemo::WideBlob out;
        static uint8_t buffer[1 << 20];
        tabledemo::WideBlobReset( blob );
        blob.label_length = 2;
        blob.label[0] = 'o';
        blob.label[1] = 'k';
        blob.label[2] = 0;
        blob.payload_length = 3;
        blob.payload[0] = 1;
        blob.payload[1] = 2;
        blob.payload[2] = 3;
        blob.samples_count = 2;
        blob.samples[0] = 11;
        blob.samples[1] = 22;
        const int64_t need = tabledemo::WideBlobMeasure( blob );
        bool round_trip = false;
        if ( need > 0 && need < (int64_t) sizeof( buffer ) ) {
            tabledemo::TableReport save_report;
            (void) save_report;
            if ( tabledemo::WideBlobSave( blob, buffer, need ) == need ) {
                tabledemo::TableReport report;
                if ( tabledemo::WideBlobLoad( out, buffer, need, &report ) &&
                     !report.malformed && !report.refused ) {
                    round_trip = out.label_length == 2 &&
                                 std::strcmp( out.label, "ok" ) == 0 &&
                                 out.payload_length == 3 &&
                                 out.payload[2] == 3 &&
                                 out.samples_count == 2 &&
                                 out.samples[0] == 11 && out.samples[1] == 22;
                }
            }
        }
        check( round_trip, "wideblob-keeps-form-1", "WideBlob form-1 Save/Load round-trips" );
    }

    // 5. NO FORM EMITTED FOR WIDEBLOB: no WideBlobFixed* symbol anywhere in
    // its generated header — no body size, no hash, no layout, no plan, and
    // no lineage/known-layout entry — though tables/examples/schema.lock
    // carries a lineage entry for it. Control: WeaponConfig, a small fixed
    // table, DOES emit WeaponConfigFixedBodyBytes, so the scan bites.
    {
        std::string error;
        const bool wide_opened =
            file_has( "build/tables-generated/examples/WideTable.h", "", &error );
        check( wide_opened, "wide-header-readable", error.empty() ? "opened" : error.c_str() );
        if ( wide_opened ) {
            std::string unused;
            check( !file_has( "build/tables-generated/examples/WideTable.h",
                       "WideBlobFixed", &unused ),
                "no-form-emitted", "WideTable.h names no WideBlobFixed* symbol" );
            check( !file_has( "build/tables-generated/examples/WideTable.h",
                       "WideBlobFixedLineage", &unused ),
                "no-lineage-parsed", "WideTable.h names no WideBlobFixedLineage entry" );
        }
        std::string control_error;
        check( file_has( "build/tables-generated/examples/TablesTable.h",
                   "WeaponConfigFixedBodyBytes", &control_error ),
            "control-small-table-has-form",
            control_error.empty() ? "TablesTable.h names WeaponConfigFixedBodyBytes" : control_error.c_str() );
    }

    std::printf( "R6: %d checks, %d failures\n", checks, failures );
    return failures == 0 ? 0 : 1;
}
