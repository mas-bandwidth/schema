/* THE C CONFORMANCE DRIVER (test/conformance/README.md).
 *
 * One process per surface. The harness hands it the derived manifest, the
 * surface name and an output directory; the driver writes one file per case and
 * says nothing else. Every expectation lives in the DATA — this file holds no
 * literal instance, no expected byte and no expected count.
 *
 *   driver <manifest> list
 *   driver <manifest> <surface> <outdir>
 *
 * Exit 0 means the surface ran. Exit 2 means this backend does not implement
 * it, which the matrix prints as ABSENT rather than as a failure.
 *
 * THIS FILE INCLUDES NO GENERATED HEADER. C has no namespaces, so the seven
 * units the corpus names cannot meet in one translation unit; each has one of
 * its own beside this file and reaches it through driver.h's erased shapes. */

#include "driver.h"

/* ---------------------------------------------------------------------------
   the manifest, read exactly as testdata/conformance/tables/FORMAT.md states it
   --------------------------------------------------------------------------- */

#define MaxFields 16
#define MaxLines 4096

typedef struct Line
{
    int count;
    char * field[MaxFields];
} Line;

static Line lines[MaxLines];
static int num_lines;

static int read_manifest( const char * path )
{
    FILE * file = fopen( path, "r" );
    char * text = NULL;
    size_t length = 0, capacity = 0;
    int c;
    size_t i = 0;
    if ( file == NULL )
    {
        fprintf( stderr, "driver: cannot open %s\n", path );
        return 0;
    }
    while ( ( c = fgetc( file ) ) != EOF )
    {
        if ( length + 2 > capacity )
        {
            capacity = capacity ? capacity * 2 : 8192;
            text = (char *) realloc( text, capacity );
            if ( text == NULL ) { fclose( file ); return 0; }
        }
        text[length++] = (char) c;
    }
    fclose( file );
    if ( text == NULL ) { return 1; }
    text[length] = 0;
    while ( i < length && num_lines < MaxLines )
    {
        size_t start = i;
        size_t end;
        Line * line;
        while ( i < length && text[i] != '\n' ) { i++; }
        end = i;
        if ( i < length ) { i++; }
        text[end] = 0;
        if ( end == start || text[start] == '#' ) { continue; }
        line = &lines[num_lines];
        line->count = 0;
        {
            size_t p = start;
            while ( p < end && line->count < MaxFields )
            {
                size_t token;
                while ( p < end && ( text[p] == ' ' || text[p] == '\t' || text[p] == '\r' ) ) { p++; }
                token = p;
                while ( p < end && text[p] != ' ' && text[p] != '\t' && text[p] != '\r' ) { p++; }
                if ( p > token )
                {
                    line->field[line->count++] = text + token;
                    if ( p < end ) { text[p] = 0; p++; }
                }
            }
        }
        if ( line->count > 0 && line->field[0][0] != '#' ) { num_lines++; }
    }
    return 1;
}

/* --------------------------------------------------------------------------- */

static uint8_t * slurp( const char * path, size_t * bytes )
{
    FILE * file = fopen( path, "rb" );
    long size;
    uint8_t * out;
    if ( file == NULL ) { return NULL; }
    fseek( file, 0, SEEK_END );
    size = ftell( file );
    fseek( file, 0, SEEK_SET );
    out = (uint8_t *) malloc( (size_t) ( size > 0 ? size : 1 ) );
    if ( out == NULL ) { fclose( file ); return NULL; }
    if ( size > 0 && fread( out, 1, (size_t) size, file ) != (size_t) size )
    {
        fclose( file );
        free( out );
        return NULL;
    }
    fclose( file );
    *bytes = (size_t) size;
    return out;
}

/* spill_absent says this backend cannot answer THIS CASE — a feature it lacks,
   not a test it failed. The harness counts it and the matrix prints it beside
   what the leg did answer (test/conformance/README.md). */
static int spill_absent( const char * dir, const char * name );

/* no_text marks an instance the corpus carries on the WIRE only — past the text
   form's depth cap by the form's own rule (docs/SPEC-TABLES.md 16.7) — so no
   leg is asked for its text. */
static int no_text( const Line * f )
{
    return f->count > 5 && strcmp( f->field[5], "no-text" ) == 0;
}

