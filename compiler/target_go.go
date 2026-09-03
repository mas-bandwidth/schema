// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// goTarget emits Go.
type goTarget struct{}

func (goTarget) Names() []string { return []string{"go"} }

func (goTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	if err := refuseTableArms(u, "go"); err != nil {
		return nil, err
	}
	if err := refuseUnionArrays(u, "go"); err != nil {
		return nil, err
	}
	files, err := golang.Generate(u)
	if err != nil {
		return nil, err
	}
	// units that declare tables ALSO get <Base>Table.go per file — the
	// TABLE-wire codecs and the reflection descriptors (SPEC-TABLES.md); a
	// table-free unit's output is byte-identical to what the packet emitter
	// alone produces
	tables, err := gotable.Generate(u)
	if err != nil {
		return nil, err
	}
	for name, data := range tables {
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("generated file %s is claimed twice — a schema file named <X>.schema beside <X minus Table>.schema with tables collides on the Table source; rename one file (SPEC-TABLES.md)", name)
		}
		files[name] = data
	}
	return files, nil
}

func init() { registerBuiltin(goTarget{}, true, false, false) }
