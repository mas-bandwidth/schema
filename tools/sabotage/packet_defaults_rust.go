package main

func init() {
	sabotages["packet-defaults-rust-constructor-bytes"] = []edit{{
		old: `buffer[..%d].copy_from_slice(&[%s]);\n`,
		new: `// SABOTAGED: buffer[..%d].copy_from_slice(&[%s]);\n`,
	}}
}
