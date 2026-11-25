// THE SURFACES ANSWERED THROUGH THE CODEC ROWS (test/conformance/README.md):
// every surface whose answer is one loop over the manifest and a row of the
// registry (driver.h), so this unit reads no generated header at all. The
// rows themselves are the family units' (driver_<family>.cpp); a surface here
// finds one by (unit, root) and calls through its erased columns.

#include <clocale>

#include "driver.h"

// ---------------------------------------------------------------------------
// the surfaces
// ---------------------------------------------------------------------------

static std::vector<uint8_t> scratch;

// Record the strict reader's actual verdict for the harness to compare.
static bool spill_refusal( const std::string & out, const std::string & name )
{
    static const char refused[] = "refused\n";
    return spill( out, name, refused, sizeof( refused ) - 1 );
}

// THE VERDICT LINE of a `report`, `framing` and `json-hostile` answer: the
// legacy seven fields the harness still reads, with the retired counters at
// zero and the refusal flag the only thing that selects the verdict
// (test/conformance/README.md, "Every read has one outcome").
static std::vector<char> verdict_line( const Report & report, bool read )
{
    char text[128];
    const int n = snprintf( text, sizeof( text ), "%d,0,%d,0,0,false,%s\n",
                            report.unknown, report.widened, read ? "read" : "refused" );
    return std::vector<char>( text, text + n );
}

int surface_wire( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "instance" ) continue;
        std::vector<uint8_t> wire;
        if ( !slurp( f[4].c_str(), wire ) ) { fprintf( stderr, "driver: cannot read %s\n", f[4].c_str() ); return 1; }
        const VarCodec * variable = find_var_codec( f[2], f[3] );
        if ( variable != NULL )
        {
            std::vector<uint8_t> region;
            Report report;
            const void * root = variable->load( region, wire.data(), (int64_t) wire.size(), &report );
            if ( root == NULL )
            {
                if ( !spill_refusal( out, f[1] ) ) return 1;
                continue;
            }
            int64_t size = variable->measure( root );
            if ( size < 0 ) return 1;
            scratch.assign( (size_t) size, 0 );
            if ( variable->save( root, scratch.data(), size ) != size ) return 1;
            if ( !spill( out, f[1], scratch.data(), (size_t) size ) ) return 1;
            continue;
        }
        const Codec * codec = find_codec( f[2], f[3] );
        if ( codec == NULL ) { fprintf( stderr, "driver: no codec for %s.%s\n", f[2].c_str(), f[3].c_str() ); return 1; }

        void * value = codec->make();
        codec->reset( value );
        Report report;
        if ( !codec->load( value, wire.data(), (int64_t) wire.size(), &report ) )
        {
            if ( !spill_refusal( out, f[1] ) ) return 1;
            continue;
        }
        int64_t size = codec->measure( value );
        if ( size < 0 ) return 1;
        scratch.assign( (size_t) size, 0 );
        if ( codec->save( value, scratch.data(), size ) != size ) return 1;
        if ( !spill( out, f[1], scratch.data(), (size_t) size ) ) return 1;
    }
    return 0;
}