static int spill( const char * dir, const char * name, const void * data, size_t bytes )
{
    char path[1024];
    FILE * file;
    int ok;
    snprintf( path, sizeof( path ), "%s/%s", dir, name );
    file = fopen( path, "wb" );
    if ( file == NULL )
    {
        fprintf( stderr, "driver: cannot write %s\n", path );
        return 0;
    }
    ok = bytes == 0 || fwrite( data, 1, bytes, file ) == bytes;
    fclose( file );
    return ok;
}

/* --------------------------------------------------------------------------- */

typedef const ConformanceCodec * ( *UnitFn )( int * count );

static const UnitFn units[] = {
    conformance_codecs_messagedemo,
    conformance_codecs_tblm1,
    conformance_codecs_tblm2,
    conformance_codecs_tbla1,
    conformance_codecs_tbla2,
    conformance_codecs_tblk1,
    conformance_codecs_tblk2,
    conformance_codecs_tblr1,
    conformance_codecs_tblr2,
    conformance_codecs_backenddemo,
    conformance_codecs_vocabdemo,
    conformance_codecs_vocab9demo,
    conformance_codecs_tabledemo,
    conformance_codecs_tblv1,
    conformance_codecs_tblv2,
    conformance_codecs_tblp1,
    conformance_codecs_tblp3,
    conformance_codecs_widedemo,
    conformance_codecs_scalars,
    conformance_codecs_graphdemo, conformance_codecs_blobdemo, conformance_codecs_tblg1, conformance_codecs_tblp2, conformance_codecs_streamdemo, conformance_codecs_tblscalars2
};

static const ConformanceCodec * find_codec( const char * unit, const char * root )
{
    size_t u;
    for ( u = 0; u < sizeof( units ) / sizeof( units[0] ); u++ )
    {
        int count = 0;
        const ConformanceCodec * table = units[u]( &count );
        int i;
        for ( i = 0; i < count; i++ )
        {
            if ( strcmp( table[i].unit, unit ) == 0 && strcmp( table[i].root, root ) == 0 )
            {
                return &table[i];
            }
        }
    }
    return NULL;
}

/* ---------------------------------------------------------------------------
   the surfaces
   --------------------------------------------------------------------------- */

static int save_into( const ConformanceCodec * codec, void * value, const char * out, const char * name )
{
    int64_t size = codec->measure( value );
    uint8_t * scratch;
    int ok;
    if ( size < 0 ) { return 0; }
    scratch = (uint8_t *) malloc( (size_t) ( size > 0 ? size : 1 ) );
    if ( scratch == NULL ) { return 0; }
    if ( codec->save( value, scratch, size ) != size ) { free( scratch ); return 0; }
    ok = spill( out, name, scratch, (size_t) size );
    free( scratch );
    return ok;
}

static int surface_wire( const char * out )
{
    int i;
    for ( i = 0; i < num_lines; i++ )
    {
        const Line * f = &lines[i];
        const ConformanceCodec * codec;
        uint8_t * wire;
        size_t bytes = 0;
        void * value;
        ConformanceReport report;
        if ( strcmp( f->field[0], "instance" ) != 0 ) { continue; }
        codec = find_codec( f->field[2], f->field[3] );
        /* the C backend writes the earlier NESTED wire for a pointered unit
           (SPEC-TABLES 3.1's backend status), so it has no codec here and says
           so per case rather than failing the surface */
        if ( codec == NULL ) { if ( !spill_absent( out, f->field[1] ) ) { return 1; } continue; }
        wire = slurp( f->field[4], &bytes );
        if ( wire == NULL ) { fprintf( stderr, "driver: cannot read %s\n", f->field[4] ); return 1; }
        value = codec->storage();
        memset( &report, 0, sizeof( report ) );
        if ( !codec->load( value, wire, (int64_t) bytes, &report ) ) { free( wire ); return 1; }
        free( wire );
        if ( !save_into( codec, value, out, f->field[1] ) ) { return 1; }
    }
    return 0;
}

static int spill_absent( const char * dir, const char * name )
{
    char marker[1024];
    snprintf( marker, sizeof( marker ), "%s.absent", name );
    return spill( dir, marker, "", 0 );
}

