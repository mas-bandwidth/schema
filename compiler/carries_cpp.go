package compiler

func init() {
	// §16.10, the text form; §4.3, a one-sided integer range fills its other
	// side from the type's limit, exactly as Go carries it (SPEC §4.6);
	// §2.8, ?map[K]V; §2.11 the timestamp kind, its text exactly RFC 3339
	// §5.6 date-time (internal/codegen/cpptable/json.go).
	registerCarries("cpp", carryTimestamp, carryArrayRoot, carryOneSidedRange, carryOptionalMap)
}
