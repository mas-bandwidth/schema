package main

func init() {
	sabotages["packet-wide-rust-pairing"] = []edit{{
		old: `if low != expect_low { return Err(Error::Validation); }`,
		new: `if false && low != expect_low { return Err(Error::Validation); } // SABOTAGED`,
	}}
}
