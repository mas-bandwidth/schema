// THE PREVIOUS-RELEASE DIFFERENTIAL'S SABOTAGE (docs/VERSIONING.md, #463;
// Makefile release-differential-negative-control). It moves a union arm's tag
// on the packet wire by one, in the C++ writer only: the wire changes and the
// shape the protocol id is taken over does not, which is exactly the class the
// gate exists to refuse.
package main

func init() {
	sabotages["release-differential-union-tag"] = []edit{{
		old: `g.pf("            write_bits( stream, %du, %d );\n", i+1, bits)`,
		new: `g.pf("            write_bits( stream, %du, %d );\n", i+2, bits) // SABOTAGED: every arm's tag moved by one`,
	}}
}
