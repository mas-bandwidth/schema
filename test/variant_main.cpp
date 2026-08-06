// The opt-in variant dispatch surface (--cpp-message variant), compiled
// against its own generated directory: proves the second representation
// builds, runs, and speaks the same wire. The default (tagged union) is
// exercised by main.cpp.

#include <cstdio>
#include <cstring>
#include <type_traits>

#include "Messages.h"

#define check( condition )                                                    \
    do                                                                        \
    {                                                                         \
        if ( !( condition ) )                                                 \
        {                                                                     \
            printf( "FAILED: %s (%s:%d)\n", #condition, __FILE__, __LINE__ ); \
            return 1;                                                         \
        }                                                                     \
    } while ( 0 )

int main()
{
    using namespace example;

    static_assert(std::is_trivially_copyable<Message>::value,
                  "every message is trivially copyable, so the variant is too — no heap anywhere");
    static_assert(std::variant_size_v<Message> == 7, "one alternative per message plus None");

    alignas( 8 ) uint8_t buffer[4096];

    Message stream_out[3];
    Chat chat;
    std::memcpy( chat.text, "dispatch", 8 );
    chat.text_length = 8;
    stream_out[0] = chat;
    Test t;
    t.test_b = 42;
    stream_out[1] = t;
    stream_out[2] = std::monostate{}; // None terminates the stream (SPEC §4.8)

    serialize::WriteStream ws( buffer, sizeof( buffer ) );
    for ( const Message & m : stream_out )
        check( WriteMessage( ws, m ) );
    ws.Flush();

    // cross-representation wire identity: the variant surface must produce
    // exactly the bytes the union surface pinned (SPEC §7.2 gate 7)
    {
        FILE * f = fopen( "testdata/wire/message_stream.bin", "rb" );
        check( f != nullptr ); // run: make update-goldens
        static uint8_t expected[4096];
        size_t n = fread( expected, 1, sizeof( expected ), f );
        fclose( f );
        check( (int64_t) n == ws.GetBytesProcessed() );
        check( std::memcmp( expected, buffer, n ) == 0 );
    }

    serialize::ReadStream rs( buffer, ws.GetBytesProcessed() );
    Message in;
    check( ReadMessage( rs, in ) );
    check( GetMessageType( in ) == MessageType::Chat );
    check( std::get_if<Chat>( &in ) && std::get_if<Chat>( &in )->text_length == 8 );
    check( std::strcmp( std::get_if<Chat>( &in )->text, "dispatch" ) == 0 );
    check( ReadMessage( rs, in ) );
    check( GetMessageType( in ) == MessageType::Test );
    check( std::get_if<Test>( &in ) && std::get_if<Test>( &in )->test_b == 42 );
    check( ReadMessage( rs, in ) );
    check( GetMessageType( in ) == MessageType::None ); // the terminator

    printf( "OK\n" );
    return 0;
}
