package cpptable

// TestNestedTableUnionDefinitionOrder and its companions pin the C++ table
// emitter's by-value dependency closure across nested table-armed unions and
// structs: every union and struct shape, extent walk, and fixed-form body must
// be defined before any by-value holder uses it, regardless of declaration
// order in the schema file or whether a file declares unions alone.

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const nestedUnionOrderSchema = `package deepunion

table Leaf
{
    n int32
}

union L1
{
    a Leaf
}

union L2
{
    a L1
}

union L3
{
    a L2
}

table Root
{
    pick L3
}
`

const nestedUnionReverseExtentSchema = `package deepextent

table Root
{
    pick L3
    by_key map[int32]L3
}

union L3
{
    a L2
}

union L2
{
    a L1
}

union L1
{
    a Leaf
}

table Leaf
{
    xs []int32
}
`

const nestedUnionReverseFixedSchema = `package deepfixed

fixed table Root
{
    pick L2
}

union L2
{
    a L1
}

union L1
{
    a Leaf
}

fixed table Leaf
{
    n int32 | min = 0, max = 10
    f float32
}
`

const nestedUnionCrossFileLeafSchema = `package deepcross

table Leaf
{
    n int32
}
`

const nestedUnionCrossFileRootSchema = `package deepcross

table Root
{
    pick L3
}
`

const nestedUnionCrossFileUnionsSchema = `package deepcross

union L3
{
    a L2
}

union L2
{
    a L1
}

union L1
{
    a Leaf
}
`

func TestNestedTableUnionDefinitionOrder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "DeepUnion.schema")
	if err := os.WriteFile(path, []byte(nestedUnionOrderSchema), 0o644); err != nil {
		t.Fatal(err)
	}

	body := `    Root src;
    src.pick.type = L3Type::A;
    src.pick.a.type = L2Type::A;
    src.pick.a.a.type = L1Type::A;
    src.pick.a.a.a.n = 42;
    uint8_t buf[256] = {};
    const int64_t n = RootSave( &src, buf, (int64_t) sizeof( buf ) );
    if ( n <= 0 ) { return 1; }
    alignas( 8 ) uint8_t region[256] = {};
    TableReport rep;
    const Root * dst = RootLoad( region, (int64_t) sizeof( region ), buf, n, &rep );
    if ( dst == NULL ) { return 2; }
    if ( dst->pick.type != L3Type::A || dst->pick.a.type != L2Type::A || dst->pick.a.a.type != L1Type::A || dst->pick.a.a.a.n != 42 ) { return 3; }
`
	if out, err := cppRunUnit(t, path, "deepunion", body, nil); err != nil {
		t.Fatalf("unions nested three deep by value did not compile and round-trip:\n%s\n%v", out, err)
	}
}

func TestNestedTableUnionReverseExtentOrder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	extentPath := filepath.Join(dir, "DeepExtent.schema")
	if err := os.WriteFile(extentPath, []byte(nestedUnionReverseExtentSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := cppRunUnit(t, extentPath, "deepextent", "", nil); err != nil {
		t.Fatalf("top-down nested unions with extent table did not compile:\n%s\n%v", out, err)
	}
}

func TestNestedTableUnionReverseFixedOrder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fixedPath := filepath.Join(dir, "DeepFixed.schema")
	if err := os.WriteFile(fixedPath, []byte(nestedUnionReverseFixedSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := cppRunUnit(t, fixedPath, "deepfixed", "", nil); err != nil {
		t.Fatalf("top-down nested unions with fixed table did not compile:\n%s\n%v", out, err)
	}
}

func TestNestedTableUnionOnlyFileOrder(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "an external toolchain")

	var sources []check.SourceFile
	for _, spec := range []struct{ name, src string }{
		{"Leaf.schema", nestedUnionCrossFileLeafSchema},
		{"Unions.schema", nestedUnionCrossFileUnionsSchema},
		{"Root.schema", nestedUnionCrossFileRootSchema},
	} {
		ast, perrs := parser.Parse(spec.name, []byte(spec.src))
		if len(perrs) > 0 || ast == nil {
			t.Fatalf("parse %s: %v", spec.name, perrs)
		}
		sources = append(sources, check.SourceFile{
			Path:  spec.name,
			Name:  spec.name,
			Base:  strings.TrimSuffix(spec.name, ".schema"),
			Bytes: []byte(spec.src),
			AST:   ast,
		})
	}
	u, cerrs := check.Unit(sources)
	if len(cerrs) > 0 || u == nil {
		t.Fatalf("check: %v", cerrs)
	}

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
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(outDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	probe := `#include "UnionsTable.h"
int main( void )
{
    deepcross::L3 v;
    v.type = deepcross::L3Type::A;
    v.a.type = deepcross::L2Type::A;
    v.a.a.type = deepcross::L1Type::A;
    v.a.a.a.n = 7;
    return v.a.a.a.n == 7 ? 0 : 1;
}
`
	probePath := filepath.Join(outDir, "probe.cpp")
	if err := os.WriteFile(probePath, []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	bin := filepath.Join(outDir, "probe")
	if out, err := exec.Command(cxx, "-std=c++17", "-Wall", "-Wextra", "-Werror", "-I"+outDir, probePath, "-o", bin).CombinedOutput(); err != nil {
		t.Fatalf("union-only nested header did not compile:\n%s\n%v", out, err)
	}
	if out, err := exec.Command(bin).CombinedOutput(); err != nil {
		t.Fatalf("union-only nested probe failed:\n%s\n%v", out, err)
	}
}
