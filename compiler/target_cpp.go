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

func (cppTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	// the TABLE side of a payload-free arm is this target's (§2.6); the PACKET
	// side is a named follow-on, so a union with one outside a table closure
	// is refused here
	if err := refusePacketVoidArms(u, "cpp"); err != nil {
		return nil, err
	}
	files, err := cpp.Generate(u)
	if err != nil {
		return nil, err
	}
	// units that declare tables ALSO get <Base>Table.h per file — the
	// TABLE-wire codecs (docs/SPEC-TABLES.md); a table-free unit's output is
	// byte-identical to what the packet emitter alone produces
	tables, err := cpptable.Generate(u)
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
	registerOptionalArrayCarrier("cpp")
	registerMapCarrier("cpp") // the C++ reference carries the map codecs (docs/SPEC-TABLES.md §2.8)
}
