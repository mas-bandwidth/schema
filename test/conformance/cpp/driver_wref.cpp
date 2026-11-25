// THE WRITE CONTRACT'S BATTERIES of the C++ conformance driver
// (docs/SPEC.md §5.1; test/conformance/README.md, `write-refusal` and the
// surfaces beside it). The rows set fields of the generated values by name, so
// this unit reads the header of every unit a row names.

#include <cerrno>
#include <fcntl.h>
#include <signal.h>
#include <sys/wait.h>
#include <unistd.h>
#if defined( __linux__ )
#include <sys/prctl.h>
#endif

#include "driver.h"

#include "TablesTable.h"
#include "ScalarsTable.h"
#include "CookTortureTable.h"
#include "ShapesTable.h"
#include "SurfacesTable.h"
#include "BackendBlock.h"
#include "VarEvo3Table.h"
#include "GraphTable.h"
#include "SaveTable.h"
#include "FleetTable.h"

static std::vector<uint8_t> scratch;

// THE WRITE CONTRACT's batteries (docs/SPEC.md §5.1; FORMAT.md, `write-refusal`
// and the three surfaces beside it): load the wire, set ONE field out of (or at
// the edge of) its declaration, and write it through the writer the surface
// names — the variable-form Save, the fixed form's FixedSave or the message
// form's SaveMessages as a batch of one, or the text writer. C++ refuses two
// ways, and both answer `refused`: the every-build refusal is the writer's -1
// or false, and the debug-only refusal is schema_assert, which this driver
// keeps live (no NDEBUG) and does not route. An assert aborts, so EVERY CASE
// RUNS IN A CHILD: the child loads, sets, writes and exits with a code for
// wrote or refused-by-return; the parent reads the status, and death by
// SIGABRT is the assert's refusal. The child's stderr goes to /dev/null so the
// assert's text stays out of the log.

enum
{
    kWroteExit = 10,
    kRefusedExit = 11,
    kNoSetterExit = 3,
    // the value is past the storage's own width (§5.1 row 11): C++'s typed
    // storage cannot hold it, so there is nothing for the writer to refuse
    kUnrepresentableExit = 12,
};

// The hex of a `bytes` or `units` value: two digits a byte, or four a unit.
static bool write_refusal_hex( const std::string & text, int digits, std::vector<uint32_t> & out )
{
    if ( text.size() % (size_t) digits != 0 ) return false;
    for ( size_t i = 0; i < text.size(); i += (size_t) digits )
    {
        char * end = NULL;
        const std::string piece = text.substr( i, (size_t) digits );
        const unsigned long v = strtoul( piece.c_str(), &end, 16 );
        if ( end == NULL || *end != '\0' ) return false;
        out.push_back( (uint32_t) v );
    }
    return true;
}

// THE ONE CHECKED CONVERSION (defined below): every integer a row names goes
// into typed storage through wref_store_int, keyed on the storage's own type,
// and a value that type cannot hold ends the child as UNREPRESENTABLE. A cast
// would keep the low bits: 256 in a one-byte ordinal lands as 0, a lawful
// member, and the row would answer `wrote` for a value never written.
template <typename T> static bool wref_store_int( T & field, const std::string & text );
static int32_t wref_count( const std::string & text );

// A narrow text's storage set by `length` (filled with `a`) or `bytes` (exact).
// Past storage only the length moves, and the writer must refuse before it
// reads past the array.
static bool write_refusal_text( char * storage, int32_t capacity, int32_t & length,
                                const std::string & kind, const std::string & text )
{
    if ( kind == "length" )
    {
        const int32_t n = wref_count( text );
        for ( int32_t i = 0; i < n && i < capacity; i++ ) storage[i] = 'a';
        length = n;
        return true;
    }
    if ( kind == "bytes" )
    {
        std::vector<uint32_t> bytes;
        if ( !write_refusal_hex( text, 2, bytes ) || (int64_t) bytes.size() > capacity ) return false;
        for ( size_t i = 0; i < bytes.size(); i++ ) storage[i] = (char) (uint8_t) bytes[i];
        length = (int32_t) bytes.size();
        return true;
    }
    return false;
}

// Sets the one field the row names. The set is EXACTLY the rows' (root, path,
// kind) triples: an unknown one is an error that names itself, so a new row
// cannot pass by being skipped.
static bool parse_wref_f32( const std::string & text, float & out )
{
    const float inf = std::numeric_limits<float>::infinity();
    if ( text == "nan" ) { out = std::numeric_limits<float>::quiet_NaN(); return true; }
    if ( text == "inf" ) { out = inf; return true; }
    if ( text == "-inf" ) { out = -inf; return true; }
    char * end = NULL;
    out = strtof( text.c_str(), &end );
    return end != NULL && *end == 0;
}

static bool parse_wref_f64( const std::string & text, double & out )
{
    const double inf = std::numeric_limits<double>::infinity();
    if ( text == "nan" ) { out = std::numeric_limits<double>::quiet_NaN(); return true; }
    if ( text == "inf" ) { out = inf; return true; }
    if ( text == "-inf" ) { out = -inf; return true; }
    char * end = NULL;
    out = strtod( text.c_str(), &end );
    return end != NULL && *end == 0;
}

static bool write_refusal_bytes_field( uint8_t * storage, int32_t capacity, int32_t & length,
                                       const std::string & kind, const std::string & text )
{
    if ( kind == "length" )
    {
        const int32_t n = wref_count( text );
        for ( int32_t i = 0; i < n && i < capacity; i++ ) storage[i] = 0x61;
        length = n;
        return true;
    }
    if ( kind == "bytes" )
    {
        std::vector<uint32_t> bytes;
        if ( !write_refusal_hex( text, 2, bytes ) || (int64_t) bytes.size() > capacity ) return false;
        for ( size_t i = 0; i < bytes.size(); i++ ) storage[i] = (uint8_t) bytes[i];
        length = (int32_t) bytes.size();
        return true;
    }
    return false;
}

// A decimal integer as __int128, saturating far past any storage, so a value
// past 64 bits still compares as past it.
static __int128 wref_int128( const std::string & text )
{
    size_t i = 0;
    bool negative = false;
    if ( i < text.size() && ( text[i] == '-' || text[i] == '+' ) ) { negative = text[i] == '-'; i++; }
    __int128 v = 0;
    const __int128 cap = (__int128) 1 << 100;
    for ( ; i < text.size() && text[i] >= '0' && text[i] <= '9'; i++ )
    {
        if ( v < cap ) v = v * 10 + ( text[i] - '0' );
    }
    return negative ? -v : v;
}

