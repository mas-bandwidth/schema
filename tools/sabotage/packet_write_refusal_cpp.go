// THE C++ WRITE-REFUSAL-PACKET CONTROL (docs/SPEC.md §5.1 rows 1, 3). It
// moves the generated range assert's upper bound up by one, so a value one
// past the bound is accepted: the ranged scalar, the constant and
// reserved-range fields, and the counted array each go red
// at their one-past case, and no lawful twin does.
package main

func init() {
	sabotages["packet-write-refusal-cpp-upper"] = []edit{{
		old: `g.pf("%sserialize_assert( %s( %s ) >= %s( %s ) && %s( %s ) <= %s( %s ) );\n", ind, cast, expr, cast, lo, cast, expr, cast, hi)`,
		new: `g.pf("%sserialize_assert( %s( %s ) >= %s( %s ) && %s( %s ) <= %s( %s ) + 1 ); // SABOTAGED: upper bound moved by one\n", ind, cast, expr, cast, lo, cast, expr, cast, hi)`,
	}}
}