// THE MESSAGE SURFACE (docs/SPEC-TABLES.md §3.3): read the message against the
// connection's announced table, and write it back. The expectation is the
// message golden itself, exactly as the wire surface's is the wire golden.
//
// With prior set it answers the `message-prior` surface
// (testdata/conformance/tables/FORMAT.md): a prior writer's lawful batch read
// at today's reader, handed on its own keywords. Such a row may owe a refusal,
// so the answer is what the public read returned: the re-saved batch, or
// `<case>.refused`, whose contents the harness never compares
// (docs/rulings.json `refusal-verdict-only`).
int surface_message( const std::string & out, bool prior )
{
    const char * connection_kind = prior ? "prior-connection" : "connection";
    const char * message_kind = prior ? "prior-message" : "message";
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( !prior && f[0] == "message-batch" )
        {
            // message-batch <case> <connection> <root> <wire>
            std::string batch_unit, batch_announcement;
            for ( size_t j = 0; j < manifest_lines.size(); j++ )
            {
                const std::vector<std::string> & c = manifest_lines[j].field;
                if ( c[0] == "connection" && c[1] == f[2] ) { batch_unit = c[2]; batch_announcement = c[4]; break; }
            }
            const MsgCodec * batch_codec = find_msg_codec( batch_unit, f[3] );
            if ( batch_codec == NULL ) { fprintf( stderr, "driver: no message codec for %s.%s\n", batch_unit.c_str(), f[3].c_str() ); return 1; }
            std::vector<uint8_t> announcement, message;
            if ( !slurp( batch_announcement.c_str(), announcement ) || !slurp( f[4].c_str(), message ) ) { fprintf( stderr, "driver: cannot read the files of %s\n", f[1].c_str() ); return 1; }
            int64_t reported = 0;
            if ( !batch_codec->batch( announcement.data(), (int64_t) announcement.size(), message.data(), (int64_t) message.size(), reported ) ) { fprintf( stderr, "driver: %s: the announcement did not read\n", f[1].c_str() ); return 1; }
            std::string text = "count " + std::to_string( (long long) reported ) + "\n";
            if ( !spill( out, f[1], (const uint8_t *) text.data(), text.size() ) ) return 1;
            continue;
        }
        if ( f[0] != message_kind ) continue;
        // message <name> <connection> <root> <file-form wire> <message-form wire>
        std::string unit, announcement_path;
        for ( size_t j = 0; j < manifest_lines.size(); j++ )
        {
            const std::vector<std::string> & c = manifest_lines[j].field;
            if ( c[0] == connection_kind && c[1] == f[2] ) { unit = c[2]; announcement_path = c[4]; break; }
        }
        if ( unit.empty() ) { fprintf( stderr, "driver: no connection %s\n", f[2].c_str() ); return 1; }
        const MsgCodec * codec = find_msg_codec( unit, f[3] );
        if ( f[5] == "-" )
        {
            // a variable table: the message form is fixed tables only (§3.3),
            // so no codec exists, and the row's answer is the refusal (R13)
            if ( codec != NULL ) { fprintf( stderr, "driver: %s.%s is a refusal row and carries a message codec\n", unit.c_str(), f[3].c_str() ); return 1; }
            static const char refused[] = "refused\n";
            if ( !spill( out, f[1], (const uint8_t *) refused, sizeof( refused ) - 1 ) ) return 1;
            continue;
        }
        if ( codec == NULL ) { fprintf( stderr, "driver: no message codec for %s.%s\n", unit.c_str(), f[3].c_str() ); return 1; }
        std::vector<uint8_t> announcement, message;
        if ( !slurp( announcement_path.c_str(), announcement ) ) { fprintf( stderr, "driver: cannot read %s\n", announcement_path.c_str() ); return 1; }
        if ( !slurp( f[5].c_str(), message ) ) { fprintf( stderr, "driver: cannot read %s\n", f[5].c_str() ); return 1; }
        std::vector<uint8_t> answer;
        Report report;
        if ( !codec->round_trip( announcement.data(), (int64_t) announcement.size(),
                                 message.data(), (int64_t) message.size(), answer, &report ) )
        {
            if ( !prior ) return 1;
            // THE ANSWER IS WHAT THE PUBLIC READ RETURNED: a batch the read
            // refuses delivers no body, so the row is answered refused and
            // nothing is saved. Whether the row owed a refusal is the
            // harness's to say.
            static const char refused[] = "refused";
            if ( !spill( out, f[1] + ".refused", (const uint8_t *) refused, sizeof( refused ) - 1 ) ) return 1;
            continue;
        }
        if ( !spill( out, f[1], answer.data(), answer.size() ) ) return 1;
    }
    return 0;
}

// THE RETAIN SURFACES (docs/SPEC-TABLES.md §6.6): the COUNTERS on one and the
// BYTES on the other, so each is one shape. A row's `retain` line carries the
// wire and the two capacities; nothing about the answer reaches the driver.
static bool retain_row( const std::vector<std::string> & f, RetainAnswer & answer )
{
    // retain <case> <unit> <root> <wire> <capacity> <ids>
    const std::string unit = f[2], root = f[3], wire_path = f[4];
    const bool short_buffer = f[5] == "short";
    int64_t ids = -1;
    if ( f[6] != "full" ) { ids = (int64_t) strtoll( f[6].c_str(), NULL, 10 ); }

    std::vector<uint8_t> wire;
    if ( !slurp( wire_path.c_str(), wire ) ) { fprintf( stderr, "driver: cannot read %s\n", wire_path.c_str() ); return false; }
    const RetainCodec * codec = find_retain_codec( unit, root );
    if ( codec == NULL ) { fprintf( stderr, "driver: no retain codec for %s.%s\n", unit.c_str(), root.c_str() ); return false; }
    return codec->run( wire.data(), (int64_t) wire.size(), short_buffer, ids, answer );
}

int surface_retain( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "retain" ) continue;
        RetainAnswer answer;
        if ( !retain_row( f, answer ) ) return 1;
        // the whole KNOWN value or the refusal (test/conformance/README.md):
        // the retention counters are this port's diagnostics, never compared
        std::vector<char> text;
        static const char read[] = "read\n", refused[] = "refused\n";
        if ( answer.read ) text.insert( text.end(), read, read + sizeof( read ) - 1 );
        else text.insert( text.end(), refused, refused + sizeof( refused ) - 1 );
        text.insert( text.end(), answer.text.begin(), answer.text.end() );
        if ( !spill( out, f[1], text.data(), text.size() ) ) return 1;
    }
    return 0;
}

int surface_retain_save( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "retain" ) continue;
        RetainAnswer answer;
        if ( !retain_row( f, answer ) ) return 1;
        if ( !spill( out, f[1], answer.saved.data(), answer.saved.size() ) ) return 1;
    }
    return 0;
}

