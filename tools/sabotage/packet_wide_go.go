package main

func init() {
	sabotages["packet-wide-go-pairing"] = []edit{{
		old: `if low != expectLow { return ErrValidation }`,
		new: `if false && low != expectLow { return ErrValidation } // SABOTAGED`,
	}}
}
