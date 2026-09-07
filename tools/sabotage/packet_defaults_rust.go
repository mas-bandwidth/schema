package main

func init() {
	sabotages["packet-defaults-rust-constructor-bytes"] = []edit{{
		old: `				g.pf("        %s[..%d].copy_from_slice(&[", name, len(f.DefBytes))`,
		new: `				g.pf("        // SABOTAGED: %s[..%d].copy_from_slice(&[", name, len(f.DefBytes))`,
	}}
}