// Stores an integer the row names in typed storage, or ends the child as
// UNREPRESENTABLE when the storage cannot hold it (§5.1 row 11 on a bare int).
template <typename T> static bool wref_store_int( T & field, const std::string & text )
{
    const __int128 v = wref_int128( text );
    if ( v < (__int128) std::numeric_limits<T>::min() || v > (__int128) std::numeric_limits<T>::max() ) _exit( kUnrepresentableExit );
    field = (T) v;
    return true;
}

// A count or a length: int32 storage in every generated value.
static int32_t wref_count( const std::string & text )
{
    int32_t v = 0;
    wref_store_int( v, text );
    return v;
}

// An enum's ordinal or a union's tag, held to the enum's own underlying type.
template <typename E> static bool wref_store_enum( E & field, const std::string & text )
{
    typename std::underlying_type<E>::type raw = 0;
    wref_store_int( raw, text );
    field = (E) raw;
    return true;
}

// The `real` kind (FORMAT.md): a decimal parsed as a double, then rounded to the
// float storage the way any caller's assignment rounds it, so a row can name
// the float32 one step past a bound exactly.
static bool parse_wref_real_f32( const std::string & text, float & out )
{
    char * end = NULL;
    const double d = strtod( text.c_str(), &end );
    if ( end == NULL || *end != 0 ) return false;
    // a finite decimal past the float's range would be stored as an infinity
    const double most = (double) std::numeric_limits<float>::max();
    if ( d - d == 0 && ( d > most || d < -most ) ) _exit( kUnrepresentableExit );
    out = (float) d;
    return true;
}

static bool parse_wref_float_kind( const std::string & kind, const std::string & text, float & out )
{
    if ( kind == "real" ) return parse_wref_real_f32( text, out );
    if ( kind == "float" ) return parse_wref_f32( text, out );
    return false;
}

// The SHAPES unit's rows (test/tables/shapes): the fields both roots share.
template <typename T, typename E> static bool write_refusal_shapes_common( T & v, E * tones, int32_t tones_count,
                                                                           const std::string & path, const std::string & kind,
                                                                           const std::string & text )
{
    if ( path == "tenth" ) return parse_wref_float_kind( kind, text, v.tenth );
    if ( path == "seventh" ) return parse_wref_float_kind( kind, text, v.seventh );
    if ( path == "ranged" ) return parse_wref_float_kind( kind, text, v.ranged );
    if ( path == "zero" ) return parse_wref_float_kind( kind, text, v.zero );
    if ( path == "nonzero" ) return parse_wref_float_kind( kind, text, v.nonzero );
    if ( path == "octets" && kind == "count" )
    {
        const int32_t storage = (int32_t) ( sizeof( v.octets ) / sizeof( v.octets[0] ) );
        for ( int32_t i = v.octets_count; i < wref_count( text ) && i < storage; i++ ) v.octets[i] = 9;
        v.octets_count = wref_count( text );
        return true;
    }
    if ( path == "recs" && kind == "count" )
    {
        const int32_t storage = (int32_t) ( sizeof( v.recs ) / sizeof( v.recs[0] ) );
        for ( int32_t i = v.recs_count; i < wref_count( text ) && i < storage; i++ ) v.recs[i] = v.recs[0];
        v.recs_count = wref_count( text );
        return true;
    }
    if ( path == "tones[0]" && kind == "ordinal" )
    {
        if ( tones_count < 1 || tones == NULL ) return false;
        return wref_store_enum( tones[0], text );
    }
    if ( path == "pick" && kind == "tag" )
    {
        wref_store_enum( v.pick.type, text );
        // a lawful tag takes its arm at the arm's declared default
        switch ( (int) v.pick.type )
        {
            case 1: v.pick.small = 0; break;
            case 2: v.pick.ratio = 0.0f; break;
            case 3: v.pick.on = false; break;
            default: break;
        }
        return true;
    }
    return false;
}

