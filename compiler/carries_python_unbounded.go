//go:build schema_leg_python

package compiler

// THE UNBOUNDED ARRAY carries no key in compiler/carries.go's carryConstructs.
// The vocabulary names text-list, text-list-total and unbounded-text, and a
// []T is none of them, so this card registers no line and adds no key. The
// table runtime's array kind carries the construct; the carry set stays as the
// base has it.
