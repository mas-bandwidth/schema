// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/elixir"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/elixirtable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// elixirTarget emits Elixir 1.20.
type elixirTarget struct{}

func (elixirTarget) Names() []string { return []string{"elixir"} }

func (elixirTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	// WIDE TEXT FIRST, ahead of every form refusal: it is a STORAGE
	// construct (SPEC §4.12), so a target that has not laid out the
	// member cannot emit the field under any form, and naming that is
	// more use to a port author than naming a form the field sits in.
	if err := refuseWideText(u, "elixir"); err != nil {
		return nil, err
	}
	if err := refuseUnported(u, "elixir"); err != nil {
		return nil, err
	}
	if err := refuseOptionalArrays(u, "elixir"); err != nil {
		return nil, err
	}
	if err := refuseMaps(u, "elixir"); err != nil {
		return nil, err
	}
	if err := refuseLists(u, "elixir"); err != nil {
		return nil, err
	}
	files, err := elixir.Generate(u)
	if err != nil {
		return nil, err
	}
	// units that declare tables ALSO get <Base>Table.ex per file — the
	// TABLE-wire codecs, the reflection descriptors, the text form and the two
	// accelerators' READ side (docs/SPEC-TABLES.md); a table-free unit's output
	// is byte-identical to what the packet emitter alone produces.
	tables, err := elixirtable.Generate(u)
	if err != nil {
		return nil, err
	}
	for name, data := range tables {
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("generated file %s is claimed twice — a schema file whose basename collides with a generated table module; rename one file (docs/SPEC-TABLES.md §11)", name)
		}
		files[name] = data
	}
	return files, nil
}

func init() {
	registerBuiltin(elixirTarget{}, true, false, false, false)
}
