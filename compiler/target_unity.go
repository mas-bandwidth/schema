// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/unity"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/unitytable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// unityTarget emits Unity C#: C# 9 over netstandard2.1, unmanaged structs,
// IL2CPP and Burst safe (docs/UNITY.md). It is a generator of its own beside
// --lang cs, which is the modern .NET one; the two share the IR and the name
// mapping and no emitter. The packet types come from internal/codegen/unity
// and the table surface from internal/codegen/unitytable, into the same
// namespace and the same partial Schema class.
type unityTarget struct{}

func (unityTarget) Names() []string { return []string{"unity"} }

func (unityTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	// Wide table kinds first, so a unit that declares them is refused with
	// the shared diagnostic before any other follow-on. Then each construct
	// this target does not carry, by name.
	if err := ir.RefuseWideTableKinds(u, "unity"); err != nil {
		return nil, err
	}
	if err := refuseUnported(u, "unity"); err != nil {
		return nil, err
	}
	if err := refuseOptionalArrays(u, "unity"); err != nil {
		return nil, err
	}
	if err := refuseMaps(u, "unity"); err != nil {
		return nil, err
	}
	if err := refuseLists(u, "unity"); err != nil {
		return nil, err
	}
	files, err := unity.Generate(u)
	if err != nil {
		return nil, err
	}
	tables, err := unitytable.Generate(u)
	if err != nil {
		return nil, err
	}
	for name, data := range tables {
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("generated file %s is claimed twice — the packet and table emitters both name it; rename the schema file (docs/UNITY.md)", name)
		}
		files[name] = data
	}
	return files, nil
}

func init() {
	registerPacketValueDefaultCarrier("unity")
	registerBuiltin(unityTarget{}, true, false, false, false)
}
