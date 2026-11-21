package compiler

// The Lua PACKET WIRE's string, bytes and flags defaults (SPEC §4.2;
// make packet-defaults-lua). The Lua target never calls refuseValueDefaults,
// so this line changes no Lua output; it keeps the refusal's list of carriers
// whole for any leg that still reaches it (a `schema new-leg` scaffold).
func init() {
	registerPacketValueDefaultCarrier("lua")
}
