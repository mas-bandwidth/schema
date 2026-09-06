// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/csharp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cstable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// csTarget emits C#.
type csTarget struct{}

func (csTarget) Names() []string { return []string{"cs", "csharp"} }

func (csTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	// WIDE TEXT FIRST, ahead of every form refusal: it is a STORAGE
	// construct (SPEC §4.12), so a target that has not laid out the
	// member cannot emit the field under any form, and naming that is
	// more use to a port author than naming a form the field sits in.
	if err := refuseWideText(u, "cs"); err != nil {
		return nil, err
	}
	if err := refuseUnported(u, "cs"); err != nil {
		return nil, err
	}
	if err := refuseOptionalArrays(u, "cs"); err != nil {
		return nil, err
	}
	if err := refuseMaps(u, "cs"); err != nil {
		return nil, err
	}
	if err := refuseLists(u, "cs"); err != nil {
		return nil, err
	}
	files, err := csharp.Generate(u)
	if err != nil {
		return nil, err
	}
	// units that declare tables ALSO get <Base>Table.cs per file — the
	// TABLE-wire codecs, FIXED class (docs/SPEC-TABLES.md); a table-free unit's
	// output is byte-identical to what the packet emitter alone produces
	tables, err := cstable.Generate(u)
	if err != nil {
		return nil, err
	}
	for name, data := range tables {
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("generated file %s is claimed twice — a schema file named <X>.schema beside <X minus Table>.schema with tables collides on the Table source; rename one file (docs/SPEC-TABLES.md)", name)
		}
		files[name] = data
	}
	return files, nil
}

func init() {
	registerBuiltin(csTarget{}, true, false, false, false)
	registerPacketVoidArmCarrier("cs")
}
