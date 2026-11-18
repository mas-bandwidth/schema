package compiler

// The Ruby PACKET WIRE's carried constructs (docs/PORT-RUBY.md, THE CARD MAP):
// string, bytes and flags packet defaults (SPEC §4.2), which the type surface
// constructs and the packet wire carries.
func init() {
	registerPacketValueDefaultCarrier("ruby")
}
