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
	// WIDE TEXT FIRST, ahead of every form refusal: it is a STORAGE
	// construct (SPEC §4.12), so a target that has not laid out the
	// member cannot emit the field under any form, and naming that is
	// more use to a port author than naming a form the field sits in.
	if err := refuseWideText(u, "dart"); err != nil {
		return nil, err
	}
	if err := refuseUnported(u, "dart"); err != nil {
		return nil, err
	}
	if err := refuseOptionalArrays(u, "dart"); err != nil {
		return nil, err
	}
	if err := refuseMaps(u, "dart"); err != nil {
		return nil, err
	}
	if err := refuseLists(u, "dart"); err != nil {
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

func init() {
	registerBuiltin(dartTarget{}, true, false, false, false)
}
