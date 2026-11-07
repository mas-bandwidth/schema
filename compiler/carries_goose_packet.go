//go:build schema_leg_goose

package compiler

// The Goose PACKET WIRE's carried constructs (docs/PORT-GOOSE.md, THE CARD
// MAP; internal/codegen/goose/wire.go): wide text on the packet wire (SPEC
// §4.12; a wstring in a table closure stays refused by tableWideTextTargets)
// and the string, bytes and flags packet defaults (SPEC §4.2), which the type
// surface constructs and the packet wire carries, held by make
// packet-wide-goose and packet-defaults-goose.
func init() {
	registerWideTextCarrier("goose")
	registerPacketValueDefaultCarrier("goose")
}
