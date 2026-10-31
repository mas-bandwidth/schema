// THE PREVIOUS-RELEASE DIFFERENTIAL'S SABOTAGES (docs/VERSIONING.md, #463;
// Makefile release-differential-negative-control). Each moves encoded bytes
// and leaves every protocol id where it was, which is exactly the class the
// gate exists to refuse, one wire each.
//
//   - release-differential-union-tag moves every union arm's tag by one in the
//     C++ packet writer.
//   - release-differential-table-bool writes a true bool as 2 in the Go
//     engine's variable form.
package main

func init() {
	sabotages["release-differential-union-tag"] = []edit{{
		old: `g.pf("            write_bits( stream, %du, %d );\n", i+1, bits)`,
		new: `g.pf("            write_bits( stream, %du, %d );\n", i+2, bits) // SABOTAGED: every arm's tag moved by one`,
	}}
	sabotages["release-differential-table-bool"] = []edit{{
		old: "\tcase ir.TableKindBool:\n\t\tif cell.B {\n\t\t\tw.u8(1)\n",
		new: "\tcase ir.TableKindBool:\n\t\tif cell.B {\n\t\t\tw.u8(2) // SABOTAGED: true is written as 2\n",
	}}
}
