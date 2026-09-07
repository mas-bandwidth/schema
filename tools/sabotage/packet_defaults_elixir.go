package main

func init() {
	sabotages["packet-defaults-elixir-constructor-bytes"] = []edit{{
		old: `			return "<<" + strings.Join(bytes, ", ") + ">>"`,
		new: `			return fmt.Sprintf("<<0::size(%d)-unit(8)>>", len(f.DefBytes)) // SABOTAGED: same length, zero bytes`,
	}}
}
