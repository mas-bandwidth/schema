// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/rust"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/rusttable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// rustTarget emits Rust.
type rustTarget struct{}

func (rustTarget) Names() []string { return []string{"rust"} }

func (rustTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	if err := refuseUnported(u, "rust"); err != nil {
		return nil, err
	}
	if err := refuseOptionalArrays(u, "rust"); err != nil {
		return nil, err
	}
	if err := refuseMaps(u, "rust"); err != nil {
		return nil, err
	}
	if err := refuseLists(u, "rust"); err != nil {
		return nil, err
	}
	files, err := rust.Generate(u)
	if err != nil {
		return nil, err
	}
	// units that declare tables ALSO get the table modules — the TABLE-wire
	// codecs, the reflection descriptors, the text form and the two
	// accelerators (docs/SPEC-TABLES.md); a table-free unit's output is
	// byte-identical to what the packet emitter alone produces.
	tables, err := rusttable.Generate(u)
	if err != nil {
		return nil, err
	}
	if len(tables) == 0 {
		return files, nil
	}
	for name, data := range tables {
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("generated file %s is claimed twice — a schema file whose lowered basename collides with a generated table module; rename one file (docs/SPEC-TABLES.md §11)", name)
		}
		files[name] = data
	}
	// the crate root is a generated index, so it has to declare them
	lib, err := rust.Lib(u, rusttable.Modules(tables))
	if err != nil {
		return nil, err
	}
	files["lib.rs"] = lib
	return files, nil
}

func init() {
	registerBuiltin(rustTarget{}, true, false, false, false)
	registerPacketVoidArmCarrier("rust")
}
