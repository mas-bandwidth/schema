//go:build schema_leg_lua

package main

import "github.com/mas-bandwidth/schema/v2/internal/codegen/luatable"

// The Lua target's generated tests (docs/SPEC-TABLES.md §16.11): the tagged
// build registers luatable.Testgen, so `schema testgen --lang lua` writes
// <Base>Table_gen_test.lua beside what `schema generate --lang lua` wrote.
// main.go still refuses every other non-Go --lang through compiler.Testgen.
func init() { testgenLua = luatable.Testgen }