static int surface_report( const char * out )
{
    int i;
    for ( i = 0; i < num_lines; i++ )
    {
        const Line * f = &lines[i];
        const ConformanceCodec * codec;
        uint8_t * wire;
        size_t bytes = 0;
        void * value;
        ConformanceReport report;
        char text[128];
        int n;
        if ( strcmp( f->field[0], "report" ) != 0 ) { continue; }
        codec = find_codec( f->field[2], f->field[3] );
        if ( codec == NULL ) { if ( !spill_absent( out, f->field[1] ) ) { return 1; } continue; }
        wire = slurp( f->field[4], &bytes );
        if ( wire == NULL ) { fprintf( stderr, "driver: cannot read %s\n", f->field[4] ); return 1; }
        value = codec->storage();
        memset( &report, 0, sizeof( report ) );
        codec->load( value, wire, (int64_t) bytes, &report );
        free( wire );
        n = snprintf( text, sizeof( text ), "%d,%d,%d,%d,%d,%s,%s\n",
                      report.unknown, report.kind_mismatch, report.widened, report.clamped, report.duplicate,
                      report.malformed ? "true" : "false", report.refused ? "refused" : "read" );
        if ( !spill( out, f->field[1], text, (size_t) n ) ) { return 1; }
    }
    return 0;
}

static int surface_json_read( const char * out )
{
    int i;
    for ( i = 0; i < num_lines; i++ )
    {
        const Line * f = &lines[i];
        const ConformanceCodec * codec;
        char path[1024];
        uint8_t * text;
        size_t bytes = 0;
        void * value;
        ConformanceReport report;
        if ( strcmp( f->field[0], "instance" ) != 0 || no_text( f ) ) { continue; }
        codec = find_codec( f->field[2], f->field[3] );
        /* the C port carries no text form for a pointered unit (16.7), and says so per case */
        if ( codec == NULL || codec->from_json == NULL ) { if ( !spill_absent( out, f->field[1] ) ) { return 1; } continue; }
        snprintf( path, sizeof( path ), "testdata/conformance/tables/json/%s.json", f->field[1] );
        text = slurp( path, &bytes );
        if ( text == NULL ) { fprintf( stderr, "driver: cannot read %s\n", path ); return 1; }
        value = codec->storage();
        memset( &report, 0, sizeof( report ) );
        if ( !codec->from_json( value, (const char *) text, (int64_t) bytes, &report ) ) { free( text ); return 1; }
        free( text );
        if ( !save_into( codec, value, out, f->field[1] ) ) { return 1; }
    }
    return 0;
}

static int surface_json_write( const char * out )
{
    int i;
    for ( i = 0; i < num_lines; i++ )
    {
        const Line * f = &lines[i];
        const ConformanceCodec * codec;
        uint8_t * wire;
        size_t bytes = 0;
        void * value;
        ConformanceReport report;
        int64_t size;
        char * text;
        char name[512];
        int ok;
        if ( strcmp( f->field[0], "instance" ) != 0 || no_text( f ) ) { continue; }
        codec = find_codec( f->field[2], f->field[3] );
        snprintf( name, sizeof( name ), "%s.json", f->field[1] );
        if ( codec == NULL || codec->to_json == NULL ) { if ( !spill_absent( out, name ) ) { return 1; } continue; }
        wire = slurp( f->field[4], &bytes );
        if ( wire == NULL ) { fprintf( stderr, "driver: cannot read %s\n", f->field[4] ); return 1; }
        value = codec->storage();
        memset( &report, 0, sizeof( report ) );
        if ( !codec->load( value, wire, (int64_t) bytes, &report ) ) { free( wire ); return 1; }
        free( wire );
        size = codec->to_json( value, NULL, 0 );
        if ( size < 0 ) { return 1; }
        text = (char *) malloc( (size_t) size + 1 );
        if ( text == NULL ) { return 1; }
        if ( codec->to_json( value, text, size ) != size ) { free( text ); return 1; }
        snprintf( name, sizeof( name ), "%s.json", f->field[1] );
        ok = spill( out, name, text, (size_t) size );
        free( text );
        if ( !ok ) { return 1; }
    }
    return 0;
}

/* json-hostile: one tree per rule the text form states (§16.2, §16.3, §17.5).
   The answer is the REPORT the read produces, or `refused`. */
