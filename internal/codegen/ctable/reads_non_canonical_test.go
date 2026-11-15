package ctable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type cNonCanonicalRow struct {
	file         string
	form         string
	spelling     string
	verdict      string
	measureHolds bool
}

func cNonCanonicalRows(t *testing.T) (string, []cNonCanonicalRow) {
	t.Helper()
	vector := filepath.Join("..", "..", "..", "testdata", "wire", "non-canonical")
	data, err := os.ReadFile(filepath.Join(vector, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []cNonCanonicalRow
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			t.Fatalf("MANIFEST.tsv:%d: %d fields, want 4", i+1, len(f))
		}
		if f[0] == "file" {
			continue
		}
		// The measure answers a size from the node table's framing and the
		// root's storage walk; it refuses a node table out of its place or
		// empty, which that walk meets. The id table's first-use order is
		// judged by the reads, which meet every reference.
		measureHolds := f[2] == "empty-node-table" || f[2] == "node-table-out-of-place" || (f[1] == "variable" && f[3] == "read")
		rows = append(rows, cNonCanonicalRow{file: f[0], form: f[1], spelling: f[2], verdict: f[3], measureHolds: measureHolds})
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no rows")
	}
	return vector, rows
}

func cNonCanonicalBytes(data []byte) string {
	parts := make([]string, len(data))
	for i, b := range data {
		parts[i] = fmt.Sprint(b)
	}
	return strings.Join(parts, ",")
}

func cCanonicalCName(name string) string {
	var out strings.Builder
	for i, r := range name {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				out.WriteByte('_')
			}
			out.WriteRune(r + ('a' - 'A'))
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

func TestCReadsRefuseNonCanonical(t *testing.T) {
	t.Parallel()

	vector, rows := cNonCanonicalRows(t)
	for _, row := range rows {
		row := row
		t.Run(row.file, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Join(vector, row.file))
			if err != nil {
				t.Fatal(err)
			}
			schema := filepath.Join("..", "..", "..", "test", "non-canonical", "NonCanonical.schema")
			announcement := filepath.Join(vector, "announcement.bin")
			table := "Root"
			if strings.HasPrefix(row.file, "../message-one-encoding/") {
				schema = filepath.Join("..", "..", "..", "test", "message-one-encoding", "One.schema")
				announcement = filepath.Join("..", "..", "..", "testdata", "wire", "message-one-encoding", "announcement.bin")
				table = "One"
			}
			if row.form == "message" {
				table = "Message"
				if strings.HasPrefix(row.file, "../message-one-encoding/") {
					table = "One"
				}
			}
			src, err := os.ReadFile(schema)
			if err != nil {
				t.Fatal(err)
			}
			var probe string
			switch row.form {
			case "variable":
				probe = cNonCanonicalVariableProbe(data, row.verdict == "read", row.measureHolds)
			case "message":
				ann, err := os.ReadFile(announcement)
				if err != nil {
					t.Fatal(err)
				}
				probe = cNonCanonicalMessageProbe(cCanonicalCName(table), table, ann, data, row.verdict == "read")
			case "announcement":
				probe = cNonCanonicalAnnouncementProbe(data)
			default:
				t.Fatalf("unknown form %q", row.form)
			}
			runCGenerated(t, string(src), probe)
		})
	}
}

