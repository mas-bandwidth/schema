package main

func init() {
	// Removes the one-packet end check of SPEC §5 from the Elixir emitter's
	// read_<name>/2: the trailing byte and the final byte's non-zero unused
	// bits are accepted, while read_<name>_bits/2 stays composable.
	sabotages["packet-end-elixir-read"] = []edit{{
		old: `	g.bpf("        if byte_size(data) == div(bits_read + 7, 8) and\n")
	g.bpf("             (rem(bits_read, 8) == 0 or\n")
	g.bpf("                :binary.at(data, byte_size(data) - 1) >>> rem(bits_read, 8) == 0) do\n")`,
		new: `	g.bpf("        if (bits_read >>> 0) >= 0 do # SABOTAGED: no packet end check\n")`,
	}}
}