static int surface_json_hostile( const char * out )
{
    int i;
    for ( i = 0; i < num_lines; i++ )
    {
        const Line * f = &lines[i];
        const ConformanceCodec * codec;
        char path[1024];
        uint8_t * text;
        size_t bytes = 0;
        void * value;
        ConformanceReport report;
        char verdict[128];
        int n, ok;
        if ( strcmp( f->field[0], "json-hostile" ) != 0 ) { continue; }
        codec = find_codec( f->field[2], f->field[3] );
        if ( codec == NULL || codec->from_json == NULL ) { if ( !spill_absent( out, f->field[1] ) ) { return 1; } continue; }
        /* the tree is what `schema pack` reads, so the text is <tree>/<root>.json (§17) */
        snprintf( path, sizeof( path ), "%s/%s.json", f->field[4], f->field[3] );
        text = slurp( path, &bytes );
        if ( text == NULL ) { fprintf( stderr, "driver: cannot read %s\n", path ); return 1; }
        value = codec->storage();
        memset( &report, 0, sizeof( report ) );
        ok = codec->from_json( value, (const char *) text, (int64_t) bytes, &report );
        free( text );
        if ( !ok || report.malformed )
        {
            n = snprintf( verdict, sizeof( verdict ), "refused\n" );
        }
        else
        {
            n = snprintf( verdict, sizeof( verdict ), "%d,%d,%d,%d,%d,false,read\n",
                          report.unknown, report.kind_mismatch, report.widened, report.clamped, report.duplicate );
        }
        if ( !spill( out, f->field[1], verdict, (size_t) n ) ) { return 1; }
    }
    return 0;
}

static int surface_block( const char * out )
{
    int i;
    for ( i = 0; i < num_lines; i++ )
    {
        const Line * f = &lines[i];
        uint8_t * data;
        size_t bytes = 0;
        const char * verdict;
        if ( strcmp( f->field[0], "block" ) != 0 ) { continue; }
        data = slurp( f->field[3], &bytes );
        if ( data == NULL ) { fprintf( stderr, "driver: cannot read %s\n", f->field[3] ); return 1; }
        verdict = conformance_block_open( f->field[1], data, bytes, -1, 0 ) ? "open\n" : "refuse\n";
        free( data );
        if ( !spill( out, f->field[1], verdict, strlen( verdict ) ) ) { return 1; }
    }
    return 0;
}

static int surface_block_dump( const char * out )
{
    int i;
    for ( i = 0; i < num_lines; i++ )
    {
        const Line * f = &lines[i];
        uint8_t * data;
        size_t bytes = 0;
        ConformanceText text;
        int ok;
        if ( strcmp( f->field[0], "block" ) != 0 ) { continue; }
        data = slurp( f->field[3], &bytes );
        if ( data == NULL ) { fprintf( stderr, "driver: cannot read %s\n", f->field[3] ); return 1; }
        memset( &text, 0, sizeof( text ) );
        ok = conformance_block_dump( f->field[1], data, bytes, &text );
        free( data );
        if ( !ok ) { free( text.data ); return 1; }
        ok = spill( out, f->field[1], text.data, text.length );
        free( text.data );
        if ( !ok ) { return 1; }
    }
    return 0;
}

/* THE TWO FOREIGN SURFACES: the cross-endian refusal, and the one answer a leg
   can give on ANY host (test/conformance/README.md).
 
   A block and a cook are produced in the byte order of the build that wrote
   them (§19.1, §7), so a reader of the other order must REFUSE — and the check
   that does it is the MAGIC, read first and bytewise for exactly this reason.
   Reversing the magic's eight bytes is what that word looks like to a reader of
   the other order, so it makes the file foreign to WHOEVER READS IT rather than
   to a particular host: whatever this build's order is, the magic it now reads
   is not this build's. That is the only shape a cross-endian expectation can
   take without depending on the host it runs on. */
static void conformance_foreign( uint8_t * data, size_t bytes )
{
    int i;
    if ( bytes < 8 ) { return; }
    for ( i = 0; i < 4; i++ )
    {
        uint8_t t = data[i];
        data[i] = data[7 - i];
        data[7 - i] = t;
    }
}

