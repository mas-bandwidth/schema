//go:build schema_leg_lua

package compiler

func init() {
	registerCarries("lua", carryOneSidedRange)
}
