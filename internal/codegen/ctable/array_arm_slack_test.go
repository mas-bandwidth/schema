package ctable

import (
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// TestPinnedArrayArmSlackC pins the Certify mutant of
// testdata/conformance/tables/wire/widenbound_envelope_spans.bin. Byte 12,
// the spans arm's element kind, is i32 (4) in the seed and i8 (2) in the
// mutant. The arm's L is the seed's i32 width, so three bytes remain inside
// L after the one i8 element. docs/SPEC-TABLES.md §3 and §4 call bytes left
// inside an array's L damage. The plain load and the retaining load both
// refuse that mutant, and both still read the lawful seed: sequence 8, arm
// spans, values -9 and 0.
//
// REVERSED WITNESS: with the emitted array slack check removed, the probe
// goes red.
func TestPinnedArrayArmSlackC(t *testing.T) {
	t.Parallel()
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Fatalf("cc is not on PATH: %v", err)
	}
	seed, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "conformance", "tables", "wire", "widenbound_envelope_spans.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(seed) != 51 || seed[12] != 0x04 {
		t.Fatalf("widenbound_envelope_spans.bin is not the 51-byte seed (len %d byte12 %#x)", len(seed), seed[12])
	}
	mutant := append([]byte(nil), seed...)
	mutant[12] = 0x02
	sum := sha256.Sum256(mutant)
	const want = "228fe1e666bdbb2ec40bce44c4dc22c88d6e53d80fa8155a98b1565b15a10ff3"
	if hex.EncodeToString(sum[:]) != want {
		t.Fatalf("mutant sha256 %x, want %s", sum, want)
	}

	u := zeroWidthUnit(t, "tables/messages/Messages.schema")
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatalf("cgen: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	maps.Copy(files, tables)
	var headers []string
	for name := range tables {
		if strings.HasSuffix(name, "Table.h") {
			headers = append(headers, name)
		}
	}
	if len(headers) != 1 {
		t.Fatalf("the C table generator wrote %d Table.h headers: %v", len(headers), headers)
	}
	src := "#include <stdint.h>\n#include <stdio.h>\n#include <stdlib.h>\n#include <string.h>\n#include \"" + headers[0] + "\"\n" +
		"static const uint8_t good[] = {" + cMessageBytes(seed) + "};\n" +
		"static const uint8_t bad[] = {" + cMessageBytes(mutant) + "};\n" +
		arrayArmSlackProbe

	out, err := runArrayArmSlackProbe(t, cc, files, src)
	if err != nil || !strings.Contains(out, "ARRAY_ARM_SLACK_OK") {
		t.Fatalf("array arm slack: %v\n%s", err, out)
	}

	// The generated check is sometimes wrapped before its closing brace, so
	// the witness matches the condition rather than the whole statement.
	slack := "if ( sub.offset != sub.size )"
	patched := 0
	for name, data := range files {
		text := string(data)
		patched += strings.Count(text, slack)
		files[name] = []byte(strings.ReplaceAll(text, slack, "if ( 0 )"))
	}
	if patched == 0 {
		t.Fatalf("REVERSED WITNESS: the emitted array read holds no slack check to remove")
	}
	out, err = runArrayArmSlackProbe(t, cc, files, src)
	if err == nil && strings.Contains(out, "ARRAY_ARM_SLACK_OK") {
		t.Fatalf("REVERSED WITNESS FAILED: without the array slack check the probe stayed green\n%s", out)
	}
	if !strings.Contains(out, "retaining load accepted bytes left inside the array arm") {
		t.Fatalf("REVERSED WITNESS: the patched reader did not accept the slack\n%v\n%s", err, out)
	}
	t.Logf("reversed witness (%d slack checks dropped): %v\n%s", patched, err, out)
}

const arrayArmSlackProbe = `
static int failures;
#define FAIL( row, why ) do { printf( "FAIL: %s: %s\n", row, why ); failures++; } while ( 0 )

static int spans_ok( const char * row, const ToolEnvelope * root, const TableReport * report )
{
    if ( root == NULL || report->refused ) { FAIL( row, "the lawful short array was refused" ); return 0; }
    if ( root->sequence != 8u ) { FAIL( row, "sequence" ); return 0; }
    if ( root->body.type != TOOL_BODY_TYPE_SPANS ) { FAIL( row, "arm" ); return 0; }
    if ( root->body.as.spans[0] != -9 || root->body.as.spans[1] != 0 ) { FAIL( row, "spans" ); return 0; }
    return 1;
}

int main( void )
{
    uint8_t * raw;
    uint8_t * region;
    static uint8_t store[1 << 16];
    static TableRetainId ids[64];
    TableRetain retain;
    TableReport report;
    const ToolEnvelope * root;
    raw = (uint8_t *) malloc( (1u << 16) + 64u );
    if ( raw == NULL ) { return 2; }
    region = (uint8_t *) ( ( (uintptr_t) raw + 63u ) & ~(uintptr_t) 63u );
    memset( &report, 0, sizeof( report ) );
    root = tool_envelope_load( region, 1 << 16, good, (int64_t) sizeof( good ), &report );
    spans_ok( "load", root, &report );
    memset( &report, 0, sizeof( report ) );
    memset( &retain, 0, sizeof( retain ) );
    retain.bytes = store;
    retain.capacity = sizeof( store );
    retain.ids = ids;
    retain.id_capacity = 64;
    root = tool_envelope_load_retain( region, 1 << 16, good, (int64_t) sizeof( good ), &retain, &report );
    spans_ok( "retain", root, &report );
    memset( &report, 0, sizeof( report ) );
    root = tool_envelope_load( region, 1 << 16, bad, (int64_t) sizeof( bad ), &report );
    if ( root != NULL || !report.refused ) { FAIL( "load-slack", "bytes left inside the array arm were accepted" ); }
    memset( &report, 0, sizeof( report ) );
    memset( &retain, 0, sizeof( retain ) );
    retain.bytes = store;
    retain.capacity = sizeof( store );
    retain.ids = ids;
    retain.id_capacity = 64;
    root = tool_envelope_load_retain( region, 1 << 16, bad, (int64_t) sizeof( bad ), &retain, &report );
    if ( root != NULL || !report.refused ) { FAIL( "retain-slack", "retaining load accepted bytes left inside the array arm" ); }
    free( raw );
    if ( failures == 0 ) { printf( "ARRAY_ARM_SLACK_OK\n" ); }
    return failures != 0;
}
`

func runArrayArmSlackProbe(t *testing.T, cc string, files map[string][]byte, src string) (string, error) {
	t.Helper()
	build := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(build, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	main := filepath.Join(build, "arrayarmslack.c")
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
	if out, err := testproc.Command(t, cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	out, err := testproc.Command(t, binary).CombinedOutput()
	return string(out), err
}