static int surface_block_foreign( const char * out )
{
    int i;
    for ( i = 0; i < num_lines; i++ )
    {
        const Line * f = &lines[i];
        uint8_t * data;
        size_t bytes = 0;
        const char * verdict;
        if ( strcmp( f->field[0], "block" ) != 0 ) { continue; }
        data = slurp( f->field[3], &bytes );
        if ( data == NULL ) { fprintf( stderr, "driver: cannot read %s\n", f->field[3] ); return 1; }
        conformance_foreign( data, bytes );
        verdict = conformance_block_open( f->field[1], data, bytes, -1, 0 ) ? "open\n" : "refuse\n";
        free( data );
        if ( !spill( out, f->field[1], verdict, strlen( verdict ) ) ) { return 1; }
    }
    return 0;
}

static int surface_cook_foreign( const char * out )
{
    int i;
    for ( i = 0; i < num_lines; i++ )
    {
        const Line * f = &lines[i];
        uint8_t * data;
        size_t bytes = 0;
        const char * verdict;
        if ( strcmp( f->field[0], "cook" ) != 0 ) { continue; }
        data = slurp( f->field[4], &bytes );
        if ( data == NULL ) { fprintf( stderr, "driver: cannot read %s\n", f->field[4] ); return 1; }
        conformance_foreign( data, bytes );
        verdict = conformance_cook_open( f->field[3], data, bytes, -1, 0 ) ? "open\n" : "refuse\n";
        free( data );
        if ( !spill( out, f->field[1], verdict, strlen( verdict ) ) ) { return 1; }
    }
    return 0;
}


static int surface_cook_write(const char * out)
{
    int i,j;
    for(i=0;i<num_lines;i++) {
        const Line * f=&lines[i], * instance=NULL;
        const ConformanceCodec * codec; void * value; ConformanceReport report={0};
        uint8_t * wire, * file; size_t wire_bytes; int64_t need; int big;
        if(strcmp(f->field[0],"cook-write")!=0) { continue; }
        for(j=0;j<num_lines;j++) {
            if(strcmp(lines[j].field[0],"instance")==0 && strcmp(lines[j].field[1],f->field[1])==0) { instance=&lines[j]; break; }
        }
        if(instance==NULL) { return 1; }
        codec=find_codec(instance->field[2],instance->field[3]);
        if(codec==NULL) { return 2; }
        wire=slurp(instance->field[4],&wire_bytes); if(wire==NULL) { return 1; }
        value=codec->storage();
        if(!codec->load(value,wire,(int64_t)wire_bytes,&report)) { free(wire); return 1; } free(wire);
        need=codec->cook_measure(value); if(need<=0) { return 1; }
        file=(uint8_t *)malloc((size_t)need); if(file==NULL) { return 1; }
        for(big=0;big<2;big++) {
            char name[1024]; int ok=codec->cook(value,file,(uint64_t)need,big);
            snprintf(name,sizeof(name),"%s%s",f->field[1],big ? "-be" : "");
            if(!ok || !spill(out,name,file,(size_t)need)) { free(file); return 1; }
        }
        free(file);
    }
    return 0;
}

static int surface_cook( const char * out )
{
    int i;
    for ( i = 0; i < num_lines; i++ )
    {
        const Line * f = &lines[i];
        uint8_t * data;
        size_t bytes = 0;
        ConformanceText text;
        int ok;
        if ( strcmp( f->field[0], "cook" ) != 0 ) { continue; }
        data = slurp( f->field[4], &bytes );
        if ( data == NULL ) { fprintf( stderr, "driver: cannot read %s\n", f->field[4] ); return 1; }
        memset( &text, 0, sizeof( text ) );
        ok = conformance_cook_dump( f->field[3], data, bytes, &text );
        free( data );
        if ( !ok ) { free( text.data ); return 1; }
        ok = spill( out, f->field[1], text.data, text.length );
        free( text.data );
        if ( !ok ) { return 1; }
    }
    return 0;
}

/* the two forgery surfaces are ONE SHAPE and two KINDS: a forged file, the
   extent its caller claims and the buffer that caller holds */