// The REVIEW-1121 rows on PLAIN VARIABLE roots (MANIFEST.txt, R1): the value is
// the region the load left, set in place.
static bool write_refusal_set_variable( const std::string & root, void * value, const std::string & path,
                                        const std::string & kind, const std::string & text )
{
    if ( root == "Scene" )
    {
        graphdemo::Scene & v = *(graphdemo::Scene *) value;
        if ( path == "version" && kind == "int" ) return wref_store_int( v.version, text );
        if ( path == "ground.depth" && kind == "int" ) return wref_store_int( v.ground.depth, text );
        if ( path == "name" ) return write_refusal_text( v.name, (int32_t) sizeof( v.name ) - 1, v.name_length, kind, text );
        if ( path == "layers" && kind == "count" )
        {
            const int32_t storage = (int32_t) ( sizeof( v.layers ) / sizeof( v.layers[0] ) );
            for ( int32_t i = v.layers_count; i < wref_count( text ) && i < storage; i++ ) v.layers[i] = v.ground;
            v.layers_count = wref_count( text );
            return true;
        }
    }
    if ( root == "Mixed" )
    {
        listdemo::Mixed & v = *(listdemo::Mixed *) value;
        if ( path == "bounds[1]" && kind == "int" )
        {
            if ( v.bounds.count < 2 || v.bounds.elements.value == 0 ) return false;
            int32_t * elems = (int32_t *) ( (uint8_t *) &v.bounds.elements + v.bounds.elements.value );
            return wref_store_int( elems[1], text );
        }
        if ( path == "grades[0]" && kind == "ordinal" )
        {
            if ( v.grades.count < 1 || v.grades.elements.value == 0 ) return false;
            listdemo::Grade * elems = (listdemo::Grade *) ( (uint8_t *) &v.grades.elements + v.grades.elements.value );
            return wref_store_enum( elems[0], text );
        }
        if ( path == "hits[0]" && kind == "tag" )
        {
            if ( v.hits.count < 1 || v.hits.elements.value == 0 ) return false;
            listdemo::Hit * elems = (listdemo::Hit *) ( (uint8_t *) &v.hits.elements + v.hits.elements.value );
            // a lawful tag takes its arm at its defaults
            wref_store_enum( elems[0].type, text );
            if ( (int) elems[0].type == 2 ) elems[0].damage = 0;
            return true;
        }
    }
    if ( root == "TortureCook" )
    {
        cooktorture::TortureCook & v = *(cooktorture::TortureCook *) value;
        if ( path == "b_bits13" && kind == "int" )
        {
            // bits(13) in uint32 storage: past 2^13 - 1 is the WRITER's to refuse
            // (row 2); below 0 the storage cannot hold
            return wref_store_int( v.b_bits13, text );
        }
        if ( path == "b_bits37" && kind == "int" ) return wref_store_int( v.b_bits37, text );
        if ( path == "b_i32" && kind == "int" ) return wref_store_int( v.b_i32, text );
        if ( path == "b_u8" && kind == "int" ) return wref_store_int( v.b_u8, text );
        if ( path == "b_u64" && kind == "int" ) return wref_store_int( v.b_u64, text );
        if ( path == "bounded_steps" && kind == "count" )
        {
            const int32_t storage = (int32_t) ( sizeof( v.bounded_steps ) / sizeof( v.bounded_steps[0] ) );
            for ( int32_t i = v.bounded_steps_count; i < wref_count( text ) && i < storage; i++ ) v.bounded_steps[i] = v.bounded_steps[0];
            v.bounded_steps_count = wref_count( text );
            return true;
        }
        if ( path == "bounded_steps[3].label" )
        {
            if ( v.bounded_steps_count < 4 || v.bounded_steps[3].type != cooktorture::StepActionType::Label ) return false;
            return write_refusal_text( v.bounded_steps[3].label.value, 12, v.bounded_steps[3].label.value_length, kind, text );
        }
        if ( path == "bounded_steps[0]" && kind == "tag" )
        {
            if ( v.bounded_steps_count < 1 ) return false;
            wref_store_enum( v.bounded_steps[0].type, text );
            if ( (int) v.bounded_steps[0].type == 4 ) { memset( v.bounded_steps[0].label.value, 0, sizeof( v.bounded_steps[0].label.value ) ); v.bounded_steps[0].label.value_length = 0; }
            return true;
        }
    }
    if ( root == "Bounds" )
    {
        shapes::Bounds & v = *(shapes::Bounds *) value;
        shapes::Tone * tones = v.tones.count < 1 || v.tones.elements.value == 0
                                   ? NULL
                                   : (shapes::Tone *) ( (uint8_t *) &v.tones.elements + v.tones.elements.value );
        return write_refusal_shapes_common( v, tones, v.tones.count, path, kind, text );
    }
    if ( root == "FixedBounds" )
    {
        shapes::FixedBounds & v = *(shapes::FixedBounds *) value;
        if ( path == "plain" ) return parse_wref_float_kind( kind, text, v.plain );
        if ( path == "wide" && kind == "real" ) { char * end = NULL; v.wide = strtod( text.c_str(), &end ); return end != NULL && *end == 0; }
        if ( path == "wide" && kind == "float" ) return parse_wref_f64( text, v.wide );
        return write_refusal_shapes_common( v, v.tones, v.tones_count, path, kind, text );
    }
    return false;
}

