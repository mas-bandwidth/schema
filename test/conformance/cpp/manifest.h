// THE MANIFEST AND THE FILES of the C++ conformance driver
// (test/conformance/README.md): the manifest reader and the two file helpers
// every surface uses, with no generated header behind them.
//
// It is its own header so that every translation unit of the driver
// (Makefile, CONFORMANCE_DRIVER_TUS) and the block pin writer (block_pin.cpp,
// through block_walk.h) read ONE manifest: manifest_lines is an inline
// variable with external linkage, so the lines main() reads are the lines
// every unit's surface walks. A `static` one would give each unit its own
// empty copy.
#pragma once

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cstdint>
#include <string>
#include <vector>

// ---------------------------------------------------------------------------
// the manifest, read exactly as testdata/conformance/tables/FORMAT.md states it
// ---------------------------------------------------------------------------

struct Line
{
    std::vector<std::string> field;
};

inline std::vector<Line> manifest_lines;

static inline bool read_manifest( const char * path )
{
    FILE * file = fopen( path, "r" );
    if ( file == NULL )
    {
        fprintf( stderr, "driver: cannot open %s\n", path );
        return false;
    }
    std::string text;
    int c;
    while ( ( c = fgetc( file ) ) != EOF )
    {
        if ( c == '\n' )
        {
            if ( !text.empty() && text[0] != '#' )
            {
                Line line;
                size_t i = 0;
                while ( i < text.size() )
                {
                    while ( i < text.size() && ( text[i] == ' ' || text[i] == '\t' || text[i] == '\r' ) ) i++;
                    size_t start = i;
                    while ( i < text.size() && text[i] != ' ' && text[i] != '\t' && text[i] != '\r' ) i++;
                    if ( i > start ) line.field.push_back( text.substr( start, i - start ) );
                }
                if ( !line.field.empty() && line.field[0][0] != '#' ) manifest_lines.push_back( line );
            }
            text.clear();
            continue;
        }
        text.push_back( (char) c );
    }
    fclose( file );
    return true;
}

// ---------------------------------------------------------------------------
// files
// ---------------------------------------------------------------------------

static inline bool slurp( const char * path, std::vector<uint8_t> & out )
{
    FILE * file = fopen( path, "rb" );
    if ( file == NULL ) return false;
    fseek( file, 0, SEEK_END );
    long size = ftell( file );
    fseek( file, 0, SEEK_SET );
    out.resize( size > 0 ? (size_t) size : 0 );
    bool ok = size == 0 || fread( out.data(), 1, (size_t) size, file ) == (size_t) size;
    fclose( file );
    return ok;
}

static inline bool spill( const std::string & dir, const std::string & name, const void * data, size_t bytes )
{
    std::string path = dir + "/" + name;
    FILE * file = fopen( path.c_str(), "wb" );
    if ( file == NULL )
    {
        fprintf( stderr, "driver: cannot write %s\n", path.c_str() );
        return false;
    }
    bool ok = bytes == 0 || fwrite( data, 1, bytes, file ) == bytes;
    fclose( file );
    return ok;
}

