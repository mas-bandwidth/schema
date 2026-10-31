package compiler

// luaCarried is every construct the Lua target carries (docs/SPEC-TABLES.md;
// docs/PORT-LUA.md). It is registered ONCE, by init below. A card that carries
// a construct does not edit this file: it adds, in its own new file
// compiler/carries_lua_<construct>.go,
//
//	var _ = carryLua(carryTimestamp)
//
// and its test appends the same key to nonGoCarriers["lua"] from its own
// init. Package-level variables are initialised before any init() runs, so
// every such line has extended the slice by the time the registration reads
// it, and no two construct cards touch a shared line.
var luaCarried []string

// carryLua adds one construct to what the Lua target carries; its result
// exists only so the call can initialise a package-level blank variable.
func carryLua(construct string) bool {
	luaCarried = append(luaCarried, construct)
	return true
}

func init() {
	registerCarries("lua", luaCarried...)
}
