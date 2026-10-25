// THE OPTIONAL MAP'S JSON NULL RESET IN THE C++ REFERENCE
// (docs/SPEC-TABLES.md §2.8, §16.2): a map's default is its EMPTY SIXTEEN-BYTE
// SLOT, never the ENTRY table its descriptor names. Reading
//
//	{"id":1,"tags":{"1":{"marker":5},"2":{"marker":6}},"tags":null}
//
// must leave `tags` absent and empty, write the same bytes as a value that
// never had the key, and leave the fields after the slot alone. The value table
// V declares `marker int32 = 7`, so the entry's reset writes non-zero bytes:
// with the map branch reverted to `f->table->reset(...)`, the entry reset reads
// the slot as an entry and leaves the two live entries' count in it.
//
// The oracle is internal/codegen/gotable/mappresence_test.go: its
// TestGoMapPresence case "a repeated key whose last value is null leaves the
// map absent and empty" (report.Duplicate == 1, describe() == "absent") and
// its "null does not write the absent wire" assertion. The JSON text and the
// wire are the same bytes that case pins for `?map[string(8)]int32`; this
// probe transcribes the answer, never a value the C++ port computed for itself.
package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const cppMapJsonNullSchema = `package probe

table V
{
    marker int32 = 7
}

table M
{
    id   int32
    tags ?map[uint32]V
    tail int32 = 9
}
`

const cppMapJsonNullProbe = `#include "ProbeTable.h"
#include <cstddef>
#include <cstdio>
#include <cstring>
using namespace probe;
#define CHECK(x) do { if (!(x)) { fprintf(stderr,"%s\n",#x); return 1; } } while(0)

static int reset_case( const char * text, bool repeated )
{
    const char * fresh = "{\"id\":1}";
    MBuilder b;
    TableReport report = {};
    if ( !MFromJson( b, text, (int64_t) strlen( text ), &report ) )
    {
        fprintf( stderr, "tags: FromJson refused\n" );
        return 1;
    }
    if ( report.malformed || report.refused || report.kind_mismatch != 0 || report.unknown != 0 )
    {
        fprintf( stderr, "tags: FromJson report malformed=%d refused=%d kind_mismatch=%d unknown=%d\n",
                 (int) report.malformed, (int) report.refused, (int) report.kind_mismatch, (int) report.unknown );
        return 1;
    }
    if ( report.duplicate != ( repeated ? 1 : 0 ) )
    {
        fprintf( stderr, "tags: duplicate %d\n", (int) report.duplicate );
        return 1;
    }
    const M * v = b.GetRoot();
    // A JSON null IS ABSENT (§16.2): Go's describe() reads "absent", which
    // means the presence companion is false AND the map count is zero.
    if ( v->tags_present )
    {
        fprintf( stderr, "tags: present after null\n" );
        return 1;
    }
    if ( v->tags.count != 0 )
    {
        fprintf( stderr, "tags: count %d after null\n", (int) v->tags.count );
        return 1;
    }
    // THE MAP'S DEFAULT IS ITS EMPTY SIXTEEN-BYTE SLOT (§2.8): the entry
    // reset leaves a reference and a count in it, the slot reset does not.
    const uint8_t zero_slot[16] = {};
    if ( memcmp( (const void *) &v->tags, zero_slot, sizeof( v->tags ) ) != 0 )
    {
        fprintf( stderr, "tags: the empty map slot is not sixteen zero bytes\n" );
        return 1;
    }
    MBuilder fb;
    TableReport fresh_report = {};
    CHECK( MFromJson( fb, fresh, (int64_t) strlen( fresh ), &fresh_report ) );
    const M * f = fb.GetRoot();
    // THE BYTES AFTER THE SLOT are untouched: the field that follows it keeps
    // its declared default, and the presence companion stays absent.
    if ( v->tags_present != f->tags_present || v->tail != f->tail )
    {
        fprintf( stderr, "tags: the bytes after the map slot moved\n" );
        return 1;
    }
    CHECK( b.Lock() );
    CHECK( fb.Lock() );
    alignas( 64 ) static uint8_t wire[1<<16];
    alignas( 64 ) static uint8_t fresh_wire[1<<16];
    const int64_t n = MSave( b.AsConst(), wire, sizeof( wire ) );
    const int64_t fn = MSave( fb.AsConst(), fresh_wire, sizeof( fresh_wire ) );
    // SAVE OF THE RESULT IS BYTE-IDENTICAL to Save of a value that never had
    // the key (Go's "null does not write the absent wire", goldenAbsent).
    if ( n < 0 || fn < 0 || n != fn || memcmp( wire, fresh_wire, (size_t) n ) != 0 )
    {
        fprintf( stderr, "tags: Save differs from a value that never had the key\n" );
        return 1;
    }
    return 0;
}

int main()
{
    if ( reset_case( "{\"id\":1,\"tags\":{\"1\":{\"marker\":5},\"2\":{\"marker\":6}},\"tags\":null}", true ) ) { return 1; }
    if ( reset_case( "{\"id\":1,\"tags\":{\"1\":{},\"2\":{}},\"tags\":null}", true ) ) { return 1; }
    if ( reset_case( "{\"id\":1,\"tags\":null}", false ) ) { return 1; }
    return 0;
}
`

