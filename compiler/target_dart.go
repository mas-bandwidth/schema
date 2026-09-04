// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/darttable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// dartTarget emits Dart 3.
type dartTarget struct{}

func (dartTarget) Names() []string { return []string{"dart"} }

func (dartTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	if err := refuseTableArms(u, "dart"); err != nil {
		return nil, err
	}
	if err := refuseUnionArrays(u, "dart"); err != nil {
		return nil, err
	}
	if err := refuseOptionalArrays(u, "dart"); err != nil {
		return nil, err
	}
	files, err := dart.Generate(u)
	if err != nil {
		return nil, err
	}
	// units that declare tables ALSO get <Base>Table.dart per file — the
	// TABLE-wire codecs, the reflection descriptors and the text form
	// (docs/SPEC-TABLES.md); a table-free unit's output is byte-identical to what
	// the packet emitter alone produces
	tables, err := darttable.Generate(u)
	if err != nil {
		return nil, err
	}
	for name, data := range tables {
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("generated file %s is claimed twice — a schema file named <X>.schema beside <X minus Table>.schema with tables collides on the Table library; rename one file (docs/SPEC-TABLES.md)", name)
		}
		files[name] = data
	}
	return files, nil
}

func init() { registerBuiltin(dartTarget{}, true, false, false) }
