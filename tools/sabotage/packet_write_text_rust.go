package main

func init() {
	sabotages["packet-write-text-rust-utf8-write"] = []edit{{
		old: `g.writeAssert(ind, fmt.Sprintf("std::str::from_utf8(&%s[..%s_length as usize]).is_ok()", name, name),`,
		new: `g.writeAssert(ind, fmt.Sprintf("true || std::str::from_utf8(&%s[..%s_length as usize]).is_ok() /* SABOTAGED */", name, name),`,
	}}
}