// cppMapJsonNullRun generates the schema, compiles the probe in a t.TempDir()
// and runs it. revert plants the branch's removal in the GENERATED source, so
// the test's witness is the same emitter the tree ships, with the map branch
// gone: the entry table's reset then takes the slot, as before the branch.
func cppMapJsonNullRun(t *testing.T, cxx string, revert bool) (string, error) {
	t.Helper()
	files, err := New().Generate(unitFromSource(t, cppMapJsonNullSchema), "cpp", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if revert {
		old := "if ( TableJsonIsMap( f ) ) { memset( (uint8_t *) base + f->offset, 0, 16 ); }\n                else if ( f->table != NULL )"
		planted := false
		for file, data := range files {
			if strings.Contains(string(data), old) {
				files[file] = []byte(strings.Replace(string(data), old, "if ( f->table != NULL )", 1))
				planted = true
			}
		}
		if !planted {
			t.Fatalf("the generated source no longer holds the map branch this witness reverts: %q", old)
		}
	}
	dir := t.TempDir()
	for file, data := range files {
		if err := os.WriteFile(filepath.Join(dir, file), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	main := filepath.Join(dir, "main.cpp")
	if err := os.WriteFile(main, []byte(cppMapJsonNullProbe), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args := []string{"-std=c++17", "-O1", "-Wall", "-Wextra", "-Werror", "-I", dir, main}
	for file := range files {
		if strings.HasSuffix(file, ".cpp") {
			args = append(args, filepath.Join(dir, file))
		}
	}
	args = append(args, "-o", bin)
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	return string(out), err
}

// TestCPPOptionalMapJsonNullResetsToEmpty pins the map arm of the JSON null
// reset, and shows it RED with that arm reverted.
func TestCPPOptionalMapJsonNullResetsToEmpty(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C++ compiler")
	cxx, err := exec.LookPath("c++")
	if err != nil {
		t.Skip("the generated C++ is compiled and run: no c++ on PATH")
	}
	if out, err := cppMapJsonNullRun(t, cxx, false); err != nil {
		t.Fatalf("the null reset probe failed: %v\n%s", err, out)
	}
	// REVERSED WITNESS: with the map branch gone the entry reset takes the
	// slot, and the probe must go red naming the field.
	t.Run("reversed witness", func(t *testing.T) {
		out, err := cppMapJsonNullRun(t, cxx, true)
		if err == nil {
			t.Fatalf("the probe passed with the map branch reverted, so it does not pin the branch:\n%s", out)
		}
		if !strings.Contains(out, "tags") {
			t.Fatalf("the reverted probe failed without naming the field tags:\n%s", out)
		}
		t.Logf("reverted branch red: %s", strings.TrimSpace(out))
	})
}