static bool write_refusal_set( const std::string & root, void * value, const std::string & path,
                               const std::string & kind, const std::string & text )
{
    if ( root == "CountedMin" && path == "few" && kind == "count" )
    {
        ((tabledemo::CountedMin *) value)->few_count = wref_count( text );
        return true;
    }
    if ( root == "RootConfig" )
    {
        tabledemo::RootConfig & v = *(tabledemo::RootConfig *) value;
        if ( path == "version_note" )
        {
            return write_refusal_text( v.version_note, (int32_t) sizeof( v.version_note ) - 1, v.version_note_length, kind, text );
        }
        if ( path == "profiles[0].precision" && kind == "float" )
        {
            return v.profiles_count >= 1 && parse_wref_f64( text, v.profiles[0].precision );
        }
        if ( path == "profiles[0].loadout.attachments[0].power" && kind == "float" )
        {
            return v.profiles_count >= 1 && v.profiles[0].loadout.attachments_count >= 1 &&
                   parse_wref_f32( text, v.profiles[0].loadout.attachments[0].power );
        }
        if ( path == "profiles[0].icon" )
        {
            return v.profiles_count >= 1 &&
                   write_refusal_bytes_field( v.profiles[0].icon, (int32_t) sizeof( v.profiles[0].icon ), v.profiles[0].icon_length, kind, text );
        }
        if ( v.weapons_count < 1 ) return false; // every weapons[0] path needs element 0
        tabledemo::WeaponConfig & w0 = v.weapons[0];
        if ( path == "weapons[0].penetration" && kind == "int" ) return wref_store_int( w0.penetration, text );
        if ( path == "weapons[0].channel" && kind == "int" ) return wref_store_int( w0.channel, text );
        if ( path == "weapons[0].damage" && kind == "float" )
        {
            return parse_wref_f32( text, w0.damage );
        }
        if ( path == "weapons[0].effect" && kind == "tag" )
        {
            wref_store_enum( w0.effect.type, text );
            // a lawful tag takes its arm at the arm's declared defaults
            switch ( (int) w0.effect.type )
            {
                case 1: w0.effect.SelectBuff(); break;
                case 2: w0.effect.SelectDebuff(); break;
                default: break;
            }
            return true;
        }
        if ( path == "weapons" && kind == "count" )
        {
            const int32_t storage = (int32_t) ( sizeof( v.weapons ) / sizeof( v.weapons[0] ) );
            for ( int32_t i = v.weapons_count; i < wref_count( text ) && i < storage; i++ ) v.weapons[i] = v.weapons[0];
            v.weapons_count = wref_count( text );
            return true;
        }
    }
    if ( root == "LoadoutConfig" )
    {
        tabledemo::LoadoutConfig & v = *(tabledemo::LoadoutConfig *) value;
        if ( path == "grade" && kind == "ordinal" ) return wref_store_enum( v.grade, text );
    }
    if ( root == "Moods" )
    {
        tabledemo::Moods & v = *(tabledemo::Moods *) value;
        if ( path == "row[0]" && kind == "ordinal" && v.row_count >= 1 ) { return wref_store_enum( v.row[0], text ); }
    }
    if ( root == "MoodField" )
    {
        tabledemo::MoodField & v = *(tabledemo::MoodField *) value;
        if ( path == "mood" && kind == "ordinal" ) { return wref_store_enum( v.mood, text ); }
    }
    if ( root == "SimState" )
    {
        scalardemo::SimState & v = *(scalardemo::SimState *) value;
        if ( path == "ticks" && kind == "int" ) return wref_store_int( v.ticks, text );
        if ( path == "angle" && kind == "int" ) return wref_store_int( v.angle, text );
        if ( path == "speed" && kind == "int" ) return wref_store_int( v.speed, text );
    }
    if ( root == "TortureFixedWref" )
    {
        cooktorture::TortureFixedWref & v = *(cooktorture::TortureFixedWref *) value;
        if ( path == "counted_f64[0]" && kind == "float" ) return v.counted_f64_count >= 1 && parse_wref_f64( text, v.counted_f64[0] );
        if ( path == "counted_f64" && kind == "count" )
        {
            const int32_t storage = (int32_t) ( sizeof( v.counted_f64 ) / sizeof( v.counted_f64[0] ) );
            for ( int32_t i = v.counted_f64_count; i < wref_count( text ) && i < storage; i++ ) v.counted_f64[i] = v.counted_f64[0];
            v.counted_f64_count = wref_count( text );
            return true;
        }
        if ( path == "fixed_f32[1]" && kind == "float" ) return parse_wref_f32( text, v.fixed_f32[1] );
        if ( path == "chan_f32[1]" && kind == "float" ) return parse_wref_f32( text, v.chan_f32.slots[1] );
        if ( path == "opt_f32" && kind == "float" ) { v.opt_f32_present = true; return parse_wref_f32( text, v.opt_f32 ); }
        if ( path == "u_val.u_f32" && kind == "float" ) return parse_wref_f32( text, v.u_val.u_f32 );
        if ( path == "u_val.u_str" ) return write_refusal_text( v.u_val.u_str.value, 16, v.u_val.u_str.value_length, kind, text );
    }
    if ( root == "TortureCook" )
    {
        cooktorture::TortureCook & v = *(cooktorture::TortureCook *) value;
        if ( path == "slice_f32[0]" && kind == "float" )
        {
            if ( v.slice_f32.count < 1 || v.slice_f32.elements.value == 0 ) return false;
            float * elems = (float *) ( (uint8_t *) &v.slice_f32.elements + v.slice_f32.elements.value );
            return parse_wref_f32( text, elems[0] );
        }
        if ( path == "map_f64[0].value" && kind == "float" )
        {
            cooktorture::TortureCookMapF64Entry * entries = (cooktorture::TortureCookMapF64Entry *) v.map_f64.Entries();
            return entries != NULL && v.map_f64.count >= 1 && parse_wref_f64( text, entries[0].value );
        }
        if ( path == "map_f64[0].key" )
        {
            cooktorture::TortureCookMapF64Entry * entries = (cooktorture::TortureCookMapF64Entry *) v.map_f64.Entries();
            return entries != NULL && v.map_f64.count >= 1 &&
                   write_refusal_text( entries[0].key, 16, entries[0].key_length, kind, text );
        }
        if ( path == "str_arr[0]" )
        {
            return v.str_arr_count >= 1 && write_refusal_text( v.str_arr[0].value, 16, v.str_arr[0].value_length, kind, text );
        }
        // SILENT-FAILURE ROWS: the variable Save's bits(N) width on every carrier
        if ( kind == "int" )
        {
            if ( path == "b_bits13" ) return wref_store_int( v.b_bits13, text );
            if ( path == "opt_bits" ) { v.opt_bits_present = true; return wref_store_int( v.opt_bits, text ); }
            if ( path == "arr_bits[0].v" )
            {
                if ( v.arr_bits.count < 1 || v.arr_bits.elements.value == 0 ) return false;
                cooktorture::BitsCell * cells = (cooktorture::BitsCell *) ( (uint8_t *) &v.arr_bits.elements + v.arr_bits.elements.value );
                return wref_store_int( cells[0].v, text );
            }
        }
    }
    if ( root == "StorePurchase" )
    {
        backenddemo::StorePurchase & v = *(backenddemo::StorePurchase *) value;
        if ( path == "quantity" && kind == "int" ) return wref_store_int( v.quantity, text );
        if ( path == "currency" && kind == "ordinal" ) return wref_store_enum( v.currency, text );
        if ( path == "sku" ) return write_refusal_text( v.sku, (int32_t) sizeof( v.sku ) - 1, v.sku_length, kind, text );
    }
    if ( root == "LoginRequest" )
    {
        backenddemo::LoginRequest & v = *(backenddemo::LoginRequest *) value;
        if ( path == "session_token" )
            return write_refusal_bytes_field( v.session_token, (int32_t) sizeof( v.session_token ), v.session_token_length, kind, text );
    }
    if ( root == "MatchResult" )
    {
        backenddemo::MatchResult & v = *(backenddemo::MatchResult *) value;
        if ( path == "players[0].score" && kind == "int" ) return wref_store_int( v.players[0].score, text );
    }
    if ( root == "EvoMsg" )
    {
        tblvarevo3::EvoMsg & v = *(tblvarevo3::EvoMsg *) value;
        if ( path == "payload.notice" )
            return write_refusal_text( v.payload.notice.value, 24, v.payload.notice.value_length, kind, text );
        if ( path == "payload" && kind == "tag" )
        {
            return wref_store_enum( v.payload.type, text );
        }
    }
    return write_refusal_set_variable( root, value, path, kind, text );
}

// The fixed form's FixedSave, a batch of one, for the roots the rows name.
static bool write_refusal_fixed_save( const std::string & root, const void * value, uint8_t * buffer, int64_t capacity, int64_t & wrote )
{
    if ( root == "CountedMin" ) { wrote = tabledemo::CountedMinFixedSave( (const tabledemo::CountedMin *) value, 1, buffer, capacity ); return true; }
    if ( root == "RootConfig" ) { wrote = tabledemo::RootConfigFixedSave( (const tabledemo::RootConfig *) value, 1, buffer, capacity ); return true; }
    if ( root == "LoadoutConfig" ) { wrote = tabledemo::LoadoutConfigFixedSave( (const tabledemo::LoadoutConfig *) value, 1, buffer, capacity ); return true; }
    if ( root == "Moods" ) { wrote = tabledemo::MoodsFixedSave( (const tabledemo::Moods *) value, 1, buffer, capacity ); return true; }
    if ( root == "MoodField" ) { wrote = tabledemo::MoodFieldFixedSave( (const tabledemo::MoodField *) value, 1, buffer, capacity ); return true; }
    if ( root == "SimState" ) { wrote = scalardemo::SimStateFixedSave( (const scalardemo::SimState *) value, 1, buffer, capacity ); return true; }
    if ( root == "TortureFixedWref" ) { wrote = cooktorture::TortureFixedWrefFixedSave( (const cooktorture::TortureFixedWref *) value, 1, buffer, capacity ); return true; }
    if ( root == "FixedBounds" ) { wrote = shapes::FixedBoundsFixedSave( (const shapes::FixedBounds *) value, 1, buffer, capacity ); return true; }
    if ( root == "Arrays" ) { wrote = surfaces::ArraysFixedSave( (const surfaces::Arrays *) value, 1, buffer, capacity ); return true; }
    return false;
}

