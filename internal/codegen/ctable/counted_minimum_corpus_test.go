package ctable

// TestCCountedMinimumMessageCorpus runs the counted-minimum message corpus
// (testdata/conformance/feature-witnesses/counted-minimum-message, ruling R10)
// on the C leg: `few [2..4]int32` in a fixed root, the message form. Every
// read row is read against the unit's own announcement and owes its verdict;
// every write row hands a fresh Root a live count of all-zero elements and
// owes the corpus's bytes (`wrote`) or a refusal (`refused`), which on this
// debug-assert leg is the debug build stopping in its own process. The birth
// value (two zero elements) is elided by the writer and refused stored by the
// reader, and a fresh Root is born holding it.

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

const cCountedMinCorpusDir = "../../../testdata/conformance/feature-witnesses/counted-minimum-message"

const cCountedMinCorpusProbe = `#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "HEADER"
SHAPE
static uint8_t buf[1 << 16];

int main( int argc, char ** argv )
{
    static TableMessageEntry entries[4096];
    TableReport r, ignored;
    Root v, back[4];
    int64_t size, count = 4;
    if ( argc < 3 ) { return 2; }
    if ( strcmp( argv[1], "read" ) == 0 )
    {
        TableVocabulary voc = table_vocabulary( entries, 4096 );
        FILE * f;
        memset( &r, 0, sizeof( r ) );
        if ( !announce_read( &voc, kTableAnnounce, kTableAnnounceBytes, &r ) ) { printf( "announce refused\n" ); return 1; }
        f = fopen( argv[2], "rb" );
        if ( f == NULL ) { return 1; }
        size = (int64_t) fread( buf, 1, sizeof( buf ), f );
        fclose( f );
        memset( &r, 0, sizeof( r ) );
        printf( "%s\n", root_load_messages( back, &count, &voc, buf, size, &r ) && !r.refused ? "read" : "refused" );
        return 0;
    }
    if ( strcmp( argv[1], "write" ) == 0 && argc == 4 )
    {
        FILE * f;
        root_reset( &v );
        v.few_count = atoi( argv[2] );
        memset( &ignored, 0, sizeof( ignored ) );
        size = root_save_messages( &v, 1, buf, sizeof( buf ), &ignored );
        if ( size <= 0 ) { printf( "refused\n" ); return 0; }
        f = fopen( argv[3], "wb" );
        if ( f == NULL ) { return 1; }
        fwrite( buf, 1, (size_t) size, f );
        fclose( f );
        printf( "wrote\n" );
        return 0;
    }
    return 2;
}
`

func TestCCountedMinimumMessageCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "functional test: invokes the C compiler (cc) and runs compiled C probes")
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	schema, err := os.ReadFile(filepath.Join(cCountedMinCorpusDir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	cases, err := os.ReadFile(filepath.Join(cCountedMinCorpusDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	debug := cArmMinBuild(t, string(schema), "", cCountedMinCorpusProbe, "corpus")
	work := t.TempDir()
	rows := 0
	for line := range strings.SplitSeq(string(cases), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 5 {
			t.Fatalf("cases.tsv: %q is not five columns", line)
		}
		kind, name, value, verdict, wire := f[0], f[1], f[2], f[3], f[4]
		rows++
		switch kind {
		case "read":
			out, err := testproc.CommandContext(t, ctx, debug, "read", filepath.Join(cCountedMinCorpusDir, value+".bin")).CombinedOutput()
			if err != nil {
				t.Errorf("%s: %v\n%s", name, err, out)
				continue
			}
			if got := strings.TrimSpace(string(out)); got != verdict {
				t.Errorf("%s: got %s, want %s", name, got, verdict)
			}
		case "write":
			if _, err := strconv.Atoi(value); err != nil {
				t.Fatalf("%s: count %q", name, value)
			}
			saved := filepath.Join(work, name+".bin")
			out, err := testproc.CommandContext(t, ctx, debug, "write", value, saved).CombinedOutput()
			got := strings.TrimSpace(string(out))
			if verdict == "refused" {
				// the debug build stops in its own process, or the writer refuses
				if err == nil && got != "refused" {
					t.Errorf("%s: count %s was written: %s", name, value, out)
				}
				continue
			}
			if err != nil || got != "wrote" {
				t.Errorf("%s: count %s did not write: %v\n%s", name, value, err, out)
				continue
			}
			have, err := os.ReadFile(saved)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join(cCountedMinCorpusDir, wire+".bin"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(have, want) {
				t.Errorf("%s: wrote % x, want %s's % x", name, have, wire, want)
			}
		default:
			t.Fatalf("cases.tsv: kind %q", kind)
		}
	}
	if rows == 0 {
		t.Fatal("cases.tsv holds no rows")
	}
}
