//go:build schema_leg_python

package compiler

// The Python PACKET WIRE's carried constructs (docs/PORT-PYTHON.md, THE
// LAYERS: P4; internal/codegen/python/wire.go): wide text on the packet wire
// (SPEC §4.12; a wstring in a table closure stays refused by
// tableWideTextTargets) and the string, bytes and flags packet defaults (SPEC
// §4.2), which the type surface constructs and the packet wire carries, held
// by make packet-wide-python and packet-defaults-python.
func init() {
	registerWideTextCarrier("python")
	registerPacketValueDefaultCarrier("python")
}