// The message form's SaveMessages, a batch of one, against the connection's
// announced vocabulary: the announcement is read into the vocabulary the
// `message` surface reads with, and a connection whose announcement does not
// read is no case at all.
static bool write_refusal_message_save( const std::string & unit, const std::string & root, const void * value,
                                        const std::vector<uint8_t> & announcement, uint8_t * buffer, int64_t capacity, int64_t & wrote )
{
    if ( unit == "backenddemo" )
    {
        std::vector<backenddemo::TableMessageEntry> entries( (size_t) backenddemo::kTableMessageEntriesHere );
        backenddemo::TableVocabulary vocabulary( entries.data(), backenddemo::kTableMessageEntriesHere );
        backenddemo::TableReport report;
        if ( !backenddemo::AnnounceRead( vocabulary, announcement.data(), (int64_t) announcement.size(), &report ) ) _exit( 2 );
        if ( root == "StorePurchase" ) { wrote = backenddemo::StorePurchaseSaveMessages( (const backenddemo::StorePurchase *) value, 1, buffer, capacity, &report ); return true; }
        if ( root == "LoginRequest" ) { wrote = backenddemo::LoginRequestSaveMessages( (const backenddemo::LoginRequest *) value, 1, buffer, capacity, &report ); return true; }
        if ( root == "MatchResult" ) { wrote = backenddemo::MatchResultSaveMessages( (const backenddemo::MatchResult *) value, 1, buffer, capacity, &report ); return true; }
        return false;
    }
    if ( unit == "tblsurf" )
    {
        std::vector<surfaces::TableMessageEntry> entries( (size_t) surfaces::kTableMessageEntriesHere );
        surfaces::TableVocabulary vocabulary( entries.data(), surfaces::kTableMessageEntriesHere );
        surfaces::TableReport report;
        if ( !surfaces::AnnounceRead( vocabulary, announcement.data(), (int64_t) announcement.size(), &report ) ) _exit( 2 );
        if ( root == "Narrow" ) { wrote = surfaces::NarrowSaveMessages( (const surfaces::Narrow *) value, 1, buffer, capacity, &report ); return true; }
        if ( root == "Wide" ) { wrote = surfaces::WideSaveMessages( (const surfaces::Wide *) value, 1, buffer, capacity, &report ); return true; }
        if ( root == "Arrays" ) { wrote = surfaces::ArraysSaveMessages( (const surfaces::Arrays *) value, 1, buffer, capacity, &report ); return true; }
        if ( root == "Big" ) { wrote = surfaces::BigSaveMessages( (const surfaces::Big *) value, 1, buffer, capacity, &report ); return true; }
        return false;
    }
    if ( unit == "tblvarevo3" )
    {
        std::vector<tblvarevo3::TableMessageEntry> entries( (size_t) tblvarevo3::kTableMessageEntriesHere );
        tblvarevo3::TableVocabulary vocabulary( entries.data(), tblvarevo3::kTableMessageEntriesHere );
        tblvarevo3::TableReport report;
        if ( !tblvarevo3::AnnounceRead( vocabulary, announcement.data(), (int64_t) announcement.size(), &report ) ) _exit( 2 );
        if ( root == "EvoMsg" ) { wrote = tblvarevo3::EvoMsgSaveMessages( (const tblvarevo3::EvoMsg *) value, 1, buffer, capacity, &report ); return true; }
        return false;
    }
    return false;
}

// `wrote` on the text surface is the text written AND the field's JSON string
// spelling the zero as \u0000 (FORMAT.md, `write-refusal-json`): the key is the
// path's last name, and its string is read to its closing quote.
static bool write_refusal_json_holds_zero( const std::string & json, const std::string & path )
{
    const size_t dot = path.rfind( '.' );
    const std::string key = "\"" + ( dot == std::string::npos ? path : path.substr( dot + 1 ) ) + "\"";
    size_t at = json.find( key );
    if ( at == std::string::npos ) return false;
    at = json.find( '"', at + key.size() );
    if ( at == std::string::npos ) return false;
    std::string value;
    for ( size_t i = at + 1; i < json.size() && json[i] != '"'; i++ )
    {
        value += json[i];
        if ( json[i] == '\\' && i + 1 < json.size() ) value += json[++i];
    }
    return value.find( "\\u0000" ) != std::string::npos;
}

// THE 2026-11-22 KINDS (testdata/conformance/tables/FORMAT.md, `write-refusal`):
// `elems`, `mask`, `dupkey`, `point`, a decimal `float`, a `count` of -1 and
// an `int` past a bare intN's width. A kind this leg's STORAGE cannot hold is
// answered `unrepresentable`, never `refused` on the runtime's behalf: a native
// `[N]T` holds exactly N elements, an `int8_t` holds no 128. The child exits
// with this code for it.
enum
{
    kRowWireExit = 2, // the row's own wire did not load: a corpus fault, never a verdict
};

enum WrefSet
{
    kWrefNotMine, // a (root, path, kind) this setter does not know: the older setter's
    kWrefSet,
    kWrefUnrep,
    kWrefBad, // a value the row spells that does not parse
};

// a bare intN field, held to its own storage's width
template <typename T>
static WrefSet wref_int( T & field, const std::string & text )
{
    char * end = NULL;
    errno = 0;
    const long long n = strtoll( text.c_str(), &end, 10 );
    if ( end == NULL || *end != 0 || errno != 0 ) return kWrefBad;
    if ( n < (long long) std::numeric_limits<T>::min() || n > (long long) std::numeric_limits<T>::max() ) return kWrefUnrep;
    field = (T) n;
    return kWrefSet;
}

// a flags field's raw bits, in decimal: the storage is the unit's 64-bit mask
template <typename T>
static WrefSet wref_mask( T & field, const std::string & text )
{
    char * end = NULL;
    errno = 0;
    const unsigned long long n = strtoull( text.c_str(), &end, 10 );
    if ( end == NULL || *end != 0 || errno != 0 ) return kWrefBad;
    if ( n > (unsigned long long) std::numeric_limits<T>::max() ) return kWrefUnrep;
    field = (T) n;
    return kWrefSet;
}

