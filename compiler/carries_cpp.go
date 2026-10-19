package compiler

func init() {
	registerCarries("cpp", carryArrayRoot, carryOptionalMap) // docs/SPEC-TABLES.md §16.10, the text form; §2.8, ?map[K]V
}
