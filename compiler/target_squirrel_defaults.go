// Squirrel carries a string, bytes or flags DEFAULT on the FORM-1 table wire
// (docs/SPEC-TABLES.md §4, §16.2; SPEC §4.2): its table writer elides a field
// whose value is the declared default, and its reader restores that default
// on an absent field. The table surface already walks the construct, so the
// target registers as a carrier here rather than refusing it by name.
//
// This is a per-target registration: the carries registry (compiler/carries.go)
// names no key for the value default (its construct keys are the table-wire
// features of §2 and §16, and none is the elision rule of §4/§16.2), so the
// value default carrier list is joined directly, as every other port's value
// file does.
package compiler

func init() {
	// Squirrel carries the value default (docs/SPEC-TABLES.md §4, §16.2);
	// registering it here lets the table surface walk the construct.
	valueDefaultTargets = append(valueDefaultTargets, "squirrel")
}
