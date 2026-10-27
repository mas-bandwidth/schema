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
	// Construct by construct first, so a unit is refused for the first thing
	// in it this target does not carry, by name; then the table declaration
	// itself, which no Unity backend carries yet.
	for _, refuse := range []func(*ir.Unit, string) error{
		refuseWideText, refuseValueDefaults, refuseWasRows, refuseUnionArrays,
		refuseOptionalArrays, refuseMaps, refuseLists, refuseTables,
	} {
		if err := refuse(u, "unity"); err != nil {
			return nil, err
		}
	}
	return unity.Generate(u)
}

func init() {
	registerPacketValueDefaultCarrier("unity")
	registerWideTextCarrier("unity")
	registerBuiltin(unityTarget{}, false, false, false, false)
}
