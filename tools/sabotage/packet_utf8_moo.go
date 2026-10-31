package main

// Packet text, the moo leg (SPEC §4.7). moo's UTF-8 read check is the
// runtime's serializeString, so the sabotage takes it away at the emitter:
// the text is read raw (the same length field, alignment and bytes) and only
// the zero-byte rule is put back. Malformed UTF-8 alone now reads, which the
// mutation sweep must catch against the C++ oracle.
func init() {
	sabotages["packet-utf8-moo-read"] = []edit{{
		old: `g.call(ind, fmt.Sprintf("stream:serializeString(STR, %d)", t.Size+1), "refuses a zero byte, then malformed UTF-8 (SPEC §4.7)")
		g.pf("%s%s = STR.value\n", ind, val)`,
		new: `g.call(ind, fmt.Sprintf("stream:serializeInt(NUM, 0, %d)", t.Size), "SABOTAGED: the text is read raw, its UTF-8 unchecked")
		g.pf("%slocal raw = buffer.create(%d)\n", ind, t.Size)
		g.call(ind, "stream:serializeBytes(raw, 0, NUM.value)", "")
		g.pf("%s%s = buffer.readstring(raw, 0, NUM.value)\n", ind, val)
		g.refuse(ind, fmt.Sprintf("string.find(%s, \"\\0\", 1, true) ~= nil", val), "the zero-byte rule stays")`,
	}}
}
