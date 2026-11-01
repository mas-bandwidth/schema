// THE C++ PORT RUNS THE SHARED HOSTILE CORPUS (docs/HOSTILE-CORPUS.md): one
// case per process.
//
// The case binary, its answer shape and its binding row are the C port's
// (test/c-hostile/main.c and hostile.h); a C++ unit's translation unit
// includes its own generated headers, defines HC_NS as the unit's namespace,
// includes this file, and lists its roots with the macros below, exporting
// the same hc_bindings_<unit> name the C units export.
//
// Every read is the TOLERANT one where the port has one, as the engine's
// verdict is: a named refusal is its reason, a read that did not land or
// reported damage is `damaged`, and anything else is accepted. The message
// form has only the plain LoadMessages, which is what it calls.

#ifndef SCHEMA_CPP_HOSTILE_HPP
#define SCHEMA_CPP_HOSTILE_HPP

#include <cstdint>
#include <cstdio>
#include <cstring>
#include <memory>
#include <new>
#include <vector>

#include "hostile.h"

// THE REFUSAL'S NAME, off the unit's own enumerators: every unit spells the
// one TableRefuseReason vocabulary in its own namespace.
#define HC_CPP_REASON( name ) \
    if ( reason == HC_NS::name ) return #name;

#define HC_CPP_REASONS() \
    static const char * hc_reason( HC_NS::TableRefuseReason reason ) \
    { \
        HC_CPP_REASON( newer_form ) \
        HC_CPP_REASON( message_form_as_file ) \
        HC_CPP_REASON( no_vocabulary ) \
        HC_CPP_REASON( second_announcement ) \
        HC_CPP_REASON( vocabulary_too_large ) \
        HC_CPP_REASON( batch_too_large ) \
        HC_CPP_REASON( non_finite_float ) \
        HC_CPP_REASON( count_over_length ) \
        HC_CPP_REASON( count_over_extent_cap ) \
        HC_CPP_REASON( blob_over_size_cap ) \
        HC_CPP_REASON( data_cycle ) \
        HC_CPP_REASON( invalid_value ) \
        HC_CPP_REASON( allocation_failed ) \
        HC_CPP_REASON( wire_damaged ) \
        HC_CPP_REASON( malformed ) \
        HC_CPP_REASON( clamped ) \
        HC_CPP_REASON( kind_mismatch ) \
        HC_CPP_REASON( integrity_mismatch ) \
        HC_CPP_REASON( blob_shared ) \
        HC_CPP_REASON( previous_form ) \
        HC_CPP_REASON( unknown_form ) \
        HC_CPP_REASON( text_ill_formed ) \
        static char other[32]; \
        std::snprintf( other, sizeof( other ), "reason_%d", (int) reason ); \
        return other; \
    } \
    static bool hc_refused( const HC_NS::TableReport & report, bool landed ) \
    { \
        hc_trace( report.refused, report.malformed, (int) (HC_NS::TableRefuseReason) report.reason, landed ); \
        if ( report.refused ) { hc_refuse( hc_reason( report.reason ) ); return true; } \
        if ( !landed || report.malformed ) { hc_refuse( "damaged" ); return true; } \
        return false; \
    }