// THE RETAIN-REFUSAL SURFACE: the sequence and its answer files are
// RETAINSEQUENCE's (driver.h)
int surface_retain_refusal( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "retain-refusal" ) continue;
        const RetainSequenceCodec * codec = find_retain_sequence_codec( f[2], f[3] );
        if ( codec == NULL ) { fprintf( stderr, "driver: no retain codec for %s.%s\n", f[2].c_str(), f[3].c_str() ); return 1; }
        std::vector<uint8_t> first, second;
        if ( !slurp( f[4].c_str(), first ) || !slurp( f[5].c_str(), second ) ) { fprintf( stderr, "driver: cannot read %s or %s\n", f[4].c_str(), f[5].c_str() ); return 1; }
        RetainSequence answer;
        if ( !codec->run( first, second, answer ) ) { fprintf( stderr, "retain-refusal %s failed\n", f[1].c_str() ); return 1; }
        std::string text = "refused\n";
        if ( answer.first_read )
        {
            if ( !spill( out, f[1] + ".before", answer.before.data(), answer.before.size() ) ) return 1;
            text = std::string( "read\n" ) + ( answer.second_refused ? "refused\n" : "read\n" );
            if ( answer.second_saved )
            {
                if ( !spill( out, f[1] + ".wire", answer.after.data(), answer.after.size() ) ) return 1;
                text += "saved\n";
            }
            else
            {
                text += "save-refused\n";
            }
        }
        if ( !spill( out, f[1], text.data(), text.size() ) ) return 1;
    }
    return 0;
}

// The §16 text of a by-value codec's value: the value half of a `report` and
// `json-hostile` answer (test/conformance/README.md).
static bool codec_text( const Codec * codec, void * value, std::vector<char> & text )
{
    int64_t size = codec->to_json_measure( value );
    if ( size < 0 ) return false;
    text.resize( (size_t) size + 1 );
    if ( codec->to_json( value, text.data(), size ) != size ) return false;
    text.resize( (size_t) size );
    return true;
}

// THE ONE READ AND ITS WRITE-BACK (test/conformance/README.md): an accepted row
// owes the verdict line, the §16 text of the value, and the canonical wire the
// value saves to in `<case>.wire`; a refused row owes `refused` and nothing
// else. `with_value` is false for a framing row that says `no-value`, which
// still owes its wire. The value is ONE value in two forms, so both come from
// the same read. `form1` reads a by-value root through its form-1 read, which
// only the framing battery asks for.
static bool read_answer( const std::string & out, const std::string & name, const std::vector<uint8_t> & wire,
                         const VarCodec * variable, const Codec * codec, bool with_value, bool form1 )
{
    Report report = Report();
    std::vector<char> value_text;
    std::vector<uint8_t> saved;
    bool ok = false;
    // the framing battery's read is a form-1 read, and the generated load is
    // the strict read (§6.5): the whole wire's framing is judged inside the
    // load, before any value is decoded, and its verdict is this read's.
    if ( variable != NULL )
    {
        std::vector<uint8_t> region;
        const void * root = variable->load( region, wire.data(), (int64_t) wire.size(), &report );
        ok = root != NULL && !report.refused;
        if ( ok )
        {
            if ( with_value && !variable->to_json( root, value_text ) ) return false;
            const int64_t size = variable->measure( root );
            if ( size < 0 ) return false;
            saved.assign( (size_t) size, 0 );
            if ( variable->save( root, saved.data(), size ) != size ) return false;
        }
    }
    else
    {
        void * value = codec->make();
        codec->reset( value );
        ok = ( form1 ? codec->load_form1 : codec->load )( value, wire.data(), (int64_t) wire.size(), &report ) && !report.refused;
        if ( ok )
        {
            if ( with_value && !codec_text( codec, value, value_text ) ) return false;
            // the write-back is the root's OWN file form whichever form was
            // read: the value is one value, and its canonical bytes are form 3
            const int64_t size = codec->measure( value );
            if ( size < 0 ) return false;
            saved.assign( (size_t) size, 0 );
            if ( codec->save( value, saved.data(), size ) != size ) return false;
        }
    }
    std::vector<char> answer = verdict_line( report, ok );
    if ( ok )
    {
        answer.insert( answer.end(), value_text.begin(), value_text.end() );
        if ( !spill( out, name + ".wire", saved.data(), saved.size() ) ) return false;
    }
    return spill( out, name, answer.data(), answer.size() );
}

int surface_report( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "report" ) continue;
        std::vector<uint8_t> wire;
        if ( !slurp( f[4].c_str(), wire ) ) { fprintf( stderr, "driver: cannot read %s\n", f[4].c_str() ); return 1; }
        const VarCodec * variable = find_var_codec( f[2], f[3] );
        const Codec * codec = variable == NULL ? find_codec( f[2], f[3] ) : NULL;
        if ( variable == NULL && codec == NULL ) { fprintf( stderr, "driver: no codec for %s.%s\n", f[2].c_str(), f[3].c_str() ); return 1; }
        if ( !read_answer( out, f[1], wire, variable, codec, true, false ) ) return 1;
    }
    return 0;
}

