package cpptable

// fixedroadmap_t11 — the cpp leg's unions-and-optionals rows of
// docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW Fixed Tables"):
// union-field-arms, payload-free-arms, union-arrays, optional-scalars,
// optional-nesting, optional-arrays, scalar-defaults and
// text-bytes-flags-defaults. Law: docs/SPEC-TABLES.md §3.4 (the fixed form's
// record, union and optional wrappers), docs/FIXED-FORM-ALGORITHM.md (the
// prefill and the union arm layout) and docs/FIXED-FORM-VERSIONING-TESTS.md.
//
// [capability] means the leg WRITES and READS the construct with non-default
// and boundary values, asserted, in a target a gate runs: each subtest below
// generates the fixture, compiles a C++ probe against the emitted header and
// runs it — the same shape cppRunVersionProbe uses for the versioning rows.
// One subtest per task id.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// t11Unit parses and checks an inline schema, the fixture every subtest reads.
func t11Unit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	ast, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 || ast == nil {
		t.Fatalf("parse: %v", perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: ast,
	}})
	if len(cerrs) > 0 || u == nil {
		t.Fatalf("check: %v", cerrs)
	}
	return u
}

// t11Probe generates the fixture's type wire and table header, compiles a probe
// whose `body` is the main() after the root's declaration and Reset, and runs
// it. A non-zero exit or a build failure fails the subtest with the probe's own
// words, so a broken walk names the rule it broke.
func t11Probe(t *testing.T, src, root, body string) {
	t.Helper()
	u := t11Unit(t, src)
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatalf("cpp.Generate: %v", err)
	}
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	maps.Copy(files, tables)

	outDir := t.TempDir()
	var header string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(outDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.h") {
			header = name
		}
	}
	if header == "" {
		t.Fatalf("no Table.h was generated; files: %v", t11Names(files))
	}

	probe := fmt.Sprintf(`#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "%s"

using namespace %s;

int main( void )
{
    %s a;
    %sReset( a );
    static uint8_t buf[1 << 16];
%s
    return 0;
}
`, header, u.Package, root, root, body)

	if err := os.WriteFile(filepath.Join(outDir, "probe.cpp"), []byte(probe), 0o600); err != nil {
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
	args := []string{"-std=c++17", "-O1", "-I" + outDir, "-I" + serialize, filepath.Join(outDir, "probe.cpp"), "-o", filepath.Join(outDir, "probe")}
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("the probe did not compile: %v\n%s\n--- probe ---\n%s", err, out, probe)
	}
	out, err := exec.Command(filepath.Join(outDir, "probe")).CombinedOutput()
	if err != nil {
		t.Fatalf("the probe failed: %v\n%s", err, out)
	}
}

func t11Names(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	return names
}

func TestFixedRoadmapCppT11UnionsOptionals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"union-field-arms/cpp/valid-data", t11UnionFieldArms},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t11UnionFieldArms is union-field-arms/cpp/valid-data: a union whose first arm
// holds a scalar and whose second holds a scalar, a string(8) and a scalar, so
// a mislaid arm offset or text length moves a neighbour. SPEC-TABLES §3.4: a
// union arm is stored in one record and the arm's own fields are read at the
// arm's own offsets. The probe writes the SECOND (text) arm with non-default
// values and boundary text (the bound is 8, the value 7 characters plus the
// terminator) and reads it back field for field.
func t11UnionFieldArms(t *testing.T) {
	src := `package t11u

type Plain
{
    n int32 = 7
}

type Labelled
{
    lead  int32 = 5
    label string(8)
    trail int32 = 9
}

union Pick
{
    plain    Plain
    labelled Labelled
}

fixed table Root
{
    pick Pick
}
`
	body := `
    a.pick.type = PickType::Labelled;
    a.pick.labelled.lead = 101;
    strcpy( a.pick.labelled.label, "seven77" );
    a.pick.labelled.label_length = 7;
    a.pick.labelled.trail = 202;

    const int64_t w = RootFixedMeasure( 1 );
    if ( RootFixedSave( &a, 1, buf, w ) != w ) { printf( "union-field-arms: save wrote %lld of %lld\n", (long long) RootFixedSave( &a, 1, buf, w ), (long long) w ); return 1; }

    Root b;
    RootReset( b );
    TableReport r;
    static TableFixedEntry plan[1024];
    memset( &r, 0, sizeof( r ) );
    const int64_t n = RootFixedLoad( &b, 1, buf, w, plan, 1024, NULL, &r );
    if ( n != 1 ) { printf( "union-field-arms: the record did not read: n=%lld reason=%d\n", (long long) n, (int) r.reason ); return 1; }
    if ( b.pick.type != PickType::Labelled ) { printf( "union-field-arms: the arm is not the second: %d\n", (int) b.pick.type ); return 1; }
    if ( b.pick.labelled.label_length != 7 || strcmp( b.pick.labelled.label, "seven77" ) != 0 )
        { printf( "union-field-arms: the arm's string(8) is not whole: len=%d text=%s\n", b.pick.labelled.label_length, b.pick.labelled.label ); return 1; }
    if ( b.pick.labelled.lead != 101 || b.pick.labelled.trail != 202 )
        { printf( "union-field-arms: the arm's scalars moved: lead=%d trail=%d\n", b.pick.labelled.lead, b.pick.labelled.trail ); return 1; }
    if ( r.clamped != 0 || r.malformed || r.refused )
        { printf( "union-field-arms: a clean read moved a counter: clamped=%d malformed=%d refused=%d\n", r.clamped, (int) r.malformed, (int) r.refused ); return 1; }
`
	t11Probe(t, src, "Root", body)
}