// a NATIVE fixed array of `storage` elements: the declared count leaves it as
// loaded, and any other count is a value it cannot hold
static WrefSet wref_elems( int64_t storage, const std::string & text )
{
    char * end = NULL;
    const long long n = strtoll( text.c_str(), &end, 10 );
    if ( end == NULL || *end != 0 ) return kWrefBad;
    return n == storage ? kWrefSet : kWrefUnrep;
}

// `point`: the pointer at `path` is aimed at the node the pointer at `text`
// points at. A region slot is SELF-RELATIVE (docs/SPEC-TABLES.md §6.3), so the
// slot holds the delta from its own address. head.next aimed at head is a
// cycle; alias aimed at head is the lawful sharing of one node.
static bool wref_point_scene( graphdemo::Scene & v, const std::string & path, const std::string & text )
{
    if ( text != "head" || v.head.value == 0 ) return false;
    uint8_t * target = (uint8_t *) &v.head + v.head.value;
    graphdemo::TableRef * slot = NULL;
    if ( path == "head.next" ) slot = &( (graphdemo::ListNode *) target )->next;
    else if ( path == "alias" ) slot = &v.alias;
    else return false;
    slot->value = (int64_t) ( target - (uint8_t *) slot );
    return true;
}

static WrefSet write_refusal_set_1122( const std::string & root, void * value, const std::string & path,
                                       const std::string & kind, const std::string & text )
{
    if ( root == "ProfileConfig" && kind == "int" )
    {
        tabledemo::ProfileConfig & v = *(tabledemo::ProfileConfig *) value;
        if ( path == "tilt" ) return wref_int( v.tilt, text );
        if ( path == "badge" ) return wref_int( v.badge, text );
        if ( path == "experience" ) return wref_int( v.experience, text );
    }
    if ( root == "Arrays" )
    {
        surfaces::Arrays & v = *(surfaces::Arrays *) value;
        if ( path == "trio" && kind == "elems" ) return wref_elems( (int64_t) ( sizeof( v.trio ) / sizeof( v.trio[0] ) ), text );
        if ( path == "per" && kind == "elems" ) return wref_elems( (int64_t) ( sizeof( v.per.slots ) / sizeof( v.per.slots[0] ) ), text );
        if ( path == "marks" && kind == "mask" ) return wref_mask( v.marks, text );
    }
    if ( root == "Counted" && path == "few" && kind == "count" )
    {
        // a counted array's count, element 0 copied into new elements; -1 is a
        // negative count, which this leg's int32 count holds
        surfaces::Counted & v = *(surfaces::Counted *) value;
        char * end = NULL;
        const long long n = strtoll( text.c_str(), &end, 10 );
        if ( end == NULL || *end != 0 ) return kWrefBad;
        const int32_t storage = (int32_t) ( sizeof( v.few ) / sizeof( v.few[0] ) );
        for ( int32_t i = v.few_count; i < (int32_t) n && i < storage; i++ ) v.few[i] = v.few[0];
        v.few_count = (int32_t) n;
        return kWrefSet;
    }
    if ( root == "Narrow" && path == "hp" && kind == "int" ) return wref_int( ( (surfaces::Narrow *) value )->hp, text );
    if ( root == "Wide" && path == "hp" && kind == "int" ) return wref_int( ( (surfaces::Wide *) value )->hp, text );
    if ( root == "Big" )
    {
        surfaces::Big & v = *(surfaces::Big *) value;
        if ( path == "hp" && kind == "int" ) return wref_int( v.hp, text );
        if ( path == "mask" && kind == "int" ) return wref_int( v.mask, text );
        if ( path == "ratio" && kind == "float" ) return parse_wref_f32( text, v.ratio ) ? kWrefSet : kWrefBad;
    }
    if ( root == "MatchResult" && path == "players" && kind == "elems" )
    {
        backenddemo::MatchResult & v = *(backenddemo::MatchResult *) value;
        return wref_elems( (int64_t) ( sizeof( v.players ) / sizeof( v.players[0] ) ), text );
    }
    if ( root == "Scene" && kind == "point" ) return wref_point_scene( *(graphdemo::Scene *) value, path, text ) ? kWrefSet : kWrefBad;
    return kWrefNotMine;
}

// `dupkey 1` appends a copy of the map's first entry, so one key is held twice
// (FL-22). A region's map is a sorted entry array inside the region, so the
// copy goes on the end of a LARGER region: the region is copied whole (every
// slot is self-relative, so the copy keeps every reference), the map's entries
// move to its tail with the copy after them, and the map's slot is re-aimed
// there. A ships entry is a key and a by-value ShipConfig, relocatable as it
// stands. `dupkey 0` leaves the map as loaded. Answers 0 or the child's exit.
static int write_refusal_dupkey( const std::string & root, std::vector<uint8_t> & region, const void *& root_value,
                                 const std::string & path, const std::string & text )
{
    if ( root != "Fleet" || path != "ships" ) return kNoSetterExit;
    if ( text == "0" ) return 0;
    if ( text != "1" ) return kNoSetterExit;
    const mapdemo::Fleet * old_root = (const mapdemo::Fleet *) root_value;
    const int32_t count = old_root->ships.count;
    const mapdemo::FleetShipsEntry * entries = old_root->ships.Entries();
    if ( count < 1 || entries == NULL ) return kNoSetterExit;
    const size_t root_at = (size_t) ( (const uint8_t *) root_value - region.data() );
    const size_t tail = ( region.size() + 63 ) & ~(size_t) 63;
    std::vector<uint8_t> grown( tail + sizeof( mapdemo::FleetShipsEntry ) * (size_t) ( count + 1 ), 0 );
    memcpy( grown.data(), region.data(), region.size() );
    mapdemo::Fleet * new_root = (mapdemo::Fleet *) (void *) ( grown.data() + root_at );
    mapdemo::FleetShipsEntry * moved = (mapdemo::FleetShipsEntry *) (void *) ( grown.data() + tail );
    for ( int32_t i = 0; i < count; i++ ) moved[i] = entries[i];
    moved[count] = entries[0];
    new_root->ships.entries.value = (int64_t) ( (uint8_t *) moved - (uint8_t *) &new_root->ships.entries );
    new_root->ships.count = count + 1;
    region.swap( grown );
    root_value = new_root;
    return 0;
}

