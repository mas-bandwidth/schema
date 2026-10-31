// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
//
// Scaffolded by `schema new-leg luau`; docs/PORT-LUAU.md is the port's
// design. The target is a builtin: `schema generate --lang luau`.
package compiler

import (
	"maps"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type luauTarget struct{}

func (luauTarget) Names() []string { return []string{"luau"} }

// Generate is the type surface (one module per schema file) and, for a unit
// that declares tables, the table surface (one <Base>Table.luau per file
// with tables) — the descriptors and constructors, no codec yet
// (docs/PORT-LUAU.md, THE CARD MAP). A construct the table surface does not
// describe is refused by luautable, by name.
func (t luauTarget) Generate(u *ir.Unit, opts Options) (map[string][]byte, error) {
	return t.GenerateLineage(u, opts, nil)
}

// GenerateLineage is the second entry point of docs/FIXED-FORM-ALGORITHM.md
// §5.9 #1: the driver read the unit's lock once (compiler/lineage.go) and
// hands the lineage down as DATA, so luautable opens no file and a build
// ships a reader for every layout the lock records.
func (luauTarget) GenerateLineage(u *ir.Unit, _ Options, lineage *FixedLineage) (map[string][]byte, error) {
	files, err := luau.Generate(u)
	if err != nil {
		return nil, err
	}
	tables, err := luautable.GenerateLineage(u, luauTableLineage(u, lineage))
	if err != nil {
		return nil, err
	}
	maps.Copy(files, tables)
	return files, nil
}

// luauTableLineage is the lock's lineage in the Luau backend's own spelling.
// Nil in, nil out: a unit with no lock hands nothing. Each entry's Record is
// already the whole record, eight hash bytes plus the body (compiler/lineage.go).
func luauTableLineage(u *ir.Unit, lineage *FixedLineage) map[string][]luautable.FixedLineageEntry {
	if lineage == nil {
		return nil
	}
	out := map[string][]luautable.FixedLineageEntry{}
	for _, st := range ir.TableFixedRoots(u) {
		for _, e := range lineage.Entries(st.Name) {
			out[st.Name] = append(out[st.Name], luautable.FixedLineageEntry{
				Wire: e.Wire, Layout: e.Layout, Record: e.Record, Retired: e.Retired, Reason: e.Reason,
			})
		}
	}
	return out
}

func init() {
	registerPacketValueDefaultCarrier("luau")
	valueDefaultTargets = append(valueDefaultTargets, "luau")
	wasRowTargets = append(wasRowTargets, "luau")
	registerWideTextCarrier("luau")
	registerListCarrier("luau")
	registerMapCarrier("luau")
	registerOptionalArrayCarrier("luau")
	registerBuiltin(luauTarget{}, true, true, true, true)
}
