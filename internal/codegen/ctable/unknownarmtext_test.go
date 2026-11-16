package ctable

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestPinnedUnknownArmTextC pins the fuzz vector unknown_arm_text_nul
// (testdata/wire/tables/fuzz-vectors/INDEX.txt) on the C leg. Every read
// refuses it: docs/SPEC-TABLES.md §6.5 says the strict read's rules are
// decidable from the wire alone and apply to every byte, in fields and
// records a reader declares and in those it does not. streamdemo.Feed's known
// union field carries an arm Feed cannot name, of the text kind, whose text
// holds a NUL. The C read once skipped an unknown arm's payload by its L and
// loaded the value. The plain load, the retaining load and the measure that
// answers a verdict (load_measure_ex) each refuse it.
//
// REVERSED WITNESS: with the walk of the unknown arm's payload removed from
// the emitted union read, the probe goes red.
func TestPinnedUnknownArmTextC(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "cc (it compiles and runs the generated unknown-arm probe)")
	cc, err := exec.LookPath("cc")
	if err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and cc is not on PATH: %v", err)
		}
		t.Skipf("cc is not on PATH: %v", err)
	}
	wire, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", "tables", "fuzz-vectors", "unknown_arm_text_nul.bin"))
	if err != nil {
		t.Fatal(err)
	}
	u := zeroWidthUnit(t, "tables/stream/Stream.schema")
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatalf("cgen: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	maps.Copy(files, tables)
	header := ""
	for name := range tables {
		if strings.HasSuffix(name, "Table.h") {
			header = name
		}
	}
	if header == "" {
		t.Fatalf("the C table generator wrote no Table.h header")
	}

	src := "#include <stdint.h>\n#include <stdio.h>\n#include <stdlib.h>\n#include <string.h>\n#include \"" + header + "\"\n" +
		"static const uint8_t wire[] = {" + cMessageBytes(wire) + "};\n" + unknownArmTextProbe

	out, err := runUnknownArmTextProbe(t, cc, files, src)
	if err != nil || !strings.Contains(out, "UNKNOWN_ARM_TEXT_OK") {
		t.Fatalf("unknown arm text: %v\n%s", err, out)
	}

	// The walk's verdict is dropped: the condition can never hold.
	walk := "if ( !table_skip_framed( arm, kind, "
	patched := 0
	for name, data := range files {
		text := string(data)
		patched += strings.Count(text, walk)
		files[name] = []byte(strings.ReplaceAll(text, walk, "if ( 0 && table_skip_framed( arm, kind, "))
	}
	if patched == 0 {
		t.Fatalf("REVERSED WITNESS: the emitted union read holds no unknown-arm walk to remove")
	}
	out, err = runUnknownArmTextProbe(t, cc, files, src)
	if err == nil && strings.Contains(out, "UNKNOWN_ARM_TEXT_OK") {
		t.Fatalf("REVERSED WITNESS FAILED: without the unknown-arm walk the probe stayed green\n%s", out)
	}
	t.Logf("reversed witness (%d walks dropped): %v\n%s", patched, err, out)
}

const unknownArmTextProbe = `
static int failures;
#define FAIL( row, why ) do { printf( "FAIL: %s: %s\n", row, why ); failures++; } while ( 0 )
int main( void )
{
    static uint8_t store[1 << 16]; static TableRetainId ids[64];
    TableRetain retain; TableReport report; TableRefuseReason reason = SCHEMA_TABLE_REFUSE_OK;
    int64_t need = feed_load_measure( wire, (int64_t) sizeof( wire ) );
    int64_t size = need > 0 ? need : 1 << 16;
    uint8_t * region = (uint8_t *) malloc( (size_t) size );
    if ( region == NULL ) { return 2; }
    memset( &report, 0, sizeof( report ) );
    if ( feed_load( region, size, wire, (int64_t) sizeof( wire ), &report ) != NULL || !report.refused ) { FAIL( "load", "the plain load read the damaged arm" ); }
    memset( &report, 0, sizeof( report ) );
    memset( &retain, 0, sizeof( retain ) ); retain.bytes = store; retain.capacity = sizeof( store ); retain.ids = ids; retain.id_capacity = 64;
    if ( feed_load_retain( region, size, wire, (int64_t) sizeof( wire ), &retain, &report ) != NULL || !report.refused ) { FAIL( "retain", "the retaining load read the damaged arm" ); }
    if ( feed_load_measure_ex( wire, (int64_t) sizeof( wire ), NULL, &reason ) >= 0 ) { FAIL( "measure", "the measure answered a size for the damaged arm" ); }
    free( region );
    if ( failures == 0 ) { printf( "UNKNOWN_ARM_TEXT_OK\n" ); }
    return failures != 0;
}
`

func runUnknownArmTextProbe(t *testing.T, cc string, files map[string][]byte, src string) (string, error) {
	t.Helper()
	build := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(build, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	main := filepath.Join(build, "unknownarmtext.c")
	if err := os.WriteFile(main, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(build, "probe")
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-I", build, main}
	for name := range files {
		if strings.HasSuffix(name, ".c") {
			args = append(args, filepath.Join(build, name))
		}
	}
	args = append(args, "-lm", "-o", binary)
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	out, err := exec.Command(binary).CombinedOutput()
	return string(out), err
}
