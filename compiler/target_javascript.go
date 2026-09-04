// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language"). The
// file is named for the alias, because Go reads a _js suffix as a GOOS build
// constraint and would leave target_js.go out of every other build.
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/jstable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// jsTarget emits JavaScript ES modules.
type jsTarget struct{}

func (jsTarget) Names() []string { return []string{"js", "javascript"} }

func (jsTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	if err := refuseTableArms(u, "js"); err != nil {
		return nil, err
	}
	if err := refuseUnionArrays(u, "js"); err != nil {
		return nil, err
	}
	if err := refuseOptionalArrays(u, "js"); err != nil {
		return nil, err
	}
	files, err := js.Generate(u)
	if err != nil {
		return nil, err
	}
	// units that declare tables ALSO get <Base>Table.js per file — the
	// TABLE-wire codecs, FIXED class — plus the two accelerators'
	// <Base>Block.js and <Base>Cook.js READERS (docs/SPEC-TABLES.md); a
	// table-free unit's output is byte-identical to what the packet emitter
	// alone produces
	tables, err := jstable.Generate(u)
	if err != nil {
		return nil, err
	}
	for name, data := range tables {
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("generated file %s is claimed twice — a schema file named <X>.schema beside <X minus Table>.schema with tables collides on the Table module; rename one file (docs/SPEC-TABLES.md)", name)
		}
		files[name] = data
	}
	return files, nil
}

func init() { registerBuiltin(jsTarget{}, true, false, false) }