// the text writer's answer, digested by the driver
#define HC_CPP_TO_JSON( TYPE, value ) \
    do { \
        const int64_t hc_need = HC_NS::TYPE##ToJsonMeasure( value ); \
        if ( hc_need < 0 ) { hc_error( "ToJsonMeasure refused the decoded value" ); break; } \
        std::vector<char> hc_text( (size_t) hc_need + 1, 0 ); \
        if ( HC_NS::TYPE##ToJson( value, hc_text.data(), hc_need + 1 ) != hc_need ) { hc_error( "ToJson disagreed with its measure" ); break; } \
        hc_accept_text( hc_text.data(), hc_need ); \
    } while ( 0 )

// A FIXED root, held by value. C++ has no retaining load for one (its
// LoadRetain is a deleted overload), so its wire_retain carrier answers skip.
#define HC_FIXED( TYPE ) \
    static int64_t hc_save_##TYPE( const void * v, uint8_t * b, int64_t n ) { return HC_NS::TYPE##Save( *(const HC_NS::TYPE *) v, b, n ); } \
    static void hc_accepted_##TYPE( const HC_NS::TYPE & value ) \
    { \
        hc_exercise_save( HC_NS::TYPE##Measure( value ), hc_save_##TYPE, &value ); \
        HC_CPP_TO_JSON( TYPE, value ); \
    } \
    static void hc_wire_##TYPE( const uint8_t * data, int64_t size ) \
    { \
        std::unique_ptr<HC_NS::TYPE> value( new HC_NS::TYPE() ); \
        HC_NS::TableReport report; \
        const bool ok = HC_NS::TYPE##LoadTolerant( *value, data, size, &report ); \
        if ( !hc_refused( report, ok ) ) { hc_accepted_##TYPE( *value ); } \
    } \
    static void hc_text_##TYPE( const uint8_t * data, int64_t size ) \
    { \
        std::unique_ptr<HC_NS::TYPE> value( new HC_NS::TYPE() ); \
        HC_NS::TableReport report; \
        const bool ok = HC_NS::TYPE##FromJsonTolerant( *value, (const char *) data, size, &report ); \
        if ( !hc_refused( report, ok ) ) { hc_accepted_##TYPE( *value ); } \
    }

#define HC_FIXED_ROW( SCHEMA, TABLE, TYPE ) \
    hc_binding_t row; \
    std::memset( &row, 0, sizeof( row ) ); \
    row.schema = SCHEMA; \
    row.table = TABLE; \
    row.wire = hc_wire_##TYPE; \
    row.text = hc_text_##TYPE; \
    row.wire_retain_skip = "C++ has no retaining load for a fixed-storage root (its LoadRetain is a deleted overload)"

// A VARIABLE root: read into a caller-owned region sized by LoadMeasure; its
// text is read into a builder whose Lock packs the same region form.
#define HC_VAR( TYPE ) \
    static int64_t hc_save_##TYPE( const void * v, uint8_t * b, int64_t n ) { return HC_NS::TYPE##Save( (const HC_NS::TYPE *) v, b, n ); } \
    static void hc_accepted_##TYPE( const HC_NS::TYPE * root ) \
    { \
        hc_exercise_save( HC_NS::TYPE##Measure( root ), hc_save_##TYPE, root ); \
        HC_CPP_TO_JSON( TYPE, root ); \
    } \
    static void hc_wire_##TYPE( const uint8_t * data, int64_t size ) \
    { \
        const int64_t extent = HC_NS::TYPE##LoadMeasure( data, size ); \
        std::vector<uint8_t> region( (size_t) ( extent > 0 ? extent : 1 ), 0 ); \
        HC_NS::TableReport report; \
        const HC_NS::TYPE * root = HC_NS::TYPE##LoadTolerant( region.data(), extent, data, size, &report ); \
        if ( !hc_refused( report, root != NULL ) ) { hc_accepted_##TYPE( root ); } \
    } \
    static void hc_wire_retain_##TYPE( const uint8_t * data, int64_t size ) \
    { \
        const int64_t roomy = 1 << 20; \
        const int64_t extent = HC_NS::TYPE##LoadMeasure( data, size ); \
        std::vector<uint8_t> region( (size_t) ( extent > 0 ? extent : 1 ), 0 ); \
        std::vector<uint8_t> bytes( (size_t) roomy, 0 ); \
        std::vector<HC_NS::TableRetain::Id> ids( (size_t) ( roomy / 8 ) ); \
        HC_NS::TableRetain retain; \
        retain.bytes = bytes.data(); \
        retain.capacity = roomy; \
        retain.ids = ids.data(); \
        retain.id_capacity = (int32_t) ids.size(); \
        HC_NS::TableReport report; \
        const HC_NS::TYPE * root = HC_NS::TYPE##LoadRetain( region.data(), extent, data, size, &retain, &report ); \
        if ( hc_refused( report, root != NULL ) ) { return; } \
        const int64_t need = HC_NS::TYPE##MeasureRetain( root, &retain ); \
        if ( need >= 0 ) \
        { \
            std::vector<uint8_t> saved( (size_t) ( need ? need : 1 ), 0 ); \
            HC_NS::TableReport save; \
            (void) HC_NS::TYPE##SaveRetain( root, &retain, saved.data(), need, &save ); \
        } \
        hc_accepted_##TYPE( root ); \
    } \
    static void hc_text_##TYPE( const uint8_t * data, int64_t size ) \
    { \
        HC_NS::TYPE##Builder builder; \
        HC_NS::TableReport report; \
        const bool ok = HC_NS::TYPE##FromJsonTolerant( builder, (const char *) data, size, &report ); \
        if ( hc_refused( report, ok ) ) { return; } \
        if ( !builder.Lock() ) { hc_error( "Lock refused the decoded value" ); return; } \
        hc_accepted_##TYPE( builder.AsConst() ); \
    }

#define HC_VAR_ROW( SCHEMA, TABLE, TYPE ) \
    hc_binding_t row; \
    std::memset( &row, 0, sizeof( row ) ); \
    row.schema = SCHEMA; \
    row.table = TABLE; \
    row.wire = hc_wire_##TYPE; \
    row.wire_retain = hc_wire_retain_##TYPE; \
    row.text = hc_text_##TYPE

// The MESSAGE FORM of a fixed root (docs/SPEC-TABLES.md §3.3): this unit's own
// announcement is read first (unless the carrier withholds it), then one body
// is decoded into a batch of capacity one.
#define HC_MESSAGE( TYPE ) \
    static void hc_message_##TYPE( const uint8_t * data, int64_t size, int announced ) \
    { \
        std::vector<HC_NS::TableMessageEntry> entries( 4096 ); \
        HC_NS::TableVocabulary vocabulary( entries.data(), (int64_t) entries.size() ); \
        HC_NS::TableReport report; \
        if ( announced && !HC_NS::AnnounceRead( vocabulary, HC_NS::kTableAnnounce, HC_NS::kTableAnnounceBytes, &report ) ) \
        { \
            hc_error( "the unit could not read its own announcement" ); \
            return; \
        } \
        report = HC_NS::TableReport(); \
        std::unique_ptr<HC_NS::TYPE> value( new HC_NS::TYPE() ); \
        HC_NS::TYPE##Reset( *value ); \
        int64_t count = 1; \
        const bool ok = HC_NS::TYPE##LoadMessages( value.get(), &count, vocabulary, data, size, &report ); \
        if ( !hc_refused( report, ok && count == 1 ) ) { hc_accepted_##TYPE( *value ); } \
    }

// The unit's announcement read (docs/SPEC-TABLES.md §3.3), under the bound the
// carrier names.
#define HC_ANNOUNCE() \
    static void hc_announce( const uint8_t * data, int64_t size, const char * carrier ) \
    { \
        std::vector<HC_NS::TableMessageEntry> entries( 4096 ); \
        HC_NS::TableVocabulary vocabulary( entries.data(), (int64_t) entries.size() ); \
        if ( std::strcmp( carrier, "announce_bytes" ) == 0 ) { vocabulary.max_bytes = 8; } \
        if ( std::strcmp( carrier, "announce_entries" ) == 0 ) { vocabulary.max_entries = 1; } \
        if ( std::strcmp( carrier, "announce_second" ) == 0 ) \
        { \
            HC_NS::TableReport first; \
            (void) HC_NS::AnnounceRead( vocabulary, data, size, &first ); \
        } \
        HC_NS::TableReport report; \
        const bool ok = HC_NS::AnnounceRead( vocabulary, data, size, &report ); \
        if ( !hc_refused( report, ok ) ) { hc_accept_none(); } \
    }

// A PACKET type (the type wire, serialize): read from bit 0 of the input,
// then re-written by the port's own writer, whose bytes the driver digests.
#define HC_PACKET( TYPE ) \
    static void hc_packet_##TYPE( const uint8_t * data, int64_t size ) \
    { \
        const int64_t room = 1 << 20; \
        std::vector<uint32_t> storage( (size_t) ( ( size + 8 + 3 ) / 4 ), 0 ); /* the stream reads up to 8 bytes past the data */ \
        std::vector<uint32_t> encoded( (size_t) ( room / 4 ), 0 ); \
        if ( size > 0 ) { std::memcpy( storage.data(), data, (size_t) size ); } \
        std::unique_ptr<HC_NS::TYPE> value( new HC_NS::TYPE() ); \
        serialize::ReadStream r( (const uint8_t *) storage.data(), size ); \
        if ( !HC_NS::Read##TYPE( r, *value ) ) { hc_refuse( "packet_refused" ); return; } \
        serialize::WriteStream w( (uint8_t *) encoded.data(), room ); \
        if ( !HC_NS::Write##TYPE( w, *value ) ) { hc_error( "the packet writer refused the decoded value" ); return; } \
        w.Flush(); \
        hc_accept_bytes( (const uint8_t *) encoded.data(), w.GetBytesProcessed() ); \
    }

#endif // SCHEMA_CPP_HOSTILE_HPP
