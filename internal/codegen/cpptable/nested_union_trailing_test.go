package cpptable

// TestNestedUnionArmWithTrailingBytesIsMalformed pins the one check the arm
// payload table of docs/SPEC-TABLES.md:3887 leaves implicit and arms.go:3887
// did not make: an arm whose payload is a UNION has an `L` that is exactly the
// inner arm id's length, or that plus the inner kind byte, its `L` and its
// payload (:3887). A read that stops short of that `L` — one byte of the arm's
// own framing that no field claims — is that arm's own framing damage
// (:3900-3903): the union reads None, `malformed` counts, and the parent reads
// on past `L`.
//
// The C++ reference checked the arm reader's consumption for a body arm
// (arms.go:284) and an enum arm (arms.go:385) but only advanced the reader past
// a nested-union arm (arms.go:419). This test is red there: the emitted text
// carries no `offset == size` guard after the nested-union arm.
//
// Two assertions, one file:
//
//   - always, on the emitted C++: the nested-union arm requires its arm reader
//     (the `sub` bounded to the arm's `L`) to have consumed exactly that `L`.
//   - behind slowtest.Gate, because it compiles and runs the generated unit:
//     a block-form table whose outer arm `L` is one byte longer than the inner
//     union's payload decodes `malformed`, and the union reads None. The saved
//     table is canonical; only that one length byte is raised, so the trailing
//     byte is the root body's own terminator, which `r.room` already covers.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// nestedTrailSchema is the pair the assertions generate: the outer union's
// first arm `inner` is itself a union, so the arm payload is an inner arm id,
// its kind, its `L` and its payload, and `Inner` is reached by the table
// through the outer union, so its arms may be declared type payloads.
const nestedTrailSchema = `package nesttrail

type Leaf
{
    n int32
}

type Mid
{
    m int32
}

union Inner
{
    a Leaf
    b Mid
}

type OuterA
{
    x int32
}

union Outer
{
    inner Inner
    other OuterA
}

table Root
{
    id   uint32
    pick Outer
}
`

// TestNestedUnionArmWithTrailingBytesIsMalformed is table-free and opens with
// t.Parallel(); the toolchain half skips by name on a default `go test`.
func TestNestedUnionArmWithTrailingBytesIsMalformed(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "NestTrail.schema")
	if err := os.WriteFile(path, []byte(nestedTrailSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, path)

	// The table codec and the packet header the Table.h includes are two
	// emitters' outputs; merge them as the driver does.
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range tables {
		files[name] = data
	}

	header, ok := files["NestTrailTable.h"]
	if !ok {
		t.Fatal("Generate emits NestTrailTable.h")
	}
	text := string(header)

	// The arm reader is the `sub` bounded to the outer arm's `L`; the union
	// that carries the arm is `value.pick`. The guard is the whole of the
	// assertion: at BASE this line is absent and the test is red.
	want := "if ( sub.offset != sub.size ) { value.pick.type = OuterType::None; r.report->malformed = true; }"
	if !strings.Contains(text, want) {
		t.Fatalf("the nested-union arm does not require its arm reader to consume exactly L; the emitted C++ misses:\n    %s", want)
	}

	slowtest.Gate(t, "the C++ compiler (it compiles and runs the generated unit)")

	out, err := runNestedTrailProbe(t, files, "NestTrailTable.h")
	if err != nil {
		t.Fatalf("the generated unit did not accept the trailing byte as malformed:\n%s\n%v", out, err)
	}
}

// nestedTrailProbe is the C++ body: it saves a canonical Root, raises the one
// length byte of the nested-union arm's `L` by one, and loads the result.
const nestedTrailProbe = `
    Root r;
    RootReset( r );
    r.pick.type = OuterType::Inner;
    r.pick.inner.type = InnerType::A;
    r.pick.inner.a.n = (int32_t) 0xA1B2C3D4;

    static uint8_t buffer[1 << 16];
    const int64_t n = RootSave( &r, buffer, (int64_t) sizeof( buffer ) );
    if ( n <= 0 ) { printf( "save wrote %lld\n", (long long) n ); return 1; }

    // The leaf's int32 is the inner union's arm payload. The nested-union
    // arm's L sits six bytes before it: the inner arm id, its kind, its L,
    // then the leaf body's field reference and kind.
    const uint8_t pattern[4] = { 0xD4, 0xC3, 0xB2, 0xA1 };
    int64_t at = -1;
    for ( int64_t i = 0; i + 4 <= n; i++ )
        if ( memcmp( buffer + i, pattern, 4 ) == 0 )
        {
            if ( at != -1 ) { printf( "the arm payload is not unique\n" ); return 1; }
            at = i;
        }
    if ( at < 6 ) { printf( "the arm payload was not found\n" ); return 1; }
    buffer[at - 6] += 1; // one byte longer than the inner union's payload

    const int64_t region_bytes = RootLoadMeasure( buffer, n );
    if ( region_bytes <= 0 ) { printf( "measure: %lld\n", (long long) region_bytes ); return 1; }
    static uint8_t raw[1 << 16];
    const uintptr_t off = ( ( -(uintptr_t) &raw[0] ) & 63 );
    if ( off + (uintptr_t) region_bytes > sizeof( raw ) ) { printf( "the region buffer is too small\n" ); return 1; }
    uint8_t * region = raw + off;

    TableReport report;
    memset( &report, 0, sizeof( report ) );
    const Root * got = RootLoad( region, region_bytes, buffer, n, &report );
    if ( got == NULL ) { printf( "load returned NULL, malformed=%d\n", (int) report.malformed ); return 1; }
    if ( !report.malformed ) { printf( "a nested-union arm with a trailing byte is not malformed\n" ); return 1; }
    if ( got->pick.type != OuterType::None ) { printf( "the union did not read None: %d\n", (int) got->pick.type ); return 1; }
    return 0;
`

// runNestedTrailProbe writes the generated files, compiles a probe including
// `header`, and runs it. It is cppRunUnit's generate-and-run shape with the
// packet and table emitters' outputs already merged.
func runNestedTrailProbe(t *testing.T, files map[string][]byte, header string) ([]byte, error) {
	t.Helper()
	outDir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(outDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	probe := fmt.Sprintf(`#include <cstdio>
#include <cstring>
#include <cstdint>
#include "%s"

using namespace nesttrail;

int main( void )
{
%s
}
`, header, nestedTrailProbe)
	if err := os.WriteFile(filepath.Join(outDir, "probe.cpp"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	args := []string{"-std=c++17", "-O1", "-I" + outDir, filepath.Join(outDir, "probe.cpp"), "-o", filepath.Join(outDir, "probe")}
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		return out, fmt.Errorf("the probe did not compile: %w", err)
	}
	return exec.Command(filepath.Join(outDir, "probe")).CombinedOutput()
}
