//go:build schema_leg_goose

package compiler

// The Goose PACKET WIRE's carried constructs (docs/PORT-GOOSE.md, THE CARD
// MAP; internal/codegen/goose/wire.go): wstring is retired (kind 33)
// and the string, bytes and flags packet defaults (SPEC §4.2), which the type
// surface constructs and the packet wire carries, held by make
// packet-wide-goose and packet-defaults-goose.
func init() {
	registerWideTextCarrier("goose")
	registerPacketValueDefaultCarrier("goose")
}
