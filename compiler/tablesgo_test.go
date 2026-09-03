// The tables tests of ONE language, in its own file so a port adds a file and
// edits no shared one (docs/CONTRIBUTING.md, "Adding a language").
package compiler

import (
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

// TestGoRefusesPointeredTables: the Go variable-class refusal is a refusal of
// the WIRE SURFACE and of nothing else (docs/SPEC-TABLES.md §11), exactly as
// the C# one is. The two ACCELERATORS need no codec — a block and a cook are
// POINTED AT, not parsed — so both are emitted, the cook's <Root>Open opens
// this unit's cooked assets in full, and every source the unit does emit opens
// with a banner naming each refused table and the follow-on.
func TestGoRefusesPointeredTables(t *testing.T) {
	u := unitFromSource(t, packetSrc+`
table Node
{
    value int32
    next  *Node
}
`)
	files, err := New().Generate(u, "go", Options{})
	if err != nil {
		t.Fatalf("--lang go refused a pointered unit outright: %v", err)
	}
	for name := range files {
		if strings.HasSuffix(name, "Table.go") {
			t.Errorf("--lang go emitted %s for a pointered unit — the wire surface is refused by name", name)
		}
	}
	cook, ok := files["ProbeCook.go"]
	if !ok {
		t.Fatal("--lang go emitted no ProbeCook.go: the COOK needs no wire codec and must still be emitted")
	}
	if !strings.Contains(string(cook), "NodeOpen") {
		t.Error("ProbeCook.go carries no NodeOpen — a root is any table, and a pointered unit's cooks open in full")
	}
	for name, data := range files {
		if !strings.HasSuffix(name, "Block.go") && !strings.HasSuffix(name, "Cook.go") {
			continue
		}
		if !strings.Contains(string(data), "REFUSED, BY NAME") || !strings.Contains(string(data), "Node") {
			t.Errorf("%s does not carry the variable-class banner naming Node", name)
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
			!strings.HasSuffix(name, "Cook.go") && !strings.HasSuffix(name, "TableJson.go") {
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
// The scan is the C# one's, shape-independent for the same reason: it collects
// every table-prefixed identifier in the emitted text, declaration or use, and
// requires the whole set to be registered. Line comments are stripped first —
// prose is not an identifier.
//
// IT MATCHES BOTH CASES, and that is the whole reason this scan is not the C#
// one copied. Go is the first backend whose runtime puts UNEXPORTED names at
// package scope, and unexported is not private: a Go package is one namespace,
// so `const tableJsonMaxDepth = 5` in a schema is a redeclaration and the unit
// does not compile. A PascalCase-only scan is blind to exactly what this port
// adds, which is how the hole reached a reviewer.
func TestTableRuntimeNamesAreClaimedGo(t *testing.T) {
	files, err := New().Generate(unitFromSource(t, goRuntimeSrc), "go", Options{})
	if err != nil {
		t.Fatal(err)
	}
	// BuildVersion rides in the alternation because it is the one registered
	// name that is not a table spelling, and the Go backend defines it
	// (docs/SPEC-TABLES.md §20).
	ident := regexp.MustCompile(`\b(?:[Tt]able[A-Za-z0-9_]*|BuildVersion)\b`)
	emitted := map[string]bool{}
	for _, data := range files {
		for line := range strings.SplitSeq(string(data), "\n") {
			if i := strings.Index(line, "//"); i >= 0 {
				line = line[:i]
			}
			for _, m := range ident.FindAllString(line, -1) {
				emitted[m] = true
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
		if !emitted[name] {
			t.Errorf("internal/tablenames says the Go backend defines %s, but nothing in the emitted "+
				"Go names it — drop the registration or fix the backend; a claim nothing needs takes "+
				"a name away from every schema for free", name)
		}
	}
}
