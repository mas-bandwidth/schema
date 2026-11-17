package ctable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// cNonCanonicalRow is one row of testdata/wire/non-canonical/MANIFEST.tsv.
type cNonCanonicalRow struct {
	file     string
	form     string
	spelling string
	verdict  string
}

func cNonCanonicalRows(t *testing.T, vector string) []cNonCanonicalRow {
	t.Helper()
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
		if f[0] == "file" && f[1] == "form" && f[2] == "case" && f[3] == "verdict" {
			continue
		}
		if f[3] != "read" && f[3] != "refused" {
			t.Fatalf("MANIFEST.tsv:%d: verdict %q, want read or refused", i+1, f[3])
		}
		rows = append(rows, cNonCanonicalRow{file: f[0], form: f[1], spelling: f[2], verdict: f[3]})
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no rows")
	}
	return rows
}

// cNonCanonicalExpected is the canonical file a read row re-saves to: the
// vector itself, or the canonical rewrite the manifest's generator pins beside
// it for a lawful spelling no writer produces (docs/SPEC-TABLES.md §3).
func cNonCanonicalExpected(t *testing.T, vector string, row cNonCanonicalRow) []byte {
	t.Helper()
	path := filepath.Join(vector, row.file)
	if row.form == "variable" {
		rewrite := filepath.Join(vector, "canonical", filepath.Base(row.file))
		if _, err := os.Stat(rewrite); err == nil {
			path = rewrite
		} else if row.file == "variable-fields-out-of-order.bin" {
			path = filepath.Join(vector, "variable-canonical.bin")
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// cNonCanonicalMeasureHolds says whether the load measure answers this row's
// verdict: it sizes the load from the id table, the node table's framing and
// the root's storage walk, so it refuses the structural spellings it meets.
func cNonCanonicalMeasureHolds(row cNonCanonicalRow) bool {
	if row.verdict == "read" {
		return true
	}
	switch row.spelling {
	case "empty-node-table", "node-table-out-of-place":
		return true
	}
	return false
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

// TestCReadsRefuseNonCanonical holds the C variable-form and message-form
// reads to docs/SPEC-TABLES.md §3, §3.1, §3.3, §4 and docs/rulings.json
// reads-refuse-non-canonical over every row of
// testdata/wire/non-canonical/MANIFEST.tsv, through every read entry: the
// load measure, the region load, the builder load, the retaining load and the
// message load. A read row re-saves through each entry's writer to the
// canonical bytes; a refused row is refused by every entry. A message row is
// also read under another build version, where only the union field written
// with arm reference 0 stays refused (§3.3 (c)).
func TestCReadsRefuseNonCanonical(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "an external toolchain")

	repo := filepath.Join("..", "..", "..")
	vector := filepath.Join(repo, "testdata", "wire", "non-canonical")
	rows := cNonCanonicalRows(t, vector)
	for _, row := range rows {
		t.Run(row.file, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Join(vector, row.file))
			if err != nil {
				t.Fatalf("MANIFEST.tsv names %s which is not on disk: %v", row.file, err)
			}
			schema := filepath.Join(repo, "test", "non-canonical", "NonCanonical.schema")
			announcement := filepath.Join(vector, "announcement.bin")
			table := "Root"
			if row.form == "message" {
				table = "Message"
			}
			if strings.HasPrefix(row.file, "../message-one-encoding/") {
				schema = filepath.Join(repo, "test", "message-one-encoding", "One.schema")
				announcement = filepath.Join(repo, "testdata", "wire", "message-one-encoding", "announcement.bin")
				table = "One"
			}
			src, err := os.ReadFile(schema)
			if err != nil {
				t.Fatal(err)
			}
			var probe string
			switch row.form {
			case "variable":
				if table != "Root" {
					t.Fatalf("%s: a variable row outside NonCanonical.schema", row.file)
				}
				probe = cNonCanonicalVariableProbe(data, cNonCanonicalExpected(t, vector, row), row.verdict == "read", cNonCanonicalMeasureHolds(row))
			case "message":
				ann, err := os.ReadFile(announcement)
				if err != nil {
					t.Fatal(err)
				}
				probe = cNonCanonicalMessageProbe(cCanonicalCName(table), table, ann, data, row.verdict == "read", row.spelling != "union-arm-zero")
			case "announcement":
				if row.verdict != "read" {
					t.Fatalf("%s: an announcement row must read", row.file)
				}
				probe = cNonCanonicalAnnouncementProbe(data)
			default:
				t.Fatalf("MANIFEST.tsv: %s: unknown form %q", row.file, row.form)
			}
			runCGenerated(t, string(src), probe)
		})
	}
	t.Run("union None element", testCUnionNoneElementReads)
}

// testCUnionNoneElementReads is the control for §3.3 (c): the refusal is of a
// union FIELD written with arm reference 0, never of a None element of an
// array of unions, which is positional. The writer's batch carrying one reads
// under this reader's build version and under another.
func testCUnionNoneElementReads(t *testing.T) {
	t.Parallel()
	const schema = `package messagenone

union Choice
{
    item uint8
}

fixed table Root
{
    tick    uint8
    choice  Choice
    choices [..2]Choice
}
`
	m := tabletext.NewModel(unitFrom(t, schema))
	value := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(value, []byte(`{"tick": 1, "choices": [{}, {"item": 3}]}`), &report) {
		t.Fatalf("value: %+v", report)
	}
	batch, err := tablewire.EncodeMessage(m, value)
	if err != nil {
		t.Fatal(err)
	}
	runCGenerated(t, schema, fmt.Sprintf(`#include "ProbeTable.h"
#include <stdint.h>
#include <string.h>
#include <stdio.h>
static const uint8_t wire[] = { %s };
static int fail(const char * what) { fprintf(stderr, "%%s\n", what); return 1; }
int main(void)
{
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary v = table_vocabulary( entries, kTableMessageEntriesHere );
    TableReport report; memset( &report, 0, sizeof( report ) );
    int other;
    if ( !announce_read( &v, kTableAnnounce, kTableAnnounceBytes, &report ) ) { return fail("announcement refused"); }
    for ( other = 0; other < 2; other++ )
    {
        Root values[1];
        int64_t count = 1;
        if ( !root_load_messages( values, &count, &v, wire, (int64_t) sizeof( wire ), &report ) || count != 1 ) { return fail("None element refused"); }
        if ( values[0].tick != 1 || values[0].choices_count != 2 || values[0].choices[0].type != 0 || values[0].choices[1].type == 0 || values[0].choice.type != 0 ) { return fail("None element value"); }
        v.build_version ^= 1;
    }
    return 0;
}
`, cNonCanonicalBytes(batch)))
}

func cNonCanonicalVariableProbe(data, expected []byte, read, measureHolds bool) string {
	action := `
    if ( total < 0 ) { return fail("measure refused"); }
    uint8_t * region = (uint8_t *) malloc( (size_t) total + 64 );
    uint8_t * region2 = (uint8_t *) malloc( (size_t) total + 64 );
    uint8_t * out = (uint8_t *) malloc( sizeof( wire ) + sizeof( expected ) + 256 );
    const int64_t cap = (int64_t) ( sizeof( wire ) + sizeof( expected ) + 256 );
    if ( region == NULL || region2 == NULL || out == NULL ) { return fail("allocate"); }
    const Root * value = root_load( region, total, wire, (int64_t) sizeof( wire ), &report );
    if ( value == NULL ) { return fail("load refused"); }
    int64_t saved = root_save( NULL, value, out, cap );
    if ( saved != (int64_t) sizeof( expected ) || memcmp( out, expected, sizeof( expected ) ) != 0 ) { return fail("load save differs"); }
    uint8_t retained[256]; TableRetainId retained_ids[16];
    TableRetain retain; memset( &retain, 0, sizeof( retain ) );
    retain.bytes = retained; retain.capacity = (int64_t) sizeof( retained ); retain.ids = retained_ids; retain.id_capacity = 16;
    const Root * kept = root_load_retain( region2, total, wire, (int64_t) sizeof( wire ), &retain, &report );
    if ( kept == NULL ) { return fail("retain refused"); }
    saved = root_save_retain( kept, &retain, out, cap, &report );
    if ( saved != (int64_t) sizeof( expected ) || memcmp( out, expected, sizeof( expected ) ) != 0 ) { return fail("retain save differs"); }
    RootBuilder builder;
    if ( !root_builder_init( &builder ) ) { return fail("builder init"); }
    if ( !root_load_builder( &builder, wire, (int64_t) sizeof( wire ), &report ) ) { return fail("builder refused"); }
    TableCtx ctx; memset( &ctx, 0, sizeof( ctx ) ); ctx.arena = &builder.arena;
    saved = root_save( &ctx, root_builder_root( &builder ), out, cap );
    if ( saved != (int64_t) sizeof( expected ) || memcmp( out, expected, sizeof( expected ) ) != 0 ) { return fail("builder save differs"); }
    root_builder_shutdown( &builder );
    free( out ); free( region2 ); free( region );`
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
    uint8_t retained[256]; TableRetainId retained_ids[16];
    TableRetain retain; memset( &retain, 0, sizeof( retain ) );
    retain.bytes = retained; retain.capacity = (int64_t) sizeof( retained ); retain.ids = retained_ids; retain.id_capacity = 16;
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
static const uint8_t expected[] = { %s };
static int fail(const char * what) { fprintf(stderr, "%%s\n", what); return 1; }
int main(void)
{
    TableReport report; memset( &report, 0, sizeof( report ) );
    int64_t total = root_load_measure( wire, (int64_t) sizeof( wire ) ); (void) total; (void) expected;%s
    return 0;
}
`, cNonCanonicalBytes(data), cNonCanonicalBytes(expected), action)
}

// cNonCanonicalMessageProbe runs one batch through the message load twice:
// under the announcement as written, where the manifest's verdict holds and a
// read re-saves to the same bytes, and under another build version
// (docs/SPEC-TABLES.md §3.3). There a field at a default and fields out of
// order are lawful evolution and read, re-saving to a batch that reads back
// to the same bytes; a union field written with arm reference 0 (§3.3 (c)) is
// refused at every build.
func cNonCanonicalMessageProbe(table, typeName string, announcement, data []byte, read, otherBuildRead bool) string {
	same := fmt.Sprintf(`
    {
        %s values[1];
        int64_t count = 1;
        if ( !%s_load_messages( values, &count, &v, wire, (int64_t) sizeof( wire ), &report ) ) { return fail("load refused"); }
        if ( count != 1 ) { return fail("load count"); }
        uint8_t out[sizeof( wire ) + 64];
        if ( %s_save_messages( values, count, out, (int64_t) sizeof( out ), &report ) != (int64_t) sizeof( wire ) || memcmp( out, wire, sizeof( wire ) ) != 0 ) { return fail("save differs"); }
    }`, typeName, table, table)
	if !read {
		same = fmt.Sprintf(`
    {
        %s values[1];
        int64_t count = 1;
        if ( %s_load_messages( values, &count, &v, wire, (int64_t) sizeof( wire ), &report ) ) { return fail("load read"); }
        if ( count != 0 ) { return fail("refused load kept a value"); }
    }`, typeName, table)
	}
	other := fmt.Sprintf(`
    v.build_version ^= 1;
    {
        %s values[1];
        int64_t count = 1;
        if ( !%s_load_messages( values, &count, &v, wire, (int64_t) sizeof( wire ), &report ) ) { return fail("other build: load refused"); }
        if ( count != 1 ) { return fail("other build: load count"); }
        uint8_t out[sizeof( wire ) + 64];
        const int64_t n = %s_save_messages( values, count, out, (int64_t) sizeof( out ), &report );
        if ( n <= 0 ) { return fail("other build: save refused"); }
        v.build_version ^= 1;
        %s again[1];
        count = 1;
        if ( !%s_load_messages( again, &count, &v, out, n, &report ) || count != 1 ) { return fail("other build: the rewrite does not read"); }
        uint8_t twice[sizeof( wire ) + 64];
        if ( %s_save_messages( again, count, twice, (int64_t) sizeof( twice ), &report ) != n || memcmp( twice, out, (size_t) n ) != 0 ) { return fail("other build: the rewrite is not canonical"); }
    }`, typeName, table, table, typeName, table, table)
	if !otherBuildRead {
		other = fmt.Sprintf(`
    v.build_version ^= 1;
    {
        %s values[1];
        int64_t count = 1;
        if ( %s_load_messages( values, &count, &v, wire, (int64_t) sizeof( wire ), &report ) ) { return fail("other build: load read"); }
        if ( count != 0 ) { return fail("other build: refused load kept a value"); }
    }`, typeName, table)
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
    if ( !announce_read( &v, announcement, (int64_t) sizeof( announcement ), &report ) ) { return fail("announcement refused"); }%s%s
    return 0;
}
`, cNonCanonicalBytes(announcement), cNonCanonicalBytes(data), same, other)
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
