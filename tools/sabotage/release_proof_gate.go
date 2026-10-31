// THE RELEASE LOCK'S SABOTAGE (Makefile release-proof-gate-negative-control).
// VerifyRelease stops looking at the red cells, so a red proof report at the
// commit lets `make release` through to the tagger.
package main

func init() {
	sabotages["release-proof-gate"] = []edit{{
		old: `if len(r.Red) > 0 {`,
		new: `if false && len(r.Red) > 0 { // SABOTAGED: red cells pass the lock`,
	}}
}
