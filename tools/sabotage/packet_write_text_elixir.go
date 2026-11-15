package main

func init() {
	// Removes the write-side text content checks of SPEC §5.1 row 8 from the
	// Elixir packet writer: the used bytes of string(N) may then hold a zero
	// byte or malformed UTF-8. The writer's length bound stays, so only the
	// ill-formed text cases may go red.
	sabotages["packet-write-text-elixir-write"] = []edit{{
		old: `	if f.Type.Kind == ir.TString {
		g.raiseIf(fmt.Sprintf("not String.valid?(%s)", name),
			fmt.Sprintf("%s is not well-formed UTF-8 (SPEC §4.7)", g.dsp(name)), ind)
		g.raiseIf(fmt.Sprintf(":binary.match(%s, <<0>>) != :nomatch", name),
			fmt.Sprintf("%s holds a zero byte (SPEC §4.7)", g.dsp(name)), ind)
	}
`,
		new: `	// SABOTAGED: the write-side text content checks removed
`,
	}}
}
