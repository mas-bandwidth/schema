// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpptable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// cppTarget emits C++17.
type cppTarget struct{}

func (cppTarget) Names() []string { return []string{"cpp"} }

// Generate is the case where the caller read no lock: cpptable then opens the
// unit's lock itself, as it did before #921.
func (t cppTarget) Generate(u *ir.Unit, opts Options) (map[string][]byte, error) {
	return t.GenerateLineage(u, opts, nil)
}

// GenerateLineage takes the lock the DRIVER opened (compiler/lineage.go) rather
// than opening it a second time. The C++ reference is the one backend that still
// INTERPRETS the lock itself: its lineage also comes from sibling schema files by
// filename convention, and its floor from a test-only map, which is §5.8 row 1's
// interim. So the OPEN is shared here and the interpretation is not, yet — the
// entries of §5.9 #1 are what it will take the day the convention goes away.
func (cppTarget) GenerateLineage(u *ir.Unit, _ Options, lineage *FixedLineage) (map[string][]byte, error) {
	files, err := cpp.Generate(u)
	if err != nil {
		return nil, err
	}
	// units that declare tables ALSO get <Base>Table.h per file — the
	// TABLE-wire codecs (docs/SPEC-TABLES.md); a table-free unit's output is
	// byte-identical to what the packet emitter alone produces
	tables, err := cpptable.GenerateLineage(u, lineage.Lock())
	if err != nil {
		return nil, err
	}
	for name, data := range tables {
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("generated file %s is claimed twice — a schema file named <X>.schema beside <X minus Table>.schema with tables collides on the Table header; rename one file (docs/SPEC-TABLES.md)", name)
		}
		files[name] = data
	}
	return files, nil
}

func init() {
	registerBuiltin(cppTarget{}, true, true, true, true)
	registerWideTextCarrier("cpp") // the C++ reference carries wstring(N) on the packet wire (SPEC §4.12)
	registerOptionalArrayCarrier("cpp")
	registerMapCarrier("cpp")  // the C++ reference carries the map codecs (docs/SPEC-TABLES.md §2.8)
	registerListCarrier("cpp") // and the unbounded array codec (docs/SPEC-TABLES.md §2.9)
}