// THE COOK WRITER over the row's value (docs/SPEC-TABLES.md §7.6): CookMeasure,
// then Cook in little-endian order; `wrote` when both complete.
static int write_refusal_cook( int64_t ( *measure )( const void * ), bool ( *cook )( const void *, void *, uint64_t, int ),
                               const void * value )
{
    const int64_t need = measure( value );
    if ( need < 0 ) return kRefusedExit;
    std::vector<uint8_t> file( (size_t) ( need > 0 ? need : 1 ) );
    return cook( value, file.data(), (uint64_t) need, 0 ) ? kWroteExit : kRefusedExit;
}

// THE BLOCK BUILDER over the row's value (docs/SPEC-TABLES.md §19.1): the
// storage is created, Begin is handed the value's counts and settles the
// layout, the projection's fields are the producer's to write and are written
// from the value, and BlockBytes closes it. `wrote` when the block is built.
// The C++ builder takes the counts of out-of-line arrays and nothing else: a
// used length or a range is not an argument of Begin, so nothing in the
// builder sees one.
static int write_refusal_block( const std::string & root, const void * value )
{
    if ( root == "StorePurchase" )
    {
        const backenddemo::StorePurchase & v = *(const backenddemo::StorePurchase *) value;
        backenddemo::StorePurchaseBlockStorage storage;
        if ( !storage.Create( backenddemo::TableBlockDefaultAllocator() ) ) return kRowWireExit;
        backenddemo::StorePurchaseBlock block;
        backenddemo::StorePurchaseCounts counts; // StorePurchase declares no out-of-line array
        backenddemo::TableBlockRefusal refusal;
        const bool begun = backenddemo::StorePurchaseBlockBegin( block, storage, counts, &refusal );
        if ( begun )
        {
            block.projection->player_id = v.player_id;
            memcpy( block.projection->sku, v.sku, sizeof( block.projection->sku ) );
            block.projection->sku_length = v.sku_length;
            block.projection->quantity = v.quantity;
            block.projection->price_minor = v.price_minor;
            block.projection->currency = v.currency;
            if ( backenddemo::StorePurchaseBlockBytes( block ) < 0 ) { storage.Destroy(); return kRefusedExit; }
        }
        storage.Destroy();
        return begun ? kWroteExit : kRefusedExit;
    }
    if ( root == "MatchResult" )
    {
        const backenddemo::MatchResult & v = *(const backenddemo::MatchResult *) value;
        backenddemo::MatchResultBlockStorage storage;
        if ( !storage.Create( backenddemo::TableBlockDefaultAllocator() ) ) return kRowWireExit;
        backenddemo::MatchResultBlock block;
        backenddemo::MatchResultCounts counts; // players is a [10] array held in the projection
        backenddemo::TableBlockRefusal refusal;
        const bool begun = backenddemo::MatchResultBlockBegin( block, storage, counts, &refusal );
        if ( begun )
        {
            block.projection->match_id = v.match_id;
            for ( size_t i = 0; i < sizeof( v.players ) / sizeof( v.players[0] ); i++ ) block.projection->players[i] = v.players[i];
            if ( backenddemo::MatchResultBlockBytes( block ) < 0 ) { storage.Destroy(); return kRefusedExit; }
        }
        storage.Destroy();
        return begun ? kWroteExit : kRefusedExit;
    }
    return kNoSetterExit;
}

// The writer a surface names, over the one value the row set. Answers the
// child's exit code.
static int write_refusal_write( const std::string & surface, const std::string & unit, const std::string & root,
                                const std::string & path, const Codec * codec, const void * value,
                                const std::vector<uint8_t> & announcement )
{
    if ( surface == "write-refusal-block" ) return write_refusal_block( root, value );
    if ( surface == "write-refusal-cook" ) return write_refusal_cook( codec->cook_measure, codec->cook, value );
    scratch.assign( (size_t) 1 << 20, 0 );
    const int64_t capacity = (int64_t) scratch.size();
    int64_t wrote = -1;
    // the shape rows' lawful twins are written as write-refusal writes them
    if ( surface == "write-refusal" || surface == "write-refusal-shape" ) wrote = codec->save( value, scratch.data(), capacity );
    else if ( surface == "write-refusal-fixed" )
    {
        if ( !write_refusal_fixed_save( root, value, scratch.data(), capacity, wrote ) ) return kNoSetterExit;
    }
    else if ( surface == "write-refusal-message" )
    {
        if ( !write_refusal_message_save( unit, root, value, announcement, scratch.data(), capacity, wrote ) ) return kNoSetterExit;
    }
    else
    {
        const int64_t size = codec->to_json_measure( value );
        if ( size < 0 ) return kRefusedExit;
        std::vector<char> text( (size_t) size + 1 );
        if ( codec->to_json( value, text.data(), size ) != size ) return kRefusedExit;
        if ( path == "version_note" && std::string( text.data(), (size_t) size ).find( "\\u0000" ) != std::string::npos )
            return write_refusal_json_holds_zero( std::string( text.data(), (size_t) size ), path ) ? kWroteExit : kRefusedExit;
        return kWroteExit;
    }
    return wrote < 0 ? kRefusedExit : kWroteExit;
}

// sets the one field a row names: the 2026-11-22 kinds first, then the older
// setter; answers 0 or the child's exit code
static int write_refusal_set_any( const std::string & root, void * value, const std::string & path,
                                  const std::string & kind, const std::string & text )
{
    const WrefSet set = write_refusal_set_1122( root, value, path, kind, text );
    if ( set == kWrefSet ) return 0;
    if ( set == kWrefUnrep ) return kUnrepresentableExit;
    if ( set == kWrefBad ) return kNoSetterExit;
    return write_refusal_set( root, value, path, kind, text ) ? 0 : kNoSetterExit;
}

// A row whose <wire> is `-` starts from the root's FRESH value (SF-05, SF-06:
// a fixed table past the 65536-byte ceiling has no file form to load). Every
// case runs in a child, which sets an alarm: a writer that walks a cycle
// forever ends the case by SIGALRM, which is named and answers nothing.
static const unsigned kWrefChildSeconds = 30;

