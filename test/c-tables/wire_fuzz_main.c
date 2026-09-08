/* The C leg of the independent table-wire differential. The protocol and
   oracle live in test/conformance/harness; this process only loads and saves. */
#include "driver.h"
#if defined(_WIN32)
#include <io.h>
#include <fcntl.h>
#endif

static int read_number( uint64_t * value, int bytes )
{
    int i; *value = 0;
    for ( i = 0; i < bytes; i++ ) { int b = getchar(); if ( b == EOF ) { return 0; } *value |= (uint64_t) b << (8*i); }
    return 1;
}
static void write_number( uint64_t value, int bytes )
{
    int i; for ( i = 0; i < bytes; i++ ) { putchar( (int) ((value >> (8*i)) & 255) ); }
}
static int read_name( char * name, size_t capacity )
{
    uint64_t n;
    if ( !read_number( &n, 2 ) || n >= capacity || fread( name, 1, (size_t) n, stdin ) != n ) { return 0; }
    name[n] = 0; return 1;
}
typedef const ConformanceCodec * (*Unit)( int * count );
static const ConformanceCodec * find_codec( const char * unit, const char * root )
{
#if defined(SCHEMA_C_COLLECTIONS_FUZZ)
    const Unit units[] = { conformance_codecs_mapdemo, conformance_codecs_listdemo, conformance_codecs_armdemo };
#else
    const Unit units[] = { conformance_codecs_mapdemo, conformance_codecs_listdemo, conformance_codecs_armdemo, conformance_codecs_tblw1, conformance_codecs_tblw2, conformance_codecs_vocab9demo, conformance_codecs_vocabdemo, conformance_codecs_backenddemo, conformance_codecs_tblr2, conformance_codecs_tblr1, conformance_codecs_tblk2, conformance_codecs_tblk1, conformance_codecs_tbla2, conformance_codecs_tbla1, conformance_codecs_tblm2, conformance_codecs_tblm1, conformance_codecs_messagedemo, conformance_codecs_tabledemo, conformance_codecs_tblv1,
        conformance_codecs_tblv2, conformance_codecs_tblp1, conformance_codecs_tblp3, conformance_codecs_widedemo, conformance_codecs_scalars, conformance_codecs_graphdemo, conformance_codecs_blobdemo, conformance_codecs_tblg1, conformance_codecs_tblp2, conformance_codecs_streamdemo, conformance_codecs_tblscalars2 };
#endif
    size_t u;
    for ( u = 0; u < sizeof( units ) / sizeof( units[0] ); u++ ) {
        int i, count; const ConformanceCodec * codecs = units[u]( &count );
        for ( i = 0; i < count; i++ ) {
            if ( strcmp( codecs[i].unit, unit ) == 0 && strcmp( codecs[i].root, root ) == 0 ) { return &codecs[i]; }
        }
    }
    return NULL;
}
int main( int argc, char ** argv )
{
    uint64_t count, i, index, size;
    int cook=argc>1 && strncmp(argv[1],"--cook-",7)==0;
    int big=cook && strcmp(argv[1],"--cook-be")==0;
    const ConformanceCodec ** roster;
    uint8_t * forms;
    #if defined(_WIN32)
    _setmode( _fileno( stdin ), _O_BINARY );
    _setmode( _fileno( stdout ), _O_BINARY );
    #endif
    if ( !read_number( &count, 4 ) || count > 4096 ) { return 1; }
    roster = (const ConformanceCodec **) calloc( (size_t) count, sizeof( *roster ) );
    forms=(uint8_t *)calloc((size_t)count,1);
    if ( roster == NULL || forms==NULL ) { return 1; }
    for ( i = 0; i < count; i++ ) {
        char unit[256], root[256]; uint64_t form, retain;
        if ( !read_name( unit, sizeof( unit ) ) || !read_name( root, sizeof( root ) ) ||
             !read_number( &form, 1 ) || !read_number( &retain, 1 ) ) { return 1; }
        forms[i]=(uint8_t)(retain?3:form);
        if ( (form == 1 || (form==2&&!cook))  ) { roster[i] = find_codec( unit, root ); }
        if(form==2 && roster[i] && !roster[i]->message_fuzz)roster[i]=NULL;
        if(retain && roster[i] && !roster[i]->retain_fuzz)roster[i]=NULL;
        putchar( roster[i] != NULL );
    }
    fflush( stdout );
    while ( read_number( &index, 4 ) ) {
        const ConformanceCodec * codec;
        uint8_t * wire, * saved = NULL; void * value; int loaded; int64_t length, region_bytes;
        ConformanceReport report;
        if ( index >= count || roster[index] == NULL || !read_number( &size, 4 ) ) { return 1; }
        codec = roster[index];
        wire = (uint8_t *) malloc( (size_t) (size ? size : 1) );
        if ( wire == NULL || fread( wire, 1, (size_t) size, stdin ) != size ) { return 1; }
        memset( &report, 0, sizeof( report ) );
        if(forms[index]==3){length=codec->retain_fuzz(wire,(int64_t)size,&saved,&report,&loaded,&region_bytes);free(wire);goto reply;}
        if(forms[index]==2){length=codec->message_fuzz(wire,(int64_t)size,&saved,&report,&loaded,&region_bytes);free(wire);goto reply;}
        value = codec->storage();
        region_bytes=codec->load_measure ? codec->load_measure(wire,(int64_t)size) : -1;
        loaded = codec->load( value, wire, (int64_t) size, &report ); free( wire );
        length = cook ? (loaded ? codec->cook_measure(value) : -1) : codec->measure( value );
        if(cook) { region_bytes=length; }
        if ( length >= 0 ) {
            saved = (uint8_t *) malloc( (size_t) (length ? length : 1) );
            if ( saved == NULL ) { return 1; }
            if(cook) { if(!codec->cook(value,saved,(uint64_t)length,big)) { length=-1; } }
            else { length = codec->save( value, saved, length ); }
        }
        /* Fixed storage always exists; load's bool is the body verdict. */
        if ( !loaded && !report.refused ) { report.malformed = 1; }
        reply:
        write_number( codec->load_measure ? (uint64_t)loaded : 1, 1 );
        write_number( (uint64_t) report.unknown, 4 ); write_number( (uint64_t) report.kind_mismatch, 4 );
        write_number( (uint64_t) report.widened, 4 ); write_number( (uint64_t) report.clamped, 4 );
        write_number( (uint64_t) report.duplicate, 4 ); write_number( (uint64_t) report.malformed, 1 );
        write_number( (uint64_t) report.refused, 1 ); write_number( (uint64_t)region_bytes, 8 );
        write_number( (uint64_t)report.retained, 4 ); write_number( (uint64_t)report.retain_lost, 4 ); write_number( (uint64_t) length, 8 );
        if ( length > 0 && fwrite( saved, 1, (size_t) length, stdout ) != (size_t) length ) { return 1; }
        free( saved ); if ( fflush( stdout ) != 0 ) { return 1; }
    }
    free(forms); free( roster ); return ferror( stdin ) ? 1 : 0;
}