// THE FRAMING VECTORS (docs/LAYERS/1-framing.md): the same answer as the
// `report` surface, the value left out where the row says `no-value`. A
// derived line is `framing <case> <unit> <root> <wire> [no-value]`.
int surface_framing( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "framing" ) continue;
        const bool no_value = f.size() > 5 && f[5] == "no-value";
        std::vector<uint8_t> wire;
        if ( !slurp( f[4].c_str(), wire ) ) { fprintf( stderr, "driver: cannot read %s\n", f[4].c_str() ); return 1; }
        const VarCodec * variable = find_var_codec( f[2], f[3] );
        const Codec * codec = variable == NULL ? find_codec( f[2], f[3] ) : NULL;
        if ( variable == NULL && codec == NULL )
        {
            // a root this leg's table carries no codec for: absent per
            // case, which the matrix counts against the reference leg
            if ( !spill( out, f[1] + ".absent", "", 0 ) ) return 1;
            continue;
        }
        if ( !read_answer( out, f[1], wire, variable, codec, !no_value, true ) ) return 1;
    }
    return 0;
}

// an instance the corpus carries on the wire alone: past the text form's depth
// cap by the form's own rule (§16.7), so no leg is asked for its text
static bool no_text( const std::vector<std::string> & f )
{
    return f.size() > 5 && f[5] == "no-text";
}

int surface_json_read( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "instance" ) continue;
        if ( no_text( f ) ) continue;
        std::vector<uint8_t> text;
        std::string path = "testdata/conformance/tables/json/" + f[1] + ".json";
        if ( !slurp( path.c_str(), text ) ) { fprintf( stderr, "driver: cannot read %s\n", path.c_str() ); return 1; }
        const VarCodec * variable = find_var_codec( f[2], f[3] );
        if ( variable != NULL )
        {
            Report report;
            std::vector<uint8_t> bytes;
            if ( !variable->json_read( (const char *) text.data(), (int64_t) text.size(), &report, bytes ) ) return 1;
            if ( !spill( out, f[1], bytes.data(), bytes.size() ) ) return 1;
            continue;
        }
        const Codec * codec = find_codec( f[2], f[3] );
        if ( codec == NULL ) return 1;

        void * value = codec->make();
        codec->reset( value );
        Report report;
        if ( !codec->from_json( value, (const char *) text.data(), (int64_t) text.size(), &report ) ) return 1;
        int64_t size = codec->measure( value );
        if ( size < 0 ) return 1;
        scratch.assign( (size_t) size, 0 );
        if ( codec->save( value, scratch.data(), size ) != size ) return 1;
        if ( !spill( out, f[1], scratch.data(), (size_t) size ) ) return 1;
    }
    return 0;
}

// A CASE THE DRIVER CANNOT ANSWER IS THAT CASE'S FAILURE, NAMED: the driver
// says why on stderr, writes no answer for it, and goes on to the next row, so
// the harness scores the case as "the driver wrote nothing" and every later
// row is still compared. Only a file it cannot read or write stops the run.
int surface_json_write( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "instance" ) continue;
        if ( no_text( f ) ) continue;
        std::vector<uint8_t> wire;
        if ( !slurp( f[4].c_str(), wire ) ) { fprintf( stderr, "driver: cannot read %s\n", f[4].c_str() ); return 1; }
        const VarCodec * variable = find_var_codec( f[2], f[3] );
        if ( variable != NULL )
        {
            std::vector<uint8_t> region;
            Report report;
            const void * root = variable->load( region, wire.data(), (int64_t) wire.size(), &report );
            if ( root == NULL ) { fprintf( stderr, "driver: json-write %s: load refused %s\n", f[1].c_str(), f[4].c_str() ); continue; }
            std::vector<char> text;
            if ( !variable->to_json( root, text ) ) { fprintf( stderr, "driver: json-write %s: ToJson refused the value\n", f[1].c_str() ); continue; }
            if ( !spill( out, f[1] + ".json", text.data(), text.size() ) ) return 1;
            continue;
        }
        const Codec * codec = find_codec( f[2], f[3] );
        if ( codec == NULL ) { fprintf( stderr, "driver: json-write %s: no codec for %s.%s\n", f[1].c_str(), f[2].c_str(), f[3].c_str() ); continue; }

        void * value = codec->make();
        codec->reset( value );
        Report report;
        if ( !codec->load( value, wire.data(), (int64_t) wire.size(), &report ) ) { fprintf( stderr, "driver: json-write %s: load refused %s\n", f[1].c_str(), f[4].c_str() ); continue; }
        int64_t size = codec->to_json_measure( value );
        if ( size < 0 ) { fprintf( stderr, "driver: json-write %s: ToJsonMeasure refused the value\n", f[1].c_str() ); continue; }
        std::vector<char> text( (size_t) size + 1 );
        if ( codec->to_json( value, text.data(), size ) != size ) { fprintf( stderr, "driver: json-write %s: ToJson did not write ToJsonMeasure's answer\n", f[1].c_str() ); continue; }
        if ( !spill( out, f[1] + ".json", text.data(), (size_t) size ) ) return 1;
    }
    return 0;
}