func cNonCanonicalVariableProbe(data []byte, read, measureHolds bool) string {
	action := fmt.Sprintf(`
    if ( total < 0 ) { return fail("measure refused"); }
    uint8_t * region = (uint8_t *) malloc( (size_t) total + 64 );
    uint8_t * region2 = (uint8_t *) malloc( (size_t) total + 64 );
    uint8_t * out = (uint8_t *) malloc( (size_t) sizeof( wire ) + 64 );
    if ( region == NULL || region2 == NULL || out == NULL ) { return fail("allocate"); }
    const Root * value = root_load( region, total, wire, (int64_t) sizeof( wire ), &report );
    if ( value == NULL ) { return fail("load refused"); }
    int64_t saved = root_save( NULL, value, out, (int64_t) sizeof( wire ) );
    if ( saved != (int64_t) sizeof( wire ) || memcmp( out, wire, sizeof( wire ) ) != 0 ) { return fail("save differs"); }
    TableRetain retain; memset( &retain, 0, sizeof( retain ) );
    const Root * kept = root_load_retain( region2, total, wire, (int64_t) sizeof( wire ), &retain, &report );
    if ( kept == NULL ) { return fail("retain refused"); }
    RootBuilder builder;
    if ( !root_builder_init( &builder ) ) { return fail("builder init"); }
    if ( !root_load_builder( &builder, wire, (int64_t) sizeof( wire ), &report ) ) { return fail("builder refused"); }
    root_builder_shutdown( &builder );
    free( out ); free( region2 ); free( region );`)
	if !read {
		measureCheck := ""
		if measureHolds {
			measureCheck = `if ( total >= 0 ) { return fail("measure read"); }`
		}
		action = fmt.Sprintf(`
    %s
    uint8_t * region = (uint8_t *) malloc( 1 << 20 );
    uint8_t * region2 = (uint8_t *) malloc( 1 << 20 );
    if ( region == NULL || region2 == NULL ) { return fail("allocate"); }
    const Root * value = root_load( region, 1 << 20, wire, (int64_t) sizeof( wire ), &report );
    if ( value != NULL ) { return fail("load read"); }
    TableRetain retain; memset( &retain, 0, sizeof( retain ) );
    const Root * kept = root_load_retain( region2, 1 << 20, wire, (int64_t) sizeof( wire ), &retain, &report );
    if ( kept != NULL ) { return fail("retain read"); }
    RootBuilder builder;
    if ( !root_builder_init( &builder ) ) { return fail("builder init"); }
    if ( root_load_builder( &builder, wire, (int64_t) sizeof( wire ), &report ) ) { return fail("builder read"); }
    root_builder_shutdown( &builder );
    free( region2 ); free( region );`, measureCheck)
	}
	return fmt.Sprintf(`#include "ProbeTable.h"
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
static const uint8_t wire[] = { %s };
static int fail(const char * what) { fprintf(stderr, "%%s\n", what); return 1; }
int main(void)
{
    TableReport report; memset( &report, 0, sizeof( report ) );
    int64_t total = root_load_measure( wire, (int64_t) sizeof( wire ) ); (void) total;%s
    return 0;
}
`, cNonCanonicalBytes(data), action)
}

func cNonCanonicalMessageProbe(table, typeName string, announcement, data []byte, read bool) string {
	action := fmt.Sprintf(`
    %s values[1];
    int64_t count = 1;
    if ( !%s_load_messages( values, &count, &v, wire, (int64_t) sizeof( wire ), &report ) ) { return fail("load refused"); }
    if ( count != 1 ) { return fail("load count"); }
    uint8_t out[sizeof( wire )];
    if ( %s_save_messages( values, count, out, (int64_t) sizeof( out ), &report ) != (int64_t) sizeof( wire ) || memcmp( out, wire, sizeof( wire ) ) != 0 ) { return fail("save differs"); }`, typeName, table, table)
	if !read {
		action = fmt.Sprintf(`
    %s values[1];
    int64_t count = 1;
    if ( %s_load_messages( values, &count, &v, wire, (int64_t) sizeof( wire ), &report ) ) { return fail("load read"); }`, typeName, table)
	}
	return fmt.Sprintf(`#include "ProbeTable.h"
#include <stdint.h>
#include <string.h>
#include <stdio.h>
static const uint8_t announcement[] = { %s };
static const uint8_t wire[] = { %s };
static int fail(const char * what) { fprintf(stderr, "%%s\n", what); return 1; }
int main(void)
{
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary v = table_vocabulary( entries, kTableMessageEntriesHere );
    TableReport report; memset( &report, 0, sizeof( report ) );
    if ( !announce_read( &v, announcement, (int64_t) sizeof( announcement ), &report ) ) { return fail("announcement refused"); }%s
    return 0;
}
`, cNonCanonicalBytes(announcement), cNonCanonicalBytes(data), action)
}

func cNonCanonicalAnnouncementProbe(data []byte) string {
	return fmt.Sprintf(`#include "ProbeTable.h"
#include <stdint.h>
#include <string.h>
#include <stdio.h>
static const uint8_t announcement[] = { %s };
static int fail(const char * what) { fprintf(stderr, "%%s\n", what); return 1; }
int main(void)
{
    uint8_t out[kTableAnnounceBytes];
    if ( announce( out, (int64_t) sizeof( out ) ) != (int64_t) sizeof( announcement ) || memcmp( out, announcement, sizeof( announcement ) ) != 0 ) { return fail("announce bytes"); }
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary v = table_vocabulary( entries, kTableMessageEntriesHere );
    TableReport report; memset( &report, 0, sizeof( report ) );
    if ( !announce_read( &v, announcement, (int64_t) sizeof( announcement ), &report ) || !v.announced ) { return fail("announce read"); }
    return 0;
}
`, cNonCanonicalBytes(data))
}
