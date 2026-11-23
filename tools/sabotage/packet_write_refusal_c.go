package main

func init() {
	sabotages["packet-write-refusal-c-range-write"] = []edit{{
		old: `g.pf("%sserialize_assert( %s );\n", ind, cond)`,
		new: `g.pf("%sserialize_assert( 1 || ( %s ) ); /* SABOTAGED */\n", ind, cond)`,
	}}
}
