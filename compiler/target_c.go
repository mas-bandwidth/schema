// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
package compiler

import (
	"fmt"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/ctable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// cTarget emits C99 (SPEC §6.1).
type cTarget struct{}

func (cTarget) Names() []string { return []string{"c"} }

func (cTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	// WIDE TEXT FIRST, ahead of every form refusal: it is a STORAGE
	// construct (SPEC §4.12), so a target that has not laid out the
	// member cannot emit the field under any form, and naming that is
	// more use to a port author than naming a form the field sits in.
	if err := refuseWideText(u, "c"); err != nil {
		return nil, err
	}
	if err := refuseUnported(u, "c"); err != nil {
		return nil, err
	}
	if err := refuseOptionalArrays(u, "c"); err != nil {
		return nil, err
	}
	if err := refuseMaps(u, "c"); err != nil {
		return nil, err
	}
	if err := refuseLists(u, "c"); err != nil {
		return nil, err
	}
	files, err := cgen.Generate(u)
	if err != nil {
		return nil, err
	}
	// units that declare tables ALSO get <Base>Table.h / <Base>Table.c per
	// file — the TABLE-wire codecs (docs/SPEC-TABLES.md); a table-free unit's
	// output is byte-identical to what the packet emitter alone produces
	tables, err := ctable.Generate(u)
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

func init() { registerBuiltin(cTarget{}, true, false, false, false) }
