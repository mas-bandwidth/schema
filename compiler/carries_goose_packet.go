//go:build schema_leg_goose

package compiler

// The Goose PACKET WIRE's carried constructs (docs/PORT-GOOSE.md, THE CARD
// MAP; internal/codegen/goose/wire.go): the string, bytes and flags packet
// defaults (SPEC §4.2), which the type surface constructs and the packet wire
// carries, held by make packet-defaults-goose.
func init() {
	registerPacketValueDefaultCarrier("goose")
}