int surface_json_write_refusal( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "json-write-refusal" ) continue;
        if ( f.size() < 6 ) { fprintf( stderr, "driver: a json-write-refusal line is <case> <unit> <root> <wire> <damage>\n" ); return 1; }
        std::vector<uint8_t> wire;
        if ( !slurp( f[4].c_str(), wire ) ) { fprintf( stderr, "driver: cannot read %s\n", f[4].c_str() ); return 1; }
        Report report = Report();
        std::vector<char> text;
        bool wrote = false;
        const VarCodec * variable = find_var_codec( f[2], f[3] );
        if ( variable != NULL )
        {
            if ( f[5] != "none" ) { fprintf( stderr, "driver: %s: a region root takes no damage\n", f[1].c_str() ); return 1; }
            std::vector<uint8_t> region;
            const void * root = variable->load( region, wire.data(), (int64_t) wire.size(), &report );
            if ( root == NULL ) { fprintf( stderr, "driver: json-write-refusal %s: load refused %s\n", f[1].c_str(), f[4].c_str() ); continue; }
            wrote = variable->to_json( root, text );
        }
        else
        {
            const Codec * codec = find_codec( f[2], f[3] );
            if ( codec == NULL ) { fprintf( stderr, "driver: no codec for %s.%s\n", f[2].c_str(), f[3].c_str() ); return 1; }
            void * value = codec->make();
            codec->reset( value );
            if ( !codec->load( value, wire.data(), (int64_t) wire.size(), &report ) ) { fprintf( stderr, "driver: json-write-refusal %s: load refused %s\n", f[1].c_str(), f[4].c_str() ); continue; }
            if ( !damage_text( f[2], f[3], value, f[5] ) ) return 1;
            wrote = codec_text( codec, value, text );
        }
        static const char refused[] = "refused\n";
        if ( !wrote ) text.assign( refused, refused + sizeof( refused ) - 1 );
        if ( !spill( out, f[1] + ".json", text.data(), text.size() ) ) return 1;
    }
    return 0;
}

// json-text: the nightly's CROSS-READ of a port's text (test/conformance/
// README.md, "The nightly"; docs/SPEC-TABLES.md §16). The harness hands a
// substituted manifest whose lines name a PORT's JSON text,
//   json-text <case> <unit> <root> <text>
// and the reference reads it, writes it back, and the text goes out as
// <case>.json; the harness holds it to the port's bytes. It is not a matrix
// surface and `list` does not name it: no leg but the reference answers it.
int surface_json_text( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "json-text" ) continue;
        if ( f.size() < 5 ) { fprintf( stderr, "driver: a json-text line is <case> <unit> <root> <text>\n" ); return 1; }
        std::vector<uint8_t> text;
        if ( !slurp( f[4].c_str(), text ) ) { fprintf( stderr, "driver: cannot read %s\n", f[4].c_str() ); return 1; }
        const VarCodec * variable = find_var_codec( f[2], f[3] );
        if ( variable != NULL )
        {
            Report report;
            std::vector<uint8_t> bytes;
            if ( !variable->json_read( (const char *) text.data(), (int64_t) text.size(), &report, bytes ) ) return 1;
            std::vector<uint8_t> region;
            const void * root = variable->load( region, bytes.data(), (int64_t) bytes.size(), &report );
            if ( root == NULL ) return 1;
            std::vector<char> written;
            if ( !variable->to_json( root, written ) ) return 1;
            if ( !spill( out, f[1] + ".json", written.data(), written.size() ) ) return 1;
            continue;
        }
        const Codec * codec = find_codec( f[2], f[3] );
        if ( codec == NULL ) { fprintf( stderr, "driver: no codec for %s.%s\n", f[2].c_str(), f[3].c_str() ); return 1; }

        void * value = codec->make();
        codec->reset( value );
        Report report;
        if ( !codec->from_json( value, (const char *) text.data(), (int64_t) text.size(), &report ) ) return 1;
        int64_t size = codec->to_json_measure( value );
        if ( size < 0 ) return 1;
        std::vector<char> written( (size_t) size + 1 );
        if ( codec->to_json( value, written.data(), size ) != size ) return 1;
        if ( !spill( out, f[1] + ".json", written.data(), (size_t) size ) ) return 1;
    }
    return 0;
}

// cook-write: the runtime WRITES a cooked file and the bytes are the tool's
// (docs/SPEC-TABLES.md §7.6), in BOTH byte orders. The value is CONSTRUCTED
// (test/conformance/README.md, "Cook writes use explicit value construction"):
// the derived line is
//   cook-write <instance> <JSON input or -> <source field or -> <alias field or ->
// and `-` as the input means the `no-text` instance's wire. A source and an
// alias name two blobs that must share ONE in-memory object, which no wire
// may carry (§2.5) and a builder may: the cook is where that sharing is legal.
//
// The two answers are written as <instance> and <instance>-be. The big-endian
// half needs no big-endian host: the order is a parameter of the write, settled
// at cook time for the target build, so every leg answers both on any machine.
static bool cook_both( const std::string & out, const std::string & name, int64_t need,
                       bool ( *cook )( const void *, void *, uint64_t, int ), const void * value )
{
    if ( need <= 0 ) return false;
    std::vector<uint8_t> file( (size_t) need );
    for ( int big = 0; big < 2; big++ )
    {
        if ( !cook( value, file.data(), (uint64_t) need, big ) ) return false;
        if ( !spill( out, big ? name + "-be" : name, file.data(), (size_t) need ) ) return false;
    }
    return true;
}

