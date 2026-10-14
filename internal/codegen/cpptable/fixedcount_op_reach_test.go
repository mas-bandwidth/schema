package cpptable

// fixedcount_op_reach — the cpp leg of SER-SCHEMA-05 part 2: a kTableFixedCount
// entry reads four bytes at its source whatever its size, so a count op must be
// bounded at src + 4 (not src + size) and a counted LOCAL against a PEER whose
// array is bare (te.size < 4) must refuse the plan rather than underflow the
// head subtraction and land a count op that over-reads the peer's record.

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

func TestFixedCountOpReachCoversItsHeader(t *testing.T) {
	t.Parallel()

	// A kTableFixedCount entry reads four bytes at src whatever its size, so
	// the reach bound must add the header for a count exactly as for a text.
	if !strings.Contains(tableFixedRuntime, "( e.op == kTableFixedText || e.op == kTableFixedCount )") {
		t.Error("the reach check does not bound a kTableFixedCount entry at src + 4")
	}
	// The kind-14 compile step refuses a counted local against a peer array too
	// small to carry the four-byte count, before the head subtraction underflows.
	if !strings.Contains(tableFixedRuntime, "if ( d.counted && te.size < 4u ) { c.hostile = true; return; }") {
		t.Error("kind-14 does not refuse a counted local field against a peer array smaller than the count")
	}
	// The refusal must run before the head subtraction, not after: once
	// (te.size - head) underflows the count has already been mis-derived.
	if strings.Index(tableFixedRuntime, "d.counted && te.size < 4u") > strings.Index(tableFixedRuntime, "head = d.counted") {
		t.Error("kind-14 refuses after the head subtraction it must precede")
	}
}

// cppCountOpFixture stages the VOLD_/VNEW_ sibling pair the reader's nil-lock
// lineage reads (the peer is VOLD_foo's bare [1]uint8 array, the reader is
// VNEW_foo's counted [..1]uint8), generates the reader, and answers its header.
func cppCountOpFixture(t *testing.T) (dir, header, pkg string) {
	t.Helper()
	dir = t.TempDir()
	oldSrc := "package vold_foo\n\nfixed table Root\n{\n    arr [1]uint8\n}\n"
	newSrc := "package vnew_foo\n\nfixed table Root\n{\n    arr [..1]uint8\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "VOLD_foo.schema"), []byte(oldSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "VNEW_foo.schema"), []byte(newSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, filepath.Join(dir, "VNEW_foo.schema"))
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.h") {
			header = name
		}
	}
	if header == "" {
		t.Fatal("the reader generated no <Base>Table.h")
	}
	return dir, header, u.Package
}

func TestFixedCountOpReachCoversItsHeaderASan(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the c++ compiler (the leg's own toolchain)")
	dir, header, pkg := cppCountOpFixture(t)

	probe := fmt.Sprintf(`#include <cstdio>
#include <cstring>
#include "%s"

using namespace %s;

int main( void )
{
    static uint8_t file[1 << 16];
    memset( file, 0, sizeof( file ) );
    file[0] = kTableFixedForm;
    TableFixedPut64( file + kTableFixedHashAt, RootFixedKnown[0].hash );
    TableFixedPut32( file + kTableFixedHeaderBytes, (uint32_t) RootFixedKnown[0].layout_bytes );
    memcpy( file + kTableFixedHeaderBytes + 4, RootFixedKnown[0].layout, (size_t) RootFixedKnown[0].layout_bytes );
    uint8_t * at = file + kTableFixedHeaderBytes + 4 + RootFixedKnown[0].layout_bytes;
    TableFixedPut64( at, RootFixedKnown[0].hash );
    at[8] = 0x7Fu; /* the bare [1]uint8 array element: the peer's last record */
    const int64_t len = (int64_t) ( at + 9 - file );

    Root back;
    RootReset( back );
    static TableFixedEntry plan[4096];
    TableReport r;
    memset( &r, 0, sizeof( r ) );
    const int64_t n = RootFixedLoad( &back, 1, file, len, plan, 4096, NULL, &r );
    if ( n != -1 ) { printf( "count-op: loaded %%lld, want a refusal\n", (long long) n ); return 1; }
    if ( !r.refused ) { printf( "count-op: the read failed without a named refusal\n" ); return 1; }
    if ( r.reason != layout_record_too_large ) { printf( "count-op: reason %%d, want layout_record_too_large\n", (int) r.reason ); return 1; }
    printf( "count-op refused by name\n" );
    return 0;
}
`, header, pkg)
	if err := os.WriteFile(filepath.Join(dir, "probe.cpp"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	serialize, err := filepath.Abs("../../../../serialize")
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c++17", "-O1", "-fsanitize=address", "-fno-sanitize-recover=all", "-g", "-I" + dir, "-I" + serialize, filepath.Join(dir, "probe.cpp"), "-o", filepath.Join(dir, "probe")}
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("the probe did not compile: %v\n%s", err, out)
	}
	out, err := exec.Command(filepath.Join(dir, "probe")).CombinedOutput()
	if err != nil {
		t.Fatalf("the probe failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "count-op refused by name") {
		t.Fatalf("the probe said %q", out)
	}
}
