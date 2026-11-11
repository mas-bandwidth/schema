package cpptable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// A table-armed union whose arm holds another union by value is only one
// level of the dependency the generated header has to respect: an arm's
// payload may itself hold a table or a further union. Each header below is
// emitted and compiled by the host C++ compiler, because the defect is an
// ORDERING one — "'L2' does not name a type" — and a textual assertion alone
// can miss the next level of it.

func nestedTableUnionCompile(t *testing.T, sources map[string]string, header string) {
	t.Helper()
	var files []check.SourceFile
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		src := sources[name]
		ast, perrs := parser.Parse(name, []byte(src))
		if len(perrs) > 0 {
			t.Fatalf("parse %s: %v", name, perrs[0])
		}
		files = append(files, check.SourceFile{
			Path: name, Name: name, Base: strings.TrimSuffix(name, ".schema"),
			Bytes: []byte(src), AST: ast,
		})
	}
	u, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	generated, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(generated, tables)
	dir := t.TempDir()
	for name, data := range generated {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	probe := fmt.Sprintf("#include <cstdint>\n#include \"%s\"\nint main( void ) { return 0; }\n", header)
	probePath := filepath.Join(dir, "probe.cpp")
	if err := os.WriteFile(probePath, []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	args := []string{"-std=c++17", "-O0", "-I" + dir, probePath, "-o", filepath.Join(dir, "probe")}
	if out, err := exec.Command(cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("the generated header does not compile: %v\n%s", err, out)
	}
}

func TestNestedTableUnionDefinitionOrder(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C++ compiler (the leg's own toolchain)")
	nestedTableUnionCompile(t, map[string]string{"Probe.schema": `package nesteddef

type Leaf { n int32 }

union L3 {
    leaf Leaf
    b int32
}

union L2 { a L3 }
union L1 { a L2 }

table Root { pick L1 }
`}, "ProbeTable.h")
}

func TestNestedTableUnionReverseExtentOrder(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C++ compiler (the leg's own toolchain)")
	nestedTableUnionCompile(t, map[string]string{"Probe.schema": `package nestedrev

union L1 { a L2 }
union L2 { a L3 }

type Leaf { n int32 }

union L3 {
    leaf Leaf
    b int32
}

table Root {
    pick L1
    items []int32
}
`}, "ProbeTable.h")
}

func TestNestedTableUnionReverseFixedOrder(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C++ compiler (the leg's own toolchain)")
	nestedTableUnionCompile(t, map[string]string{"Probe.schema": `package nestedfix

union L1 { a L2 }
union L2 {
    a int32
    b uint16
}

fixed table Root { pick L1 }
`}, "ProbeTable.h")
}

func TestNestedTableUnionOnlyFileOrder(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C++ compiler (the leg's own toolchain)")
	nestedTableUnionCompile(t, map[string]string{
		"A.schema": `package nestedonly

table Root { pick L1 }
`,
		"B.schema": `package nestedonly

union L1 { a L2 }
union L2 { a L3 }

type Leaf { n int32 }

union L3 {
    leaf Leaf
    b int32
}
`,
	}, "ATable.h")
}
