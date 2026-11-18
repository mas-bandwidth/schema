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

func (t unityTarget) Generate(u *ir.Unit, opts Options) (map[string][]byte, error) {
	return t.GenerateLineage(u, opts, nil)
}

// GenerateLineage is the second entry point of docs/FIXED-FORM-ALGORITHM.md §5.9
// #1: the driver read the unit's lock once (compiler/lineage.go) and hands the
// lineage down as DATA, so a build reads every layout the lock records.
func (unityTarget) GenerateLineage(u *ir.Unit, _ Options, lineage *FixedLineage) (map[string][]byte, error) {
	if err := refuseUnported(u, "unity"); err != nil {
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
	tables, err := unitytable.GenerateLineage(u, unityTableLineage(u, lineage))
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

// unityTableLineage is the lock's lineage in the Unity backend's own spelling,
// the writer snapshot included: the bounds pass holds an older file to the
// writer's own bounds (docs/FIXED-FORM-ALGORITHM.md §4.6). Nil in, nil out: a
// unit with no lock hands nothing, and every fixed root then carries its own
// entry alone. Record is passed through as the driver states it, the whole
// record (compiler/lineage.go).
func unityTableLineage(u *ir.Unit, lineage *FixedLineage) map[string][]unitytable.FixedLineageEntry {
	if lineage == nil {
		return nil
	}
	out := map[string][]unitytable.FixedLineageEntry{}
	for _, st := range ir.TableFixedRoots(u) {
		for _, e := range lineage.Entries(st.Name) {
			out[st.Name] = append(out[st.Name], unitytable.FixedLineageEntry{
				Wire:             e.Wire,
				Layout:           e.Layout,
				Digest:           e.Digest,
				Record:           e.Record,
				Retired:          e.Retired,
				Reason:           e.Reason,
				Writer:           e.Writer,
				LegacyLayoutOnly: e.LegacyLayoutOnly,
			})
		}
	}
	return out
}

func init() {
	registerPacketValueDefaultCarrier("unity")
	valueDefaultTargets = append(valueDefaultTargets, "unity")
	wasRowTargets = append(wasRowTargets, "unity")
	registerOptionalArrayCarrier("unity")
	registerMapCarrier("unity")
	registerListCarrier("unity")
	registerBuiltin(unityTarget{}, true, true, true, true)
}