int surface_write_refusal( const std::string & surface, const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != surface ) continue;
        // <surface> <case> <unit> <root> <wire> <path> <kind> <value>
        if ( f.size() < 8 ) { fprintf( stderr, "driver: short %s line\n", surface.c_str() ); return 1; }
        std::string unit = f[2];
        std::vector<uint8_t> announcement;
        if ( surface == "write-refusal-message" )
        {
            std::string announcement_path;
            unit.clear();
            for ( size_t j = 0; j < manifest_lines.size(); j++ )
            {
                const std::vector<std::string> & c = manifest_lines[j].field;
                if ( c[0] == "connection" && c[1] == f[2] ) { unit = c[2]; announcement_path = c[4]; break; }
            }
            if ( unit.empty() ) { fprintf( stderr, "driver: no connection %s\n", f[2].c_str() ); return 1; }
            if ( !slurp( announcement_path.c_str(), announcement ) ) { fprintf( stderr, "driver: cannot read %s\n", announcement_path.c_str() ); return 1; }
        }
        const Codec * codec = find_codec( unit, f[3] );
        const VarCodec * var_codec = codec == NULL ? find_var_codec( unit, f[3] ) : NULL;
        if ( codec == NULL && var_codec == NULL ) { fprintf( stderr, "driver: no codec for %s.%s\n", unit.c_str(), f[3].c_str() ); return 1; }
        const bool fresh = f[4] == "-";
        if ( fresh && var_codec != NULL ) { fprintf( stderr, "driver: %s %s: a variable root has no fresh value here\n", surface.c_str(), f[1].c_str() ); return 1; }
        std::vector<uint8_t> wire;
        if ( !fresh && !slurp( f[4].c_str(), wire ) ) { fprintf( stderr, "driver: cannot read %s\n", f[4].c_str() ); return 1; }
        fflush( stdout );
        fflush( stderr );
        const pid_t child = fork();
        if ( child < 0 ) { fprintf( stderr, "driver: fork failed\n" ); return 1; }
        if ( child == 0 )
        {
#if defined( __linux__ )
            // an assert's abort is the refusal, not a crash: no core, so no
            // crash reporter (apport) runs, and waits, for every refused row
            // (the C leg's wref.c does the same)
            (void) prctl( PR_SET_DUMPABLE, 0L, 0L, 0L, 0L );
#endif
            alarm( kWrefChildSeconds );
            // A VALUE OF THE WRONG KIND (`write-refusal-shape` kind `shape`,
            // DW-TABLE-SHAPE) is one C++'s types cannot hold: the compiler
            // refuses it before any writer runs
            if ( surface == "write-refusal-shape" && f[6] == "shape" ) _exit( kUnrepresentableExit );
            if ( var_codec != NULL )
            {
                std::vector<uint8_t> region;
                Report report;
                const void * cval = var_codec->load( region, wire.data(), (int64_t) wire.size(), &report );
                if ( cval == NULL ) _exit( kRowWireExit );
                const int set = f[6] == "dupkey" ? write_refusal_dupkey( f[3], region, cval, f[5], f[7] )
                                                 : write_refusal_set_any( f[3], const_cast<void *>( cval ), f[5], f[6], f[7] );
                if ( set != 0 ) _exit( set );
                const void * mval = cval;
                const int quiet = open( "/dev/null", O_WRONLY );
                if ( quiet >= 0 ) dup2( quiet, 2 );
                if ( surface == "write-refusal" || surface == "write-refusal-shape" )
                {
                    scratch.assign( (size_t) 1 << 20, 0 );
                    const int64_t sz = var_codec->measure( mval );
                    if ( sz < 0 ) _exit( kRefusedExit );
                    const int64_t wr = var_codec->save( mval, scratch.data(), (int64_t) scratch.size() );
                    _exit( wr < 0 ? kRefusedExit : kWroteExit );
                }
                if ( surface == "write-refusal-json" )
                {
                    std::vector<char> jtxt;
                    _exit( var_codec->to_json( mval, jtxt ) ? kWroteExit : kRefusedExit );
                }
                if ( surface == "write-refusal-cook" ) _exit( write_refusal_cook( var_codec->cook_measure, var_codec->cook, mval ) );
                _exit( kNoSetterExit );
            }
            void * value = codec->make();
            codec->reset( value );
            if ( !fresh )
            {
                Report report;
                if ( !codec->load( value, wire.data(), (int64_t) wire.size(), &report ) ) _exit( kRowWireExit );
            }
            const int set = write_refusal_set_any( f[3], value, f[5], f[6], f[7] );
            if ( set != 0 ) _exit( set );
            const int quiet = open( "/dev/null", O_WRONLY );
            if ( quiet >= 0 ) dup2( quiet, 2 );
            _exit( write_refusal_write( surface, unit, f[3], f[5], codec, value, announcement ) );
        }
        int status = 0;
        if ( waitpid( child, &status, 0 ) != child ) { fprintf( stderr, "driver: waitpid failed\n" ); return 1; }
        const char * verdict = NULL;
        if ( WIFSIGNALED( status ) && WTERMSIG( status ) == SIGABRT ) verdict = "refused\n";
        else if ( WIFEXITED( status ) && WEXITSTATUS( status ) == kRefusedExit ) verdict = "refused\n";
        else if ( WIFEXITED( status ) && WEXITSTATUS( status ) == kWroteExit ) verdict = "wrote\n";
        else if ( WIFEXITED( status ) && WEXITSTATUS( status ) == kUnrepresentableExit ) verdict = "unrepresentable\n";
        else if ( WIFEXITED( status ) && WEXITSTATUS( status ) == kNoSetterExit )
        {
            fprintf( stderr, "driver: %s %s: no setter for %s %s %s\n", surface.c_str(), f[1].c_str(), f[3].c_str(), f[5].c_str(), f[6].c_str() );
            return 1;
        }
        else if ( WIFEXITED( status ) && WEXITSTATUS( status ) == kRowWireExit )
        {
            fprintf( stderr, "driver: %s %s: the row's wire %s did not load\n", surface.c_str(), f[1].c_str(), f[4].c_str() );
            return 1;
        }
        else if ( WIFSIGNALED( status ) )
        {
            // THE RUNTIME DIED ANOTHER WAY (a fault, or the alarm on a writer
            // that never returned): the case is unanswered and says why, and
            // the rows after it are still asked
            fprintf( stderr, "driver: %s %s: the writer's child died by signal %d%s; no answer\n", surface.c_str(), f[1].c_str(),
                     WTERMSIG( status ), WTERMSIG( status ) == SIGALRM ? " (it ran past the alarm)" : "" );
            continue;
        }
        else
        {
            fprintf( stderr, "driver: %s %s: the child ended with status 0x%x\n", surface.c_str(), f[1].c_str(), status );
            return 1;
        }
        if ( !spill( out, f[1], verdict, strlen( verdict ) ) ) return 1;
    }
    return 0;
}
