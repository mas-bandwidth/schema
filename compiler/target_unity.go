// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
package compiler

import (
	"github.com/mas-bandwidth/schema/v2/internal/codegen/unity"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// unityTarget emits Unity C#: C# 9 over netstandard2.1, unmanaged structs,
// IL2CPP and Burst safe (docs/UNITY.md). It is a generator of its own beside
// --lang cs, which is the modern .NET one; the two share the IR and the name
// mapping and no emitter. Packet types only today: the Unity table backend is
// a named follow-on, so a unit declaring tables is refused by name.
type unityTarget struct{}

func (unityTarget) Names() []string { return []string{"unity"} }

func (unityTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	// Wide table kinds first, so a unit that declares them is refused with
	// the shared diagnostic before any other follow-on. Then each construct
	// this target does not carry, by name, and the table declaration itself.
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
	if err := refuseTables(u, "unity"); err != nil {
		return nil, err
	}
	return unity.Generate(u)
}

func init() {
	registerPacketValueDefaultCarrier("unity")
	registerBuiltin(unityTarget{}, false, false, false, false)
}
