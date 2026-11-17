// THE C++ PACKET TEXT WRITE CONTROL (docs/SPEC.md §5.1 row 8). It removes the
// generated string write's well-formedness assert while leaving the length and
// zero-byte asserts in place, so the control reddens the UTF-8 cases alone.
package main

func init() {
	sabotages["packet-write-text-cpp-utf8"] = []edit{{
		old: `g.pf("%sserialize_assert( schema_utf8_valid( reinterpret_cast<const uint8_t *>( %s ), %s_length ) );\n", ind, name, name)`,
		new: `g.pf("%sserialize_assert( true ); // SABOTAGED: no string UTF-8 write check\n", ind)`,
	}}
}
