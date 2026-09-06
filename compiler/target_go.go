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
	// WIDE TEXT FIRST, ahead of every form refusal: it is a STORAGE
	// construct (SPEC §4.12), so a target that has not laid out the
	// member cannot emit the field under any form, and naming that is
	// more use to a port author than naming a form the field sits in.
	if err := refuseWideText(u, "go"); err != nil {
		return nil, err
	}
	if err := refuseUnported(u, "go"); err != nil {
		return nil, err
	}
	if err := refuseOptionalArrays(u, "go"); err != nil {
		return nil, err
	}
	if err := refuseMaps(u, "go"); err != nil {
		return nil, err
	}
	if err := refuseLists(u, "go"); err != nil {
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

func init() {
	registerBuiltin(goTarget{}, true, false, false, false)
	registerPacketVoidArmCarrier("go")
	registerPacketValueDefaultCarrier("go")
}
