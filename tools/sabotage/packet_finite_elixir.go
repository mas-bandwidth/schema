package main

func init() {
	// Removes the three raw-bit read checks of SPEC §4.3's finite-float rule
	// from the Elixir packet runtime: f32_value's and f64_value's exponent
	// tests and get_f64's high-bytes clause. Writes keep their refusal, so
	// only the crafted-bytes read vectors may go red.
	sabotages["packet-finite-elixir-read"] = []edit{
		{
			old: "  def f32_value(bits) do\n    if (bits >>> 23 &&& 0xFF) == 0xFF do\n      throw(:invalid)\n    end\n\n",
			new: "  def f32_value(bits) do\n    # SABOTAGED: no non_finite_float read check\n",
		},
		{
			old: "      <<_::binary-size(^i), _::48, 0xF::4, _::4, _::1, 0x7F::7, _::binary>> -> throw(:invalid)\n",
			new: "      # SABOTAGED: no non_finite_float read check\n",
		},
		{
			old: "  def f64_value(bits) do\n    if (bits >>> 52 &&& 0x7FF) == 0x7FF do\n      throw(:invalid)\n    end\n\n",
			new: "  def f64_value(bits) do\n    # SABOTAGED: no non_finite_float read check\n",
		},
	}
}
