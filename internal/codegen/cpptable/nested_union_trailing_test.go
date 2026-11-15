package cpptable

// TestNestedUnionArmTrailingBytesRefused pins the C++ reader to the Go
// oracle's reading of a nested-union arm whose `L` is longer than the inner
// union it frames: the bytes left inside the arm after the inner union are
// damage, and the strict read refuses the whole load before any value is
// decoded. The Go oracle's wire-alone walk (internal/tablewire/wirecheck.go,
// "bytes left inside an arm after its payload") and the other ports read it
// so, and the C++ reader holds it in the one strict walk every form-1 read
// runs first (TableWireStrict, wirestrict.go), not in the arm's decode.
//
// Two assertions, one file:
//
//   - always, on the emitted C++: the nested-union arm's decode carries no
//     consumption guard of its own after the inner union; the strict walk
//     is the one place the rule is decided.
//   - behind slowtest.Gate, because it compiles and runs the generated unit:
//     a saved table with one extra byte inserted after the inner union's
//     payload, and the outer arm's `L` raised to cover it, is refused: the
//     measure sizes no region and the load answers no root.

import (
	"fmt"
	"maps"
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

// TestNestedUnionArmTrailingBytesRefused is table-free and opens with
// t.Parallel(); the toolchain half skips by name on a default `go test`.
func TestNestedUnionArmTrailingBytesRefused(t *testing.T) {
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
	maps.Copy(files, tables)

	header, ok := files["NestTrailTable.h"]
	if !ok {
		t.Fatal("Generate emits NestTrailTable.h")
	}
	text := string(header)

	// The arm reader is the `sub` bounded to the outer arm's `L`; the union
	// that carries the arm is `value.pick`. The rule is decided once, by the
	// strict walk before the decode, so the arm's decode carries no guard of
	// its own.
	guard := "if ( sub.offset != sub.size ) { value.pick.type = OuterType::None; r.report->damaged = true; }"
	if strings.Contains(text, guard) {
		t.Fatalf("the nested-union arm's decode carries a second guard on bytes left inside its L, beside the strict walk:\n    %s", guard)
	}

	slowtest.Gate(t, "the C++ compiler (it compiles and runs the generated unit)")

	out, err := runNestedTrailProbe(t, files, "NestTrailTable.h")
	if err != nil {
		t.Fatalf("the generated unit did not refuse the bytes left inside the arm:\n%s\n%v", out, err)
	}
}

// nestedTrailProbe is the C++ body: it saves a canonical Root, inserts one
// byte after the inner union's payload, raises the nested-union arm's `L` by
// one to cover it, and requires the measure and the load to refuse it.
const nestedTrailProbe = `
    Root r;
    RootReset( r );
    r.id = 7;
    r.pick.type = OuterType::Inner;
    r.pick.inner.type = InnerType::A;
    r.pick.inner.a.n = (int32_t) 0xA1B2C3D4;

    static uint8_t buffer[1 << 16];
    int64_t n = RootSave( &r, buffer, (int64_t) sizeof( buffer ) - 1 );
    if ( n <= 0 ) { printf( "save wrote %lld\n", (long long) n ); return 1; }

    // The leaf's int32 is the inner union's arm payload. The nested-union
    // arm's L sits six bytes before it: the inner arm id, its kind, its L,
    // then the leaf body's field reference and kind. The inner union's
    // payload ends after the int32 and the leaf body's zero terminator.
    const uint8_t pattern[4] = { 0xD4, 0xC3, 0xB2, 0xA1 };
    int64_t at = -1;
    for ( int64_t i = 0; i + 4 <= n; i++ )
        if ( memcmp( buffer + i, pattern, 4 ) == 0 )
        {
            if ( at != -1 ) { printf( "the arm payload is not unique\n" ); return 1; }
            at = i;
        }
    if ( at < 6 ) { printf( "the arm payload was not found\n" ); return 1; }
    if ( buffer[at + 4] != 0 ) { printf( "the leaf body does not end after its int32\n" ); return 1; }
    const int64_t end = at + 5;
    memmove( buffer + end + 1, buffer + end, (size_t) ( n - end ) );
    buffer[end] = 0xEE; // claimed by no field, inside the arm's L
    n += 1;
    buffer[at - 6] += 1; // the arm's L covers it

    // the same file without the byte reads whole: the refusal below is the
    // byte's, not the probe's
    {
        static uint8_t whole[1 << 16];
        memcpy( whole, buffer, (size_t) n );
        memmove( whole + end, whole + end + 1, (size_t) ( n - end - 1 ) );
        whole[at - 6] -= 1;
        if ( RootLoadMeasure( whole, n - 1 ) <= 0 ) { printf( "the unaltered file does not measure\n" ); return 1; }
    }

    const int64_t region_bytes = RootLoadMeasure( buffer, n );
    if ( region_bytes >= 0 ) { printf( "measure sized %lld region bytes for bytes left inside the arm\n", (long long) region_bytes ); return 1; }
    static uint8_t raw[1 << 16];
    const uintptr_t off = ( ( -(uintptr_t) &raw[0] ) & 63 );
    uint8_t * region = raw + off;

    TableReport report;
    memset( (void *) &report, 0, sizeof( report ) );
    const Root * got = RootLoad( region, (int64_t) ( sizeof( raw ) - off ), buffer, n, &report );
    if ( got != NULL ) { printf( "the load read past the bytes left inside the arm: id=%u\n", (unsigned) got->id ); return 1; }
    if ( !report.damaged && !report.refused ) { printf( "the load answered no root and reported no refusal\n" ); return 1; }
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
