package main

func init() {
	sabotages["packet-defaults-rust-constructor-bytes"] = []edit{{
		old: `schema_buffer[..%d].copy_from_slice(&[%s]);\n`,
		new: `// SABOTAGED: schema_buffer[..%d].copy_from_slice(&[%s]);\n`,
	}}
}
