package cpptable

// THE T11 ROADMAP SUITE (docs/roadmap.sexp, node `fixed-tables`, the eight
// union/optional/default capability rows) on the CPP leg. Every row here is an
// ordinary valid-data capability: the leg WRITES a value with non-default and
// boundary values and READS it back through its own fixed form, asserted, in a
// target the gate runs (`go test ./internal/codegen/cpptable/`). The proof is a
// probe: the schema is generated, compiled and run against the C++ reference,
// the same shape fixedversioning_test.go uses.
//
// A row the shared fixed-form gate (ir/fixedform.go, TableFixedSupported)
// refuses is named here with its reason and skipped BY NAME, never green and
// never red — a green folds an unrun row into a pass, a red blames the row for
// the gate.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
)

// ---- the harness ------------------------------------------------------------

// cppRunT11Probe writes `schema` to a temp dir, generates the unit's packet and
// table headers, compiles a probe and runs it. `root` is the fixed table name
// the probe saves and loads; `setup` is C++ that sets non-default values on the
// local `value`; `check` is C++ assertions run after the load, over the local
// `back`. A schema with no 128-bit field takes nothing from serialize.h, so the
// probe compiles against the generated headers alone.
func cppRunT11Probe(t *testing.T, schema, root, setup, check string) {
	t.Helper()
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "unit.schema")
	if err := os.WriteFile(schemaPath, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}

	u := loadUnit(t, schemaPath)
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatal(err)
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
		t.Fatal("no Table.h was generated")
	}

	pkg := u.Package
	probe := fmt.Sprintf(`#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "%s"

using namespace %s;

int main( void )
{
    static uint8_t buffer[1 << 20];
    static TableFixedEntry plan[4096];
    TableReport r;
    %s value;
    %sReset( value );
%s
    const int64_t need = %sFixedMeasure( 1 );
    const int64_t saved = %sFixedSave( &value, 1, buffer, need );
    if ( saved != need ) { printf( "save: need=%%lld got=%%lld\n", (long long) need, (long long) saved ); return 1; }
    %s back;
    %sReset( back );
    memset( &r, 0, sizeof( r ) );
    const int64_t n = %sFixedLoad( &back, 1, buffer, need, plan, 4096, NULL, &r );
    if ( n != 1 ) { printf( "load: n=%%lld reason=%%d refused=%%d malformed=%%d\n", (long long) n, (int) r.reason, (int) r.refused, (int) r.malformed ); return 1; }
%s
    return 0;
}
`, header, pkg, root, root, setup, root, root, root, root, root, check)

	if err := os.WriteFile(filepath.Join(outDir, "probe.cpp"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	args := []string{"-std=c++17", "-O1", "-I" + outDir, filepath.Join(outDir, "probe.cpp"), "-o", filepath.Join(outDir, "probe")}
	build := exec.Command(cxx, args...)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the probe did not compile: %v\n%s", err, out)
	}
	if out, err := exec.Command(filepath.Join(outDir, "probe")).CombinedOutput(); err != nil {
		t.Fatalf("the probe failed: %v\n%s", err, out)
	}
}

// ---- the rows ---------------------------------------------------------------

