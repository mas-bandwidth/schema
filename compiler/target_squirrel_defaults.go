//go:build schema_leg_squirrel

// Squirrel carries a string, bytes or flags DEFAULT on the FORM-1 table wire
// (docs/SPEC-TABLES.md §4, §16.2; SPEC §4.2): its table writer elides a field
// whose value is the declared default, and its reader restores that default
// on an absent field. The table surface already walks the construct, so the
// target registers as a carrier here rather than refusing it by name.
//
// This is a per-target registration. The card's registry that names what each
// target carries (registerCarried) is not in this tree yet, so the value
// default list is joined directly, as every other port's target file does.
package compiler

func init() {
	valueDefaultTargets = append(valueDefaultTargets, "squirrel")
}
