//go:build schema_leg_python

package compiler

// THE UNBOUNDED ARRAY carries no key in compiler/carries.go's carryConstructs.
// The vocabulary names text-list, text-list-total and unbounded-text, and a
// []T is none of them, so this card registers no carries line and adds no key.
// The table runtime's array kind carries the construct; the registration is
// the per-target list in compiler/tableslists.go, like the squirrel leg's, so
// refuseLists admits a []T instead of naming python as a follow-on.
func init() {
	registerListCarrier("python")
}
