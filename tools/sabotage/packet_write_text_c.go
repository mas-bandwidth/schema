package main

func init() {
	sabotages["packet-write-text-c-utf8-write"] = []edit{{
		old: `g.pf("%sserialize_assert( schema_utf8_valid_( (const uint8_t *) value->%s, value->%s_length ) ); /* ill-formed text on write (SPEC §5.1) */\n", ind, f.Name, f.Name)`,
		new: `g.pf("%sserialize_assert( 1 || schema_utf8_valid_( (const uint8_t *) value->%s, value->%s_length ) ); /* SABOTAGED */\n", ind, f.Name, f.Name)`,
	}}
}
