// THE FIXED FORM'S OTHER JAVA SABOTAGE (docs/SPEC-TABLES.md §3.4), and it
// plants the ENCODING this branch replaced rather than removing a check.
//
// A plan entry inside a union arm is selected by its ARM ORDINAL against the
// writer's own tag, and `arg` is that ordinal. The encoding below is the one
// that was here: the text op wrote its FLAVOUR into the same byte, so a text
// field under an arm past the first was guarded on a flavour, never fired,
// never landed and counted nothing — a `string(N)` silently dropped on every
// compiled-plan read. Nothing else about the record moved, which is exactly
// why it went unseen, so the control requires the arm-text conformance case
// (test/tables/UT.schema) to go RED under it and to say which check failed.
package main

// NOTE ON THE FILE NAME: `_arm.go` would be a GOARCH suffix and this file
// would build for nothing.

func init() {
	sabotages["fixed-form-java-arm-text-flavour"] = []edit{
		{
			old: "c.push(theirAt, at, units, auxAt, guard, opText, arg, (byte) 0, (byte) 0, meta);",
			new: "c.push(theirAt, at, units, auxAt, guard, opText, meta, (byte) 0, (byte) 0, arg); // SABOTAGED: the flavour over the arm ordinal",
		},
		{
			old: "final int unit = (p.meta == textWide) ? 2 : 1;",
			new: "final int unit = (p.arg == textWide) ? 2 : 1; // SABOTAGED: the flavour read back off the guard's lane",
		},
	}
}
