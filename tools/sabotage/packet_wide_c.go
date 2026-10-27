package main

func init() {
	sabotages["packet-wide-c-pairing"] = []edit{{
		old: `if ( low != expect_low ) { return serialize_read_fail( stream, SERIALIZE_REFUSAL_VALUE_OUT_OF_RANGE ); }`,
		new: `if ( 0 && low != expect_low ) { return serialize_read_fail( stream, SERIALIZE_REFUSAL_VALUE_OUT_OF_RANGE ); } /* SABOTAGED */`,
	}}
}
