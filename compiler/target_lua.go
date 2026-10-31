// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
//
// Scaffolded by `schema new-leg lua`; the design is docs/PORT-LUA.md. A
// builtin target: the ordinary bin/schema is `schema generate --lang lua`.
//
// What it emits today is the TYPE SURFACE (internal/codegen/lua: constants,
// enums, flags, unions, constructors) and the TABLE SURFACE
// (internal/codegen/luatable: one descriptor per table, constructors, and the
// fixed and message forms that carry every table-wire kind, the fixed-point and
// 128-bit families included — docs/SPEC-TABLES.md §3, §15: the fixed form names
// no kind, so a wide field is its declared storage image, and the message form
// carries its announced packing). Every construct's compile-time refusal, by
// name, is the schema-lua-refusals-by-name card's, landed with the codec that
// needs it (docs/PORT-LUA.md, THE CARD MAP).
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luatable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type luaTarget struct{}

func (luaTarget) Names() []string { return []string{"lua"} }

func (luaTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	return luaGenerate(u, nil)
}

// GenerateLineage is [Generate] with the LOCK's lineage handed to the table
// backend (docs/FIXED-FORM-ALGORITHM.md §5.2, §5.9 #1). A unit with no lock
// hands nothing, and every table carries the single entry it can compute.
func (luaTarget) GenerateLineage(u *ir.Unit, _ Options, lineage *FixedLineage) (map[string][]byte, error) {
	return luaGenerate(u, lineage)
}

func luaGenerate(u *ir.Unit, lineage *FixedLineage) (map[string][]byte, error) {
	files, err := lua.Generate(u)
	if err != nil {
		return nil, err
	}
	tables, err := luatable.GenerateLineage(u, luaTableLineage(u, lineage))
	if err != nil {
		return nil, err
	}
	for name, data := range tables {
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("generated file %s is claimed twice — a schema file named %s beside the unit's table module; rename the file (docs/PORT-LUA.md)", name, name)
		}
		files[name] = data
	}
	return files, nil
}

// luaTableLineage is the lock's lineage in the Lua backend's own spelling.
// Nil in, nil out: a unit with no lock hands nothing.
func luaTableLineage(u *ir.Unit, lineage *FixedLineage) map[string][]luatable.FixedLineageEntry {
	if lineage == nil {
		return nil
	}
	out := map[string][]luatable.FixedLineageEntry{}
	for _, st := range ir.TableFixedRoots(u) {
		entries := lineage.Entries(st.Name)
		if len(entries) == 0 {
			continue
		}
		for _, e := range entries {
			out[st.Name] = append(out[st.Name], luatable.FixedLineageEntry{
				Wire:    e.Wire,
				Layout:  e.Layout,
				Record:  e.Record,
				Retired: e.Retired,
				Reason:  e.Reason,
			})
		}
	}
	return out
}

func init() {
	// tables, table arms, arrays of unions and both byte buffers: the Lua
	// table backend carries every construct the nine do (the conformance
	// manifest generates for lua with no refusal), so it registers as they do.
	registerBuiltin(luaTarget{}, true, true, true, true)
	registerOptionalArrayCarrier("lua")
	registerListCarrier("lua")
	registerMapCarrier("lua")
}