int surface_cook_write( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "cook-write" ) continue;
        if ( f.size() != 5 ) { fprintf( stderr, "driver: a cook-write line has five fields\n" ); return 1; }
        // the line names an INSTANCE; that line carries the unit and the root
        const std::vector<std::string> * inst = NULL;
        for ( size_t j = 0; j < manifest_lines.size(); j++ )
        {
            const std::vector<std::string> & g = manifest_lines[j].field;
            if ( g[0] == "instance" && g[1] == f[1] ) { inst = &manifest_lines[j].field; break; }
        }
        if ( inst == NULL ) { fprintf( stderr, "driver: no instance %s\n", f[1].c_str() ); return 1; }
        const bool from_text = f[2] != "-";
        std::vector<uint8_t> input;
        const std::string & input_path = from_text ? f[2] : ( *inst )[4];
        if ( !slurp( input_path.c_str(), input ) ) { fprintf( stderr, "driver: cannot read %s\n", input_path.c_str() ); return 1; }
        if ( f[3] != "-" || f[4] != "-" )
        {
            if ( !from_text || f[3] == "-" || f[4] == "-" ) { fprintf( stderr, "driver: %s: a sharing recipe names an input and both fields\n", f[1].c_str() ); return 1; }
            if ( !cook_shared( out, f[1], *inst, input, f[3], f[4] ) ) { fprintf( stderr, "driver: %s: the shared cook failed\n", f[1].c_str() ); return 1; }
            continue;
        }

        // a FIXED root is a value; a POINTERED root is a region and a root
        // pointer (§6.2), loaded exactly as the wire surface loads it
        const Codec * codec = find_codec( ( *inst )[2], ( *inst )[3] );
        const VarCodec * var = codec == NULL ? find_var_codec( ( *inst )[2], ( *inst )[3] ) : NULL;
        if ( codec == NULL && var == NULL ) return 2; // this backend writes no cook of this root
        Report report = Report();
        bool ok;
        if ( codec != NULL )
        {
            void * value = codec->make();
            codec->reset( value );
            ok = from_text ? codec->from_json( value, (const char *) input.data(), (int64_t) input.size(), &report )
                           : codec->load( value, input.data(), (int64_t) input.size(), &report );
            ok = ok && !report.refused && cook_both( out, f[1], codec->cook_measure( value ), codec->cook, value );
        }
        else
        {
            std::vector<uint8_t> wire;
            if ( from_text )
            {
                if ( !var->json_read( (const char *) input.data(), (int64_t) input.size(), &report, wire ) ) wire.clear();
            }
            else
            {
                wire = input;
            }
            std::vector<uint8_t> region;
            const void * value = wire.empty() ? NULL : var->load( region, wire.data(), (int64_t) wire.size(), &report );
            ok = value != NULL && !report.refused && cook_both( out, f[1], var->cook_measure( value ), var->cook, value );
        }
        if ( !ok ) { fprintf( stderr, "driver: %s: the cook input did not read or did not cook\n", f[1].c_str() ); return 1; }
    }
    return 0;
}

// json-hostile: one tree per rule the text form states (§16.2, §16.3, §17.5).
// The answer is the REPORT the read produces, then the §16 text of the value it
// left, or `refused` — the same verdict the engine's own gate holds, over the
// same data, and the value the oracle's read leaves.
int surface_json_hostile( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "json-hostile" ) continue;
        std::vector<uint8_t> text;
        // the tree is what `schema pack` reads, so the text is <tree>/<root>.json (§17)
        const std::string path = f[4] + "/" + f[3] + ".json";
        if ( !slurp( path.c_str(), text ) ) { fprintf( stderr, "driver: cannot read %s\n", path.c_str() ); return 1; }

        Report report = Report();
        bool ok = false;
        std::vector<char> value_text;
        std::vector<uint8_t> saved;
        const VarCodec * variable = find_var_codec( f[2], f[3] );
        if ( variable != NULL )
        {
            // the builder's value reaches its text through its own saved bytes,
            // which are the write-back wire, and the region read takes them back
            ok = variable->json_read( (const char *) text.data(), (int64_t) text.size(), &report, saved ) && !report.refused;
            if ( ok )
            {
                std::vector<uint8_t> region;
                Report again;
                const void * root = variable->load( region, saved.data(), (int64_t) saved.size(), &again );
                if ( root == NULL || !variable->to_json( root, value_text ) )
                {
                    // A READ THE LEG CANNOT WRITE BACK is this case's wrong
                    // answer, not the surface's: a text past the depth cap that
                    // the read let through meets the writer's cap here. Say so
                    // in the value, and the case fails by name while every
                    // other row is still scored.
                    static const char unwritable[] = "(read, and the value it left could not be written back)\n";
                    value_text.assign( unwritable, unwritable + sizeof( unwritable ) - 1 );
                }
            }
        }
        else
        {
            const Codec * codec = find_codec( f[2], f[3] );
            if ( codec == NULL ) { fprintf( stderr, "driver: no codec for %s.%s\n", f[2].c_str(), f[3].c_str() ); return 1; }
            void * value = codec->make();
            codec->reset( value );
            ok = codec->from_json( value, (const char *) text.data(), (int64_t) text.size(), &report ) && !report.refused;
            if ( ok )
            {
                if ( !codec_text( codec, value, value_text ) ) return 1;
                const int64_t size = codec->measure( value );
                if ( size < 0 ) return 1;
                saved.assign( (size_t) size, 0 );
                if ( codec->save( value, saved.data(), size ) != size ) return 1;
            }
        }
        std::vector<char> answer = verdict_line( report, ok );
        if ( ok )
        {
            answer.insert( answer.end(), value_text.begin(), value_text.end() );
            if ( !spill( out, f[1] + ".wire", saved.data(), saved.size() ) ) return 1;
        }
        if ( !spill( out, f[1], answer.data(), answer.size() ) ) return 1;
    }
    return 0;
}

