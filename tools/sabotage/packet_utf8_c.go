package main

func init() {
	sabotages["packet-utf8-c-read"] = []edit{{
		old: `if ( !schema_utf8_valid_( (const serialize_uint8_t *) value->%s, value->%s_length ) )`,
		new: `if ( 0 && !schema_utf8_valid_( (const serialize_uint8_t *) value->%s, value->%s_length ) ) /* SABOTAGED */`,
	}}
}
