//go:build !schema_leg_squirrel

// The untagged build has no Squirrel target, so the optional array is still
// refused by name there (docs/SPEC-TABLES.md §2.3); the tagged build carries
// it and registers the same row from compiler/target_squirrel_optionalarrays_test.go.
package compiler

func init() {
	extraRefusalRows = append(extraRefusalRows, refusalRow{
		refuser: "refuseOptionalArrays",
		name:    "optional array",
		src:     "package probe\n\ntable L\n{\n    maybe ?[..4]int32\n}\n",
		field:   "L.maybe",
		carried: false,
	})
}