// announce <case> <unit> <root> <mode> <input>
int surface_announce( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "announce" ) continue;
        if ( f.size() < 6 ) { fprintf( stderr, "driver: short announce line\n" ); return 1; }
        const std::string & mode = f[4];
        std::vector<uint8_t> answer;
        const char * verdict = NULL;
        if ( mode == "write" || mode == "strict" || mode == "lenient" )
        {
            const AnnounceUnit * au = find_announce_unit( f[2] );
            if ( au == NULL ) { fprintf( stderr, "driver: announce %s: no announcement for unit %s\n", f[1].c_str(), f[2].c_str() ); return 1; }
            if ( mode == "write" )
            {
                const int64_t size = au->measure();
                if ( size < 0 ) verdict = "refused\n";
                else
                {
                    answer.assign( (size_t) size, 0 );
                    const int64_t wrote = au->write( answer.data(), size );
                    if ( wrote < 0 ) verdict = "refused\n";
                    else answer.resize( (size_t) wrote );
                }
            }
            else
            {
                std::vector<uint8_t> input;
                if ( !slurp( f[5].c_str(), input ) ) { fprintf( stderr, "driver: cannot read %s\n", f[5].c_str() ); return 1; }
                verdict = au->read( input.data(), (int64_t) input.size(), mode == "strict" ) ? "read\n" : "refused\n";
            }
        }
        else if ( mode == "send" )
        {
            // SaveMessages as a batch of one, with NO announcement read or
            // written on this connection: the writer's own vocabulary (FL-18)
            const Codec * codec = find_codec( f[2], f[3] );
            const MsgCodec * msg = find_msg_codec( f[2], f[3] );
            if ( codec == NULL || msg == NULL ) { fprintf( stderr, "driver: announce %s: no message codec for %s.%s\n", f[1].c_str(), f[2].c_str(), f[3].c_str() ); return 1; }
            void * value = codec->make();
            codec->reset( value );
            if ( f[5] != "-" )
            {
                std::vector<uint8_t> wire;
                Report report;
                if ( !slurp( f[5].c_str(), wire ) ) { fprintf( stderr, "driver: cannot read %s\n", f[5].c_str() ); return 1; }
                if ( !codec->load( value, wire.data(), (int64_t) wire.size(), &report ) ) { fprintf( stderr, "driver: announce %s: the row's wire did not load\n", f[1].c_str() ); return 1; }
            }
            if ( !msg->send( value, answer ) ) verdict = "refused\n";
        }
        else
        {
            fprintf( stderr, "driver: announce %s: no mode %s\n", f[1].c_str(), mode.c_str() );
            return 1;
        }
        if ( verdict != NULL ) { if ( !spill( out, f[1], verdict, strlen( verdict ) ) ) return 1; }
        else if ( !spill( out, f[1], answer.data(), answer.size() ) ) return 1;
    }
    return 0;
}

