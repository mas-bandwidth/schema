// THE BUILD-VERSION FORM GATE'S SABOTAGE (docs/VERSIONING.md, "The release
// policy"; Makefile release-form-gate-negative-control). Release reads both
// forms and hands the gate the tag's twice, so a patch release that moves the
// form tags.
package main

func init() {
	sabotages["release-form-gate"] = []edit{{
		old: `if err := FormGate(newest, next, forms[0], forms[1]); err != nil {`,
		new: `if err := FormGate(newest, next, forms[0], forms[0]); err != nil { // SABOTAGED: the commit's form is never compared`,
	}}
}
