package compiler

func init() {
	// §16.10, the text form; §4.3, a one-sided integer range fills its other
	// side from the type's limit, exactly as Go carries it (SPEC §4.6);
	// §2.8, ?map[K]V; §2.14, the integrity trailer (this card: the message form)
	registerCarries("cpp", carryArrayRoot, carryOneSidedRange, carryOptionalMap, carryIntegrity)
}
