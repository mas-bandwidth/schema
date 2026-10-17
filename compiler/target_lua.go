//go:build schema_leg_lua

// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
//
// Scaffolded by `schema new-leg lua`; the design is docs/PORT-LUA.md. The
// schema_leg_lua tag keeps this target out of the default nine until the
// wire surfaces have claims in compiler/*_test.go; `go build -tags
// schema_leg_lua` (make/lua.mk's build/schema-lua) is `schema generate --lang
// lua`. Drop the tag when the port joins the builtins.
//
// What it emits today is the TYPE SURFACE (internal/codegen/lua: constants,
// enums, flags, unions, constructors) and the TABLE SURFACE
// (internal/codegen/luatable: one descriptor per table, constructors) — no
// codec on either wire. The one refusal here is the one the table descriptors
// need to stay honest: they have no kind for fixed point or 128 bits
// (docs/SPEC-TABLES.md §15), and internal/codegen/luatable refuses the same
// units by the same rule, so the target and the leg's test agree. Every other
// construct's compile-time refusal, by name, is the schema-lua-refusals-by-name
// card's, landed with the codec that needs it (docs/PORT-LUA.md, THE CARD MAP).
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
	if err := ir.RefuseWideTableKinds(u, "lua"); err != nil {
		return nil, err
	}
	files, err := lua.Generate(u)
	if err != nil {
		return nil, err
	}
	tables, err := luatable.Generate(u)
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

func init() {
	registerBuiltin(luaTarget{}, false, false, false, false)
}
