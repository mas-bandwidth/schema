package cpptable

// TestCppMessageFormStrict pins the message form's ONE READ rule
// (docs/SPEC-TABLES.md §6.5, docs/plans/strict-only.md): a read of a BATCH
// returns the whole value or refuses the whole load. The reference read is
// the only read (there is no tolerant twin), so a value a writer carries past
// its declared range must refuse every body: LoadMessages answers false, the
// returned count is 0, the value the walk touched is reset to its declared
// default, and the report names out_of_range and no damage.
//
// The probe drives a fixed root whose one scalar is `narrow uint8` over
// [200, 250] with default 200. A value of 263 rides the wire (the writer
// clamps nothing, §3.1), so the strict read must meet it and refuse.

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const cppMessageFormStrictSchema = `package msgstrict

fixed table Root
{
    narrow uint8 = 200 | min = 200, max = 250
}
`

// cppMessageFormStrictMain is the probe: it announces the unit's own
// vocabulary, round-trips a clean value, then saves a value past max and
// asserts the strict read refuses the whole load. It returns nonzero and
// prints its red line on any failed assertion.
const cppMessageFormStrictMain = `#include "ProbeTable.h"
#include <cstdio>
#include <cstdint>
#include <cstring>
using namespace msgstrict;

int main( void )
{
    // the unit's own announcement, read into this reader's vocabulary
    static uint8_t ann[4096];
    const int64_t announced = Announce( ann, (int64_t) sizeof( ann ) );
    if ( announced != AnnounceMeasure() ) { printf( "announce: measure mismatch\n" ); return 1; }
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary vocabulary( entries, kTableMessageEntriesHere );
    TableReport ar; memset( &ar, 0, sizeof( ar ) );
    if ( !AnnounceRead( vocabulary, ann, announced, &ar ) ) { printf( "announce: refused\n" ); return 1; }

    // a clean value reads whole, and the same on a second read.
    {
        Root v; RootReset( v );
        v.narrow = 210;
        uint8_t buf[512];
        TableReport save; memset( &save, 0, sizeof( save ) );
        const int64_t n = RootSaveMessages( &v, 1, buf, (int64_t) sizeof( buf ), &save );
        if ( n < 1 ) { printf( "clean: save failed\n" ); return 1; }
        Root got; RootReset( got ); got.narrow = 99;
        int64_t count = 1;
        TableReport r; memset( &r, 0, sizeof( r ) );
        if ( !RootLoadMessages( &got, &count, vocabulary, buf, n, &r ) ) { printf( "clean: the strict read refused\n" ); return 1; }
        if ( count != 1 || got.narrow != 210 || r.damaged || r.refused || r.refuse_reason != ok )
            { printf( "clean: count=%lld narrow=%d damaged=%d refused=%d\n", (long long) count, (int) got.narrow, (int) r.damaged, (int) r.refused ); return 1; }
    }

    // A value past its declared range rides the wire (the writer clamps
    // nothing, §3.1) and the STRICT read must refuse the WHOLE load: false,
    // count 0, the value reset to its default, out_of_range, and no damage.
    {
        Root v; RootReset( v );
        v.narrow = 263; // past max 250
        uint8_t buf[512];
        TableReport save; memset( &save, 0, sizeof( save ) );
        const int64_t n = RootSaveMessages( &v, 1, buf, (int64_t) sizeof( buf ), &save );
        if ( n < 1 ) { printf( "oor: save failed\n" ); return 1; }
        Root bad; RootReset( bad ); bad.narrow = 7;
        int64_t count = 1;
        TableReport r; memset( &r, 0, sizeof( r ) );
        if ( RootLoadMessages( &bad, &count, vocabulary, buf, n, &r ) ) { printf( "oor: the strict read accepted an out_of_range value\n" ); return 1; }
        if ( r.damaged ) { printf( "oor: reported damage, want out_of_range\n" ); return 1; }
        if ( !r.refused || r.refuse_reason != out_of_range ) { printf( "oor: refused=%d reason=%d\n", (int) r.refused, (int) r.refuse_reason ); return 1; }
        if ( count != 0 ) { printf( "oor: count=%lld\n", (long long) count ); return 1; }
        if ( bad.narrow != 200 ) { printf( "oor: the value was not reset to its default: %d\n", (int) bad.narrow ); return 1; }
    }

    return 0;
}
`

// cppMessageStrictRun generates the leg's C++ for the schema, applies the
// mutation (if any) to the generated files, compiles the probe beside the
// generated translation units and runs it. The probe returns nonzero on a
// failed assertion; the caller decides what a nonzero exit means.
func cppMessageStrictRun(t *testing.T, undermine func(map[string][]byte)) ([]byte, error) {
	t.Helper()
	u := cppUnitFrom(t, cppMessageFormStrictSchema)
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	if undermine != nil {
		undermine(files)
	}
	dir := t.TempDir()
	var header string
	var sources []string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		switch {
		case strings.HasSuffix(name, "Table.h"):
			header = name
		case strings.HasSuffix(name, ".cpp"):
			sources = append(sources, filepath.Join(dir, name))
		}
	}
	if header == "" {
		t.Fatal("no Table.h was generated")
	}
	probe := `#include <cstdio>
#include <cstring>
#include <cstdint>
#include "` + header + `"
` + cppMessageFormStrictMain
	if err := os.WriteFile(filepath.Join(dir, "probe.cpp"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	args := []string{"-std=c++17", "-O1", "-I" + dir, filepath.Join(dir, "probe.cpp")}
	args = append(args, sources...)
	args = append(args, "-o", filepath.Join(dir, "probe"))
	build := exec.Command(cxx, args...)
	if out, err := build.CombinedOutput(); err != nil {
		return out, err
	}
	return exec.Command(filepath.Join(dir, "probe")).CombinedOutput()
}

func TestCppMessageFormStrict(t *testing.T) {
	slowtest.Gate(t, "the C++ compiler (the leg's own toolchain)")
	t.Parallel()
	out, err := cppMessageStrictRun(t, nil)
	if err != nil {
		t.Fatalf("the message-form strict read: %v\n%s", err, out)
	}
}

// TestCppMessageFormStrictNegativeControl plants the fault this card closes in
// the real artifact: it removes the generated strict read's whole-load refusal
// guard (so the body ends as a value even when it refused) and requires the
// same probe to go red. The probe's out_of_range row is exactly the read that
// a tolerant body accepts.
func TestCppMessageFormStrictNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the C++ compiler (the leg's own toolchain)")
	t.Parallel()
	const guard = "if ( report->refused ) { return false; } // a refusal refuses the whole batch (§3.3, §4)"
	undermine := func(files map[string][]byte) {
		for name, data := range files {
			if strings.HasSuffix(name, "Table.h") {
				text := string(data)
				if strings.Count(text, guard) != 1 {
					t.Fatalf("%s: the refusal guard is not there exactly once", name)
				}
				files[name] = []byte(strings.ReplaceAll(text, guard, ""))
			}
		}
	}
	out, err := cppMessageStrictRun(t, undermine)
	if err == nil {
		t.Fatalf("with the refusal guard removed, the strict probe still passed:\n%s", out)
	}
}
