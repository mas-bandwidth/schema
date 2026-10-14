/* test/conformance/canary/CANARY-20260922T134610Z.c -- sprint-canary end-to-end positive control (A3/A12, reports/next-sprint-prep-2026-09-21.md).
 *
 * NOT a matrixed conformance cell: no roadmap row, no leg driver wiring, no CI target. This file exists ONLY to
 * prove that the deployed card path (cutter -> worker -> RESULT.md -> harvest -> PR -> friend read -> land) can
 * carry one real, trivial change from launch to a mergeable pull request. It makes one real assertion about this
 * repository: that docs/FIXED-FORM-ALGORITHM.md still states the WRITER'S OWN declared record size bound (the law
 * every schema cell card quotes), plus a negative control proving the check can actually fail.
 *
 * Standalone: depends on no other file under test/conformance/, edits no shared file, needs no generated tables,
 * no make target. Run from the repository root:
 *   cc -std=c11 -Wall -Wextra test/conformance/canary/CANARY-20260922T134610Z.c -o build/canary-20260922T134610Z && ./build/canary-20260922T134610Z
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static int file_contains( const char * path, const char * needle )
{
    FILE * f = fopen( path, "rb" );
    static char buf[1 << 20];
    size_t n;
    if ( !f ) { return 0; }
    n = fread( buf, 1, sizeof(buf) - 1, f );
    fclose( f );
    buf[n] = '\0';
    return strstr( buf, needle ) != NULL;
}

int main( void )
{
    const char * path = "docs/FIXED-FORM-ALGORITHM.md";
    const char * real_needle = "declared record size";
    const char * absent_needle = "the-canary-must-not-find-this-literal-string-2026";
    int real_present = file_contains( path, real_needle );
    int absent_present = file_contains( path, absent_needle );
    int ok = real_present && !absent_present;

    printf( "%s: %s contains the string \"%s\"\n", real_present ? "PASS" : "FAIL", path, real_needle );
    printf( "%s: negative control -- %s does not contain \"%s\"\n", !absent_present ? "PASS" : "FAIL", path, absent_needle );

    if ( !ok )
    {
        fprintf( stderr, "CANARY RED\n" );
        return 1;
    }
    printf( "CANARY GREEN\n" );
    return 0;
}