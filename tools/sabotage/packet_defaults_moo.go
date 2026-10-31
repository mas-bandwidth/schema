package main

// Packet defaults, the moo leg: drop only the constructor's byte copy (New
// and Init both build through it). The used length still says 4, so the
// value is well formed and the consumer runs; only the bytes are missing.
func init() {
	sabotages["packet-defaults-moo-constructor-bytes"] = []edit{{
		old: `g.pf("%sbuffer.writestring(%s, 0, %s)\n%s%sLength = %d\n", ind, name, luaString(f.DefBytes), ind, name, len(f.DefBytes))`,
		new: `g.pf("%s-- SABOTAGED: buffer.writestring(%s, 0, %s)\n%s%sLength = %d\n", ind, name, luaString(f.DefBytes), ind, name, len(f.DefBytes))`,
	}}
}
