package main

// The Lua packet gates\x27 controls (make/lua.mk): each removes one read or
// construction rule from the Lua generator, and the gate it controls must fail
// its own marker.
func init() {
	sabotages["packet-utf8-lua-read"] = []edit{{
		old: `m.Printf("%sif not utf8valid(%s) then return false end -- malformed UTF-8 (SPEC §4.7)\n", ind, name)`,
		new: `m.Printf("%sif false and not utf8valid(%s) then return false end -- SABOTAGED\n", ind, name)`,
	}}
	sabotages["packet-wide-lua-pairing"] = []edit{{
		old: `m.Printf("%s  if low ~= expectLow then return false end\n", ind)`,
		new: `m.Printf("%s  if false and low ~= expectLow then return false end -- SABOTAGED\n", ind)`,
	}}
	sabotages["packet-defaults-lua-constructor-bytes"] = []edit{{
		old: "case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:\n\t\treturn ByteString(f.DefBytes)",
		new: "case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:\n\t\treturn `\"\"` // SABOTAGED",
	}}
	sabotages["packet-end-lua-end-check"] = []edit{
		{
			old: `	m.Printf("-- Read%s reads one packet: the type wire (SPEC §4.3, §6.1), then the\n", d.Name)
	m.Printf("-- end check (SPEC §5). false refuses without a latch (a stream overflow\n")
	m.Printf("-- rides stream:error()).\n")
	m.Printf("function _M.Read%s(stream, value)\n", d.Name)
	m.Printf("  if not _M.Read%sBody(stream, value) then return false end\n", d.Name)
	m.Printf("  return stream:serializeEnd()\n")`,
			new: `	m.Printf("-- Read%s reads one packet: the type wire (SPEC §4.3, §6.1), then the\n", d.Name)
	m.Printf("-- end check (SPEC §5). false refuses without a latch (a stream overflow\n")
	m.Printf("-- rides stream:error()).\n")
	m.Printf("function _M.Read%s(stream, value)\n", d.Name)
	m.Printf("  if not _M.Read%sBody(stream, value) then return false end\n", d.Name)
	m.Printf("  return true -- SABOTAGED: the end check removed\n")`,
		},
		{
			old: `	m.Printf("-- Read%s reads one packet: the union wire (SPEC §4.8), then the end check\n", d.Name)
	m.Printf("-- (SPEC §5).\n")
	m.Printf("function _M.Read%s(stream, value)\n", d.Name)
	m.Printf("  if not _M.Read%sBody(stream, value) then return false end\n", d.Name)
	m.Printf("  return stream:serializeEnd()\n")`,
			new: `	m.Printf("-- Read%s reads one packet: the union wire (SPEC §4.8), then the end check\n", d.Name)
	m.Printf("-- (SPEC §5).\n")
	m.Printf("function _M.Read%s(stream, value)\n", d.Name)
	m.Printf("  if not _M.Read%sBody(stream, value) then return false end\n", d.Name)
	m.Printf("  return true -- SABOTAGED: the end check removed\n")`,
		},
	}
}
