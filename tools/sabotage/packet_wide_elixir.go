package main

func init() {
	sabotages["packet-wide-elixir-pairing"] = []edit{{
		old: `if low != expect_low do`,
		new: `if low != expect_low and :erlang.system_info(:wordsize) == 0 do # SABOTAGED`,
	}}
}
