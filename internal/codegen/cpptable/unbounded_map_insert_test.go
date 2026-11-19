package cpptable

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

const cppUnboundedMapInsertSchema = `package mapins

table Root
{
    by_name map[string]int32
}
`

const cppUnboundedMapInsertProbe = `#include <cstdio>
#include <cstring>
#include "%s"

using namespace mapins;

int main()
{
    RootBuilder b;
    Root * r = b.GetRoot();
    // Insert in reverse UTF-8 byte order including >16B unbounded key and empty key.
    int32_t * v1 = RootByNameInsert( b.main, r->by_name, "\xF0\x9F\x98\x80" );
    int32_t * v2 = RootByNameInsert( b.main, r->by_name, "\xEF\xBD\xA1" );
    int32_t * v3 = RootByNameInsert( b.main, r->by_name, "unbounded-key-longer-than-thirty-two-bytes" );
    int32_t * v4 = RootByNameInsert( b.main, r->by_name, "" );
    if ( !v1 || !v2 || !v3 || !v4 ) { std::fprintf( stderr, "Insert returned NULL\n" ); return 1; }
    *v1 = 40; *v2 = 30; *v3 = 20; *v4 = 10;

    int32_t * f3 = RootByNameFind( b.arena, r->by_name, "unbounded-key-longer-than-thirty-two-bytes" );
    if ( !f3 || *f3 != 20 ) { std::fprintf( stderr, "Find mismatch\n" ); return 1; }

    if ( !b.Lock() ) { std::fprintf( stderr, "Lock failed\n" ); return 1; }
    const Root * locked = b.AsConst();
    const int32_t * lf1 = locked->by_name.Find( "\xF0\x9F\x98\x80" );
    const int32_t * lf2 = locked->by_name.Find( "\xEF\xBD\xA1" );
    const int32_t * lf4 = locked->by_name.Find( "" );
    if ( !lf1 || *lf1 != 40 || !lf2 || *lf2 != 30 || !lf4 || *lf4 != 10 ) { std::fprintf( stderr, "Locked Find mismatch\n" ); return 1; }

    char jbuf[512];
    int64_t jn = RootToJson( locked, jbuf, sizeof( jbuf ) );
    if ( jn <= 0 ) { std::fprintf( stderr, "ToJson failed\n" ); return 1; }
    std::printf( "OK %%d\n", locked->by_name.size() );
    return 0;
}
`

func TestUnboundedStringMapInsertAndFind(t *testing.T) {
	t.Parallel()
	u := cppUnitFrom(t, cppUnboundedMapInsertSchema)
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	hdr := string(tables["ProbeTable.h"])
	if !strings.Contains(hdr, "static const int64_t kRootByNameEntryKeyBound = INT32_MAX;") {
		t.Fatalf("ProbeTable.h missing 64-bit INT32_MAX KeyBound for unbounded map[string]V")
	}

	slowtest.Gate(t, "a C++17 compiler")
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	if _, err := exec.LookPath(cxx); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and %s is not found: %v", cxx, err)
		}
		t.Skipf("%s: %v", cxx, err)
	}

	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatalf("cpp.Generate: %v", err)
	}
	maps.Copy(files, tables)

	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.cpp") {
			sources = append(sources, filepath.Join(dir, name))
		}
	}
	probe := filepath.Join(dir, "probe.cpp")
	if err := os.WriteFile(probe, []byte(fmt.Sprintf(cppUnboundedMapInsertProbe, "ProbeTable.h")), 0o600); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "probe")
	args := append([]string{"-std=c++17", "-O1", "-Wall", "-Wextra", "-Werror", "-I" + dir, probe}, sources...)
	args = append(args, "-o", exe)
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile failed: %v\n%s", err, out)
	}
	out, err := exec.Command(exe).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "OK 4") {
		t.Fatalf("probe failed (%v): %s", err, out)
	}
}