func TestFixedRoadmapCppT11UnionsOptionals(t *testing.T) {
	t.Run("union-field-arms/cpp/valid-data", func(t *testing.T) {
		t.Parallel()
		// DIRECT text/byte-buffer/array arms are excluded by the shared gate
		// (ir/fixedform.go TableFixedSupported); the supported shape is a direct
		// scalar arm and a struct arm carrying text/array. That subset is what
		// this probe holds: one scalar arm and one text-holding arm, both
		// written with non-default values and read back whole.
		cppRunT11Probe(t, `
package t11ufa
type Plain
{
    n int32
}
type Labelled
{
    label string(8)
    trail int32
}
union Pick
{
    plain Plain
    labelled Labelled
}
fixed table Root
{
    pick Pick
    tail int32
}
`, "Root", `
    value.tail = 42;
    value.pick.type = PickType::Labelled;
    strcpy( value.pick.labelled.label, "hello" );
    value.pick.labelled.label_length = 5;
    value.pick.labelled.trail = 999;
`, `
    if ( back.tail != 42 ) { printf( "tail: %d\n", back.tail ); return 1; }
    if ( back.pick.type != PickType::Labelled ) { printf( "arm: %d\n", (int) back.pick.type ); return 1; }
    if ( back.pick.labelled.label_length != 5 || strcmp( back.pick.labelled.label, "hello" ) != 0 )
        { printf( "label: %s\n", back.pick.labelled.label ); return 1; }
    if ( back.pick.labelled.trail != 999 ) { printf( "trail: %d\n", back.pick.labelled.trail ); return 1; }
    if ( r.clamped != 0 || r.malformed || r.refused ) { printf( "counters moved\n" ); return 1; }
`)
	})

	t.Run("payload-free-arms/cpp/valid-data", func(t *testing.T) {
		t.Parallel()
		t.Skipf("UNPORTED: a payload-free arm (v.F == nil) is refused by the shared fixed-form gate (ir/fixedform.go TableFixedSupported line `a void arm has no storage this walk can name`); laying out tag-only arms is an ir change outside this card's PATHS")
	})

	t.Run("union-arrays/cpp/valid-data", func(t *testing.T) {
		t.Parallel()
		cppRunT11Probe(t, `
package t11ua
type Cell
{
    n int32
}
type Alt
{
    m int32
}
union Pick
{
    cell Cell
    alt Alt
}
fixed table Root
{
    picks [..3]Pick
    tail int32
}
`, "Root", `
    value.tail = 7;
    value.picks_count = 3;
    value.picks[0].type = PickType::Cell;
    value.picks[0].cell.n = 11;
    value.picks[1].type = PickType::Alt;
    value.picks[1].alt.m = 22;
    value.picks[2].type = PickType::Cell;
    value.picks[2].cell.n = 33;
`, `
    if ( back.tail != 7 ) { printf( "tail: %d\n", back.tail ); return 1; }
    if ( back.picks_count != 3 ) { printf( "count: %d\n", back.picks_count ); return 1; }
    if ( back.picks[0].type != PickType::Cell || back.picks[0].cell.n != 11 )
        { printf( "picks[0]: t=%d n=%d\n", (int) back.picks[0].type, back.picks[0].cell.n ); return 1; }
    if ( back.picks[1].type != PickType::Alt || back.picks[1].alt.m != 22 )
        { printf( "picks[1]: t=%d m=%d\n", (int) back.picks[1].type, back.picks[1].alt.m ); return 1; }
    if ( back.picks[2].type != PickType::Cell || back.picks[2].cell.n != 33 )
        { printf( "picks[2]: t=%d n=%d\n", (int) back.picks[2].type, back.picks[2].cell.n ); return 1; }
    if ( r.clamped != 0 || r.malformed || r.refused ) { printf( "counters moved\n" ); return 1; }
`)
	})

	t.Run("optional-scalars/cpp/valid-data", func(t *testing.T) {
		t.Parallel()
		cppRunT11Probe(t, `
package t11os
enum Grade { Bronze, Gold }
fixed table Root
{
    num ?int32
    mark ?Grade
    tail int32
}
`, "Root", `
    value.tail = 5;
    value.num = -123456;
    value.num_present = true;
    value.mark = Grade::Gold;
    value.mark_present = true;
`, `
    if ( back.tail != 5 ) { printf( "tail: %d\n", back.tail ); return 1; }
    if ( !back.num_present || back.num != -123456 ) { printf( "num: present=%d v=%d\n", (int) back.num_present, back.num ); return 1; }
    if ( !back.mark_present || back.mark != Grade::Gold ) { printf( "mark: present=%d v=%d\n", (int) back.mark_present, (int) back.mark ); return 1; }
    if ( r.clamped != 0 || r.malformed || r.refused ) { printf( "counters moved\n" ); return 1; }
`)
	})

	t.Run("optional-nesting/cpp/valid-data", func(t *testing.T) {
		t.Parallel()
		cppRunT11Probe(t, `
package t11on
type Leaf
{
    v int32
    tag string(8)
}
fixed table Root
{
    opt ?Leaf
    tail int32
}
`, "Root", `
    value.tail = 9;
    value.opt_present = true;
    value.opt.v = 777;
    strcpy( value.opt.tag, "nested" );
    value.opt.tag_length = 6;
`, `
    if ( back.tail != 9 ) { printf( "tail: %d\n", back.tail ); return 1; }
    if ( !back.opt_present ) { printf( "opt absent\n" ); return 1; }
    if ( back.opt.v != 777 ) { printf( "opt.v: %d\n", back.opt.v ); return 1; }
    if ( back.opt.tag_length != 6 || strcmp( back.opt.tag, "nested" ) != 0 )
        { printf( "opt.tag: %s\n", back.opt.tag ); return 1; }
    if ( r.clamped != 0 || r.malformed || r.refused ) { printf( "counters moved\n" ); return 1; }
`)
	})

	t.Run("optional-arrays/cpp/valid-data", func(t *testing.T) {
		t.Parallel()
		cppRunT11Probe(t, `
package t11oa
fixed table Root
{
    cells ?[..3]int32
    tail int32
}
`, "Root", `
    value.tail = 3;
    value.cells_present = true;
    value.cells_count = 2;
    value.cells[0] = -100;
    value.cells[1] = 200;
`, `
    if ( back.tail != 3 ) { printf( "tail: %d\n", back.tail ); return 1; }
    if ( !back.cells_present ) { printf( "cells absent\n" ); return 1; }
    if ( back.cells_count != 2 ) { printf( "cells count: %d\n", back.cells_count ); return 1; }
    if ( back.cells[0] != -100 || back.cells[1] != 200 ) { printf( "cells: %d %d\n", back.cells[0], back.cells[1] ); return 1; }
    if ( r.clamped != 0 || r.malformed || r.refused ) { printf( "counters moved\n" ); return 1; }
`)
	})

	t.Run("scalar-defaults/cpp/valid-data", func(t *testing.T) {
		t.Parallel()
		cppRunT11Probe(t, `
package t11sd
enum Grade { Bronze, Gold }
fixed table Root
{
    grade Grade = Gold
    n int32 = 7
    tail int32
}
`, "Root", `
    // THE DECLARED DEFAULTS ARE WHAT A FRESH RECORD HOLDS (ALGORITHM §3 prefill):
    // a field the writer does not set rides its default, not zero.
    if ( value.grade != Grade::Gold ) { printf( "grade default: %d\n", (int) value.grade ); return 1; }
    if ( value.n != 7 ) { printf( "n default: %d\n", value.n ); return 1; }
    value.tail = 1;
    value.n = 424242;   // a non-default value overrides the default on write
`, `
    if ( back.tail != 1 ) { printf( "tail: %d\n", back.tail ); return 1; }
    if ( back.n != 424242 ) { printf( "n: %d\n", back.n ); return 1; }
    if ( back.grade != Grade::Gold ) { printf( "grade: %d\n", (int) back.grade ); return 1; }
    if ( r.clamped != 0 || r.malformed || r.refused ) { printf( "counters moved\n" ); return 1; }
`)
	})

	t.Run("text-bytes-flags-defaults/cpp/valid-data", func(t *testing.T) {
		t.Parallel()
		cppRunT11Probe(t, `
package t11tb
flags Caps { Jump, Crouch, Fly }
fixed table Root
{
    name string(8) = "hi"
    tag bytes(4) = "ab"
    caps Caps = { Jump, Fly }
    tail int32
}
`, "Root", `
    if ( value.name_length != 2 || strcmp( value.name, "hi" ) != 0 )
        { printf( "name default: %s\n", value.name ); return 1; }
    if ( value.tag_length != 2 || value.tag[0] != 'a' || value.tag[1] != 'b' )
        { printf( "tag default\n" ); return 1; }
    value.tail = 2;
    value.caps = 0;   // a non-default flags value overrides the default
`, `
    if ( back.tail != 2 ) { printf( "tail: %d\n", back.tail ); return 1; }
    if ( back.caps != 0 ) { printf( "caps: %llu\n", (unsigned long long) back.caps ); return 1; }
    if ( back.name_length != 2 || strcmp( back.name, "hi" ) != 0 )
        { printf( "name: %s\n", back.name ); return 1; }
    if ( back.tag_length != 2 || back.tag[0] != 'a' || back.tag[1] != 'b' )
        { printf( "tag\n" ); return 1; }
    if ( r.clamped != 0 || r.malformed || r.refused ) { printf( "counters moved\n" ); return 1; }
`)
	})
}
