package main

func init() {
	sabotages["packet-wide-c-pairing"] = []edit{{
		old: `if ( low != expect_low ) { return 0; }`,
		new: `if ( 0 && low != expect_low ) { return 0; } /* SABOTAGED */`,
	}}
}
