package compiler

// The Python PACKET WIRE's carried constructs (docs/PORT-PYTHON.md, THE CARD
// MAP): string, bytes and flags packet defaults (SPEC §4.2), which the type
// surface constructs and the packet wire carries.
func init() {
	registerPacketValueDefaultCarrier("python")
}
