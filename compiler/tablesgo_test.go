// The tables tests of ONE language, in its own file so a port adds a file and
// edits no shared one (docs/CONTRIBUTING.md, "Adding a language").
package compiler

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablenames"
)

// TestGoEmitsTableSources: the go target adds <Base>Table.go beside the packet
// sources for a unit with tables, and adds NOTHING for one without — the same
// contract the cpp and cs targets hold.
func TestGoEmitsTableSources(t *testing.T) {
	c := New()
	with, err := c.Generate(unitFromSource(t, tableSrc), "go", Options{})
	if err != nil {
		t.Fatalf("--lang go: %v", err)
	}
	if _, ok := with["ProbeTable.go"]; !ok {
		t.Fatalf("--lang go emitted no ProbeTable.go for a unit with tables; got %d files", len(with))
	}
	without, err := c.Generate(unitFromSource(t, packetSrc), "go", Options{})
	if err != nil {
		t.Fatalf("--lang go: %v", err)
	}
	for name := range without {
		if strings.HasSuffix(name, "Table.go") || strings.HasSuffix(name, "Block.go") ||
			strings.HasSuffix(name, "Cook.go") || strings.HasSuffix(name, "TableJson.go") {
			t.Errorf("--lang go emitted %s for a table-free unit", name)
		}
	}
}

// A variable Go unit carries wire codecs, canonical rows and the builder.
func TestGoEmitsPointeredTables(t *testing.T) {
	u := unitFromSource(t, packetSrc+"\ntable Node { value int32\nnext *Node\n}\n")
	files, err := New().Generate(u, "go", Options{})
	if err != nil {
		t.Fatal(err)
	}
	for file, need := range map[string][]string{
		"ProbeTable.go":     {"type Node = NodeRow", "NodeLoadMeasure", "NodeLoad(", "NodeBuilder", "type TableArena struct"},
		"ProbeCook.go":      {"NodeOpen", "type NodeRow struct"},
		"ProbeTableJson.go": {"NodeFromJson", "&node"},
	} {
		for _, name := range need {
			if !strings.Contains(string(files[file]), name) {
				t.Errorf("%s missing %s", file, name)
			}
		}
	}
}

// TestTablesMoveNoGeneratedGoByte is the Go half of the same proof: adding a
// table changes not one byte of the non-table generated Go, and grows only the
// four Go table sources — the codecs, the two accelerators, and the unit's one
// text walk.
func TestTablesMoveNoGeneratedGoByte(t *testing.T) {
	c := New()
	with, err := c.Generate(unitFromSource(t, tableSrc), "go", Options{})
	if err != nil {
		t.Fatal(err)
	}
	without, err := c.Generate(unitFromSource(t, packetSrc), "go", Options{})
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range without {
		got, ok := with[name]
		if !ok {
			t.Errorf("file %s disappeared when a table was added", name)
			continue
		}
		if string(got) != string(data) {
			t.Errorf("file %s changed when a table was added — tables must move no packet byte", name)
		}
	}
	for name := range with {
		if _, ok := without[name]; ok {
			continue
		}
		if !strings.HasSuffix(name, "Table.go") && !strings.HasSuffix(name, "Block.go") &&
			!strings.HasSuffix(name, "Cook.go") && !strings.HasSuffix(name, "TableJson.go") && !strings.HasSuffix(name, "View.go") {
			t.Errorf("adding a table grew unexpected non-table file %s", name)
		}
	}
}

// goRuntimeSrc is runtimeSrc plus a table carrying a UNION field, which is the
// one construct the Go runtime's arms table needs to exist. It is a THIRD
// table rather than a union added to an existing one, because a union in a
// closure keeps that table out of both accelerators (§19.3) and the scan below
// has to see the block and cook halves too.
const goRuntimeSrc = runtimeSrc + `
type Boost
{
    power int32 = 0
}

union Effect
{
    boost Boost
}

table Effected
{
    effect Effect
}
`

// TestTableRuntimeNamesAreClaimedGo is the §11 promise's GO half, and the
// reason it is a second test rather than a parameter is that the two backends
// define overlapping but different sets: the scan asserts both directions
// against tablenames.Go, so a name the Go emitter defines and nobody registered
// fails here, and a name registered for Go that nothing emits fails here too.
//
// The forward scan claims package declarations, since methods and fields do
// not consume schema names. The reverse scan requires an actual declaration,
// including scoped members recorded by DefinedBy; a selector use is not one.
//
// IT MATCHES BOTH CASES, and that is the whole reason this scan is not the C#
// one copied. Go is the first backend whose runtime puts UNEXPORTED names at
// package scope, and unexported is not private: a Go package is one namespace,
// so `const tableJsonMaxDepth = 5` in a schema is a redeclaration and the unit
// does not compile. A PascalCase-only scan is blind to exactly what this port
// adds, which is how the hole reached a reviewer.
func TestTableRuntimeNamesAreClaimedGo(t *testing.T) {
	files := map[string][]byte{}
	for i, source := range []string{
		goRuntimeSrc,
		goRuntimeSrc + "\ntable Graph {head *Graph\ndata *bytes\ncaption *string\n}\n",
		goRuntimeSrc + "\ntable Lists {rows []int32}\n",
		goRuntimeSrc + "\ntable Wide {label wstring(16)\n}\n",
	} {
		generated, err := New().Generate(unitFromSource(t, source), "go", Options{})
		if err != nil {
			t.Fatal(err)
		}
		for name, data := range generated {
			files[fmt.Sprintf("%d/%s", i, name)] = data
		}
	}
	// The build version and announcement verbs are the registered runtime
	// names without a Table prefix (docs/SPEC-TABLES.md §20 and §3.3).
	ident := regexp.MustCompile(`\b(?:[Tt]able[A-Za-z0-9_]*|BuildVersion|Announce(?:Measure|Read)?)\b`)
	emitted := map[string]bool{}
	declared := map[string]bool{}
	for name, data := range files {
		file, err := parser.ParseFile(token.NewFileSet(), name, data, 0)
		if err != nil {
			t.Fatal(err)
		}
		record := func(name string) {
			declared[name] = true
			if ident.MatchString(name) && ident.FindString(name) == name {
				emitted[name] = true
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.FuncDecl:
				if n.Recv != nil {
					declared[n.Name.Name] = true
				}
			case *ast.StructType:
				for _, field := range n.Fields.List {
					for _, name := range field.Names {
						declared[name.Name] = true
					}
				}
			}
			return true
		})
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					record(d.Name.Name)
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch v := spec.(type) {
					case *ast.TypeSpec:
						record(v.Name.Name)
					case *ast.ValueSpec:
						for _, n := range v.Names {
							record(n.Name)
						}
					}
				}
			}
		}
	}
	if len(emitted) == 0 {
		t.Fatal("the scan found no Table* identifier in the emitted Go at all — the scan, not the registry, is what broke")
	}
	names := make([]string, 0, len(emitted))
	for name := range emitted {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !tablenames.Registered(name) {
			t.Errorf("the Go table emitter emits %s and internal/tablenames does not register it — "+
				"a schema declaring that name would generate Go that does not compile; register it "+
				"(with the backends that define it) in internal/tablenames", name)
		}
	}
	for _, name := range tablenames.DefinedBy(tablenames.Go) {
		if !declared[name] {
			t.Errorf("internal/tablenames says the Go backend defines %s, but nothing in the emitted "+
				"Go declares it — drop the registration or fix the backend; a claim nothing needs takes "+
				"a name away from every schema for free", name)
		}
	}
}