static int surface_forgery( const char * out, const char * kind )
{
    int i;
    for ( i = 0; i < num_lines; i++ )
    {
        const Line * f = &lines[i];
        uint8_t * data;
        size_t bytes = 0;
        int64_t extent;
        int pointer;
        const char * verdict;
        int opened;
        if ( strcmp( f->field[0], "forgery" ) != 0 ) { continue; }
        if ( strcmp( f->field[2], kind ) != 0 ) { continue; }
        data = slurp( f->field[4], &bytes );
        if ( data == NULL ) { fprintf( stderr, "driver: cannot read %s\n", f->field[4] ); return 1; }
        extent = (int64_t) strtoll( f->field[5], NULL, 0 );
        pointer = strcmp( f->field[6], "null" ) == 0 ? -1 : (int) strtol( f->field[6], NULL, 0 );
        if ( strcmp( kind, "block" ) == 0 )
        {
            opened = conformance_block_open( f->field[3], data, bytes, extent, pointer );
        }
        else
        {
            opened = conformance_cook_open( f->field[3], data, bytes, extent, pointer );
        }
        free( data );
        verdict = opened ? "open\n" : "refuse\n";
        if ( !spill( out, f->field[1], verdict, strlen( verdict ) ) ) { return 1; }
    }
    return 0;
}

static int surface_reason(const char * out,const char * kind)
{
    static const char * names[]={"ok","not_a_cook","foreign_order","wrong_build_version","reserved_not_zero","bad_alignment","truncated","unaligned_base","bad_layout","unknown_form","count_over_length","count_over_extent_cap","blob_over_size_cap","data_cycle"};
    int i,j;
    for(i=0;i<num_lines;i++) {
        const Line * r=&lines[i], * f=NULL; uint8_t * data; size_t bytes; int64_t extent; int pointer,reason=0,opened; char answer[64];
        if(strcmp(r->field[0],"refusal")!=0 || r->count<3 || strcmp(r->field[2],kind)!=0) { continue; }
        for(j=0;j<num_lines;j++) { if(strcmp(lines[j].field[0],"forgery")==0 && strcmp(lines[j].field[1],r->field[1])==0) { f=&lines[j]; break; } }
        if(f==NULL) { return 1; } data=slurp(f->field[4],&bytes);if(data==NULL) { return 1; }
        extent=(int64_t)strtoll(f->field[5],NULL,0);pointer=strcmp(f->field[6],"null")==0 ? -1 : (int)strtol(f->field[6],NULL,0);
        opened=strcmp(kind,"cook")==0 ? conformance_cook_open_reason(f->field[3],data,bytes,extent,pointer,&reason) : conformance_block_open_reason(f->field[3],data,bytes,extent,pointer,&reason);
        free(data); if(reason<0 || reason>13) { return 1; }
        snprintf(answer,sizeof(answer),"%s\n",opened ? "ok" : names[reason]);
        if(!spill(out,r->field[1],answer,strlen(answer))) { return 1; }
    }
    return 0;
}

static int surface_message(const char * out)
{
 int i,j;
 for(i=0;i<num_lines;i++){
  const Line * f=lines+i;const Line * connection=NULL;const ConformanceCodec * codec;
  uint8_t * announcement,*message,*answer=NULL;size_t an,n;int64_t size=0;ConformanceReport report={0};int ok;
  if(strcmp(f->field[0],"message")!=0)continue;
  for(j=0;j<num_lines;j++)if(!strcmp(lines[j].field[0],"connection")&&!strcmp(lines[j].field[1],f->field[2])){connection=lines+j;break;}
  if(!connection)return 1;codec=find_codec(connection->field[2],f->field[3]);if(!codec)return 1;
  announcement=slurp(connection->field[4],&an);message=slurp(f->field[5],&n);
  if(!announcement||!message){free(announcement);free(message);return 1;}
  ok=codec->message(announcement,(int64_t)an,message,(int64_t)n,&answer,&size,&report);
  if(ok)ok=spill(out,f->field[1],answer,(size_t)size);
  free(answer);free(message);free(announcement);
  if(!ok){fprintf(stderr,"message %s failed: malformed=%d refused=%d unknown=%d mismatch=%d\n",f->field[1],report.malformed,report.refused,report.unknown,report.kind_mismatch);return 1;}
 }
 return 0;
}