// read-into <case> <unit> <root> <form> <lawful> <damaged> <count or -> <announcement or ->
int surface_read_into( const std::string & out )
{
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "read-into" ) continue;
        if ( f.size() < 9 ) { fprintf( stderr, "driver: short read-into line\n" ); return 1; }
        if ( f[4] == "fixed-nocap" )
        {
            // NO CALL TO HOLD (FL-17): C++'s FixedLoad takes its capacity as a
            // parameter with no default and no overload without it, so no
            // omitted capacity can be asked of it
            const char * none = "unrepresentable\n";
            if ( !spill( out, f[1], none, strlen( none ) ) ) return 1;
            continue;
        }
        if ( f[4] == "retain-fixed" )
        {
            // NO CALL TO HOLD (docs/SPEC-TABLES.md §6.6): C++ refuses
            // <Root>LoadRetain on a FIXED-class root at COMPILE time. The
            // generator declares it as a variadic function template whose
            // body is a static_assert that always fails
            // (internal/codegen/cpptable/retain.go:1686, emitRetainRefusals),
            // so surfaces::NarrowLoadRetain( ... ) does not compile and no
            // call can be handed a report
            const char * none = "unrepresentable\n";
            if ( !spill( out, f[1], none, strlen( none ) ) ) return 1;
            continue;
        }
        const ReadIntoCodec * rc = find_read_into_codec( f[2], f[3] );
        if ( rc == NULL ) { fprintf( stderr, "driver: read-into %s: no codec for %s.%s\n", f[1].c_str(), f[2].c_str(), f[3].c_str() ); return 1; }
        std::vector<uint8_t> lawful, damaged, announcement;
        if ( !slurp( f[5].c_str(), lawful ) ) { fprintf( stderr, "driver: cannot read %s\n", f[5].c_str() ); return 1; }
        if ( !slurp( f[6].c_str(), damaged ) ) { fprintf( stderr, "driver: cannot read %s\n", f[6].c_str() ); return 1; }
        if ( f[8] != "-" && !slurp( f[8].c_str(), announcement ) ) { fprintf( stderr, "driver: cannot read %s\n", f[8].c_str() ); return 1; }
        char * end = NULL;
        const long long count = strtoll( f[7].c_str(), &end, 10 );
        if ( end == NULL || *end != 0 || count < 1 ) { fprintf( stderr, "driver: read-into %s: %s is no count\n", f[1].c_str(), f[7].c_str() ); return 1; }
        std::string answer;
        // the `region` form is the plain region read, which the builder codec
        // above does not make: tblsurf.Counted's is answered here
        const int ran = f[4] == "region" && f[2] == "tblsurf" && f[3] == "Counted"
                            ? read_into_region( lawful, damaged, (int64_t) count, answer )
                            : rc->run( f[4], lawful, damaged, (int64_t) count, announcement, answer );
        if ( ran == 1 ) { fprintf( stderr, "driver: read-into %s: no %s form for %s.%s\n", f[1].c_str(), f[4].c_str(), f[2].c_str(), f[3].c_str() ); return 1; }
        if ( ran == 2 ) { fprintf( stderr, "driver: read-into %s: unanswered\n", f[1].c_str() ); continue; }
        if ( !spill( out, f[1], answer.data(), answer.size() ) ) return 1;
    }
    return 0;
}

// ---------------------------------------------------------------------------
// THE TEXT WRITER IN A COMMA-DECIMAL LOCALE, INTO A CALLER BUFFER (FL-14,
// FL-15; README, `json-write-into`)
// ---------------------------------------------------------------------------
// json-write-into <case> <unit> <root> <wire>
int surface_json_write_into( const std::string & out )
{
    const char * name = getenv( "SCHEMA_COMMA_LOCALE" );
    if ( name == NULL || name[0] == 0 ) { fprintf( stderr, "driver: json-write-into: SCHEMA_COMMA_LOCALE is not set\n" ); return 1; }
    if ( setlocale( LC_ALL, name ) == NULL ) { fprintf( stderr, "driver: json-write-into: setlocale( LC_ALL, %s ) was refused\n", name ); return 1; }
    const struct lconv * conv = localeconv();
    if ( conv == NULL || conv->decimal_point == NULL || strcmp( conv->decimal_point, "," ) != 0 )
    {
        // a locale rule checked under a dot locale checks nothing
        fprintf( stderr, "driver: json-write-into: %s is set but its decimal point is not a comma\n", name );
        return 1;
    }
    for ( size_t i = 0; i < manifest_lines.size(); i++ )
    {
        const std::vector<std::string> & f = manifest_lines[i].field;
        if ( f[0] != "json-write-into" ) continue;
        if ( f.size() < 5 ) { fprintf( stderr, "driver: short json-write-into line\n" ); return 1; }
        const Codec * codec = find_codec( f[2], f[3] );
        if ( codec == NULL ) { fprintf( stderr, "driver: json-write-into %s: no codec for %s.%s\n", f[1].c_str(), f[2].c_str(), f[3].c_str() ); return 1; }
        std::vector<uint8_t> wire;
        if ( !slurp( f[4].c_str(), wire ) ) { fprintf( stderr, "driver: cannot read %s\n", f[4].c_str() ); return 1; }
        void * value = codec->make();
        codec->reset( value );
        Report report;
        if ( !codec->load( value, wire.data(), (int64_t) wire.size(), &report ) ) { fprintf( stderr, "driver: json-write-into %s: the wire did not load\n", f[1].c_str() ); return 1; }
        const int64_t size = codec->to_json_measure( value );
        if ( size < 0 ) { fprintf( stderr, "driver: json-write-into %s: ToJsonMeasure refused; no answer\n", f[1].c_str() ); continue; }
        const int64_t capacity = size + 64;
        std::vector<char> buffer( (size_t) capacity, (char) 0xA5 );
        const int64_t wrote = codec->to_json( value, buffer.data(), capacity );
        if ( wrote < 0 || wrote > capacity ) { fprintf( stderr, "driver: json-write-into %s: ToJson answered %lld; no answer\n", f[1].c_str(), (long long) wrote ); continue; }
        bool tail = false;
        for ( int64_t k = wrote; k < capacity; k++ ) tail = tail || (uint8_t) buffer[(size_t) k] != 0xA5;
        if ( tail )
        {
            const char * written = "tail-written\n";
            if ( !spill( out, f[1], written, strlen( written ) ) ) return 1;
        }
        else if ( !spill( out, f[1], buffer.data(), (size_t) wrote ) ) return 1;
    }
    setlocale( LC_ALL, "C" );
    return 0;
}

