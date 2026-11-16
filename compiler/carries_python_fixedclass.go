//go:build schema_leg_python

package compiler

// THE FIXED-CLASS FIELD KINDS are field kinds and not constructs of the §2
// carry vocabulary: a string, a byte buffer, a float, an enum, a flags mask,
// a bounded or counted array, and the bits and ranged integers the base's
// integer codec already carries. None of them names a carryConstructs key, so
// this card registers none. The constructs a later table card adds register in
// their own file, in the skeleton's pattern (compiler/carries_python.go).