static int surface_retain(const char * out,int saved)
{
 int i,j;
 for(i=0;i<num_lines;i++){
  const Line * f=lines+i;int message=!strcmp(f->field[0],"retain-message"),shift=message;uint8_t * a=NULL,*wire,*answer=NULL;size_t an=0,n;int counters[4]={0},ok;int64_t size=0,ids;char summary[128];
  if(!message&&strcmp(f->field[0],"retain"))continue;
  if(strcmp(f->field[2+shift],"tblrt1")||strcmp(f->field[3+shift],"Node"))return 1;
  if(message){for(j=0;j<num_lines;j++)if(!strcmp(lines[j].field[0],"connection")&&!strcmp(lines[j].field[1],f->field[2])){a=slurp(lines[j].field[4],&an);break;}if(!a)return 1;}
  wire=slurp(f->field[4+shift],&n);if(!wire){free(a);return 1;}ids=!strcmp(f->field[6+shift],"full")?-1:strtoll(f->field[6+shift],NULL,10);
  ok=conformance_retain(message,a,(int64_t)an,wire,(int64_t)n,!strcmp(f->field[5+shift],"short"),ids,counters,&answer,&size);
  if(ok){if(saved)ok=spill(out,f->field[1],answer,(size_t)size);else{int length=snprintf(summary,sizeof(summary),"%d,%d,%d %d\n",counters[0],counters[1],counters[2],counters[3]);ok=spill(out,f->field[1],summary,(size_t)length);}}
  free(answer);free(wire);free(a);if(!ok){fprintf(stderr,"retain %s failed\n",f->field[1]);return 1;}
 }
 return 0;
}

int main( int argc, char ** argv )
{
    const char * surface;
    const char * out;
    if ( argc < 3 )
    {
        fprintf( stderr, "usage: %s <manifest> list\n       %s <manifest> <surface> <outdir>\n", argv[0], argv[0] );
        return 2;
    }
    if ( !read_manifest( argv[1] ) ) { return 1; }
    surface = argv[2];
    if ( strcmp( surface, "list" ) == 0 )
    {
        printf( "wire\nmessage\nretain\nretain-save\nreport\njson-read\njson-write\njson-hostile\ncook\ncook-write\ncook-reason\nblock-reason\ncook-foreign\nblock\nblock-foreign\nblock-dump\nforgery\ncook-forgery\n" );
        return 0;
    }
    if ( argc < 4 )
    {
        fprintf( stderr, "usage: %s <manifest> <surface> <outdir>\n", argv[0] );
        return 2;
    }
    out = argv[3];
    if(!strcmp(surface,"retain"))return surface_retain(out,0);
    if(!strcmp(surface,"retain-save"))return surface_retain(out,1);
    if ( strcmp( surface, "message" ) == 0 ) { return surface_message(out); }
    if ( strcmp( surface, "wire" ) == 0 ) { return surface_wire( out ); }
    if ( strcmp( surface, "report" ) == 0 ) { return surface_report( out ); }
    if ( strcmp( surface, "json-read" ) == 0 ) { return surface_json_read( out ); }
    if ( strcmp( surface, "json-write" ) == 0 ) { return surface_json_write( out ); }
    if ( strcmp( surface, "json-hostile" ) == 0 ) { return surface_json_hostile( out ); }
    if ( strcmp( surface, "cook-reason" ) == 0 ) { return surface_reason(out,"cook"); }
    if ( strcmp( surface, "block-reason" ) == 0 ) { return surface_reason(out,"block"); }
    if ( strcmp( surface, "cook-write" ) == 0 ) { return surface_cook_write(out); }
    if ( strcmp( surface, "cook" ) == 0 ) { return surface_cook( out ); }
    if ( strcmp( surface, "block" ) == 0 ) { return surface_block( out ); }
    if ( strcmp( surface, "cook-foreign" ) == 0 ) { return surface_cook_foreign( out ); }
    if ( strcmp( surface, "block-foreign" ) == 0 ) { return surface_block_foreign( out ); }
    if ( strcmp( surface, "block-dump" ) == 0 ) { return surface_block_dump( out ); }
    if ( strcmp( surface, "forgery" ) == 0 ) { return surface_forgery( out, "block" ); }
    if ( strcmp( surface, "cook-forgery" ) == 0 ) { return surface_forgery( out, "cook" ); }
    return 2;
}
