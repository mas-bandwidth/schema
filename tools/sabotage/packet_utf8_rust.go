package main

func init() {
	sabotages["packet-utf8-rust-read"] = []edit{{
		old: `if std::str::from_utf8(&%s[..%s_length as usize]).is_err() {`,
		new: `if false && std::str::from_utf8(&%s[..%s_length as usize]).is_err() { // SABOTAGED`,
	}}
}
