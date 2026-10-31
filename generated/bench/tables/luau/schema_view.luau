--!strict
-- UnitView returns the unit registry as constant data (docs/SPEC-TABLES.md §8.3).
local manifest: any = table.freeze({
	package = "benchtable",
	protocol_id = "0926221bcb6f475f",
	constants = {
	},
	enums = {
		{name="TableWeapon",file="BenchTable.schema",max="15",bits=8,descriptor=require("./BenchTableView")["TableWeaponDescriptor"],doc="",tags={},variants={{name="None",value="0",id="0000000000000000",doc="",tags={}},{name="Fists",value="1",id="a790eee12766cf0c",doc="",tags={}},{name="Pistol",value="2",id="9fbfefa835da8476",doc="",tags={}},{name="Shotgun",value="3",id="188bbb1783928a95",doc="",tags={}},{name="Rifle",value="4",id="2c3667b9c2f272f1",doc="",tags={}},{name="Sniper",value="5",id="0ca8b41b00d755f6",doc="",tags={}},{name="Smg",value="6",id="985fc819fab21a4e",doc="",tags={}},{name="Rocket",value="7",id="229d0447c3086a55",doc="",tags={}},{name="Grenade",value="8",id="011c7b49a7228f9d",doc="",tags={}},{name="Plasma",value="9",id="89f7566e1123e15f",doc="",tags={}},{name="Railgun",value="10",id="8d7f25318b3b4469",doc="",tags={}},{name="Flamer",value="11",id="d9cd0dcc285b7816",doc="",tags={}},{name="Mine",value="12",id="04dc16aea8ff5276",doc="",tags={}},{name="Turret",value="13",id="35e53bc341129217",doc="",tags={}},{name="Drone",value="14",id="2cc8282fb0de8831",doc="",tags={}},{name="Repair",value="15",id="20bada21bc8cb334",doc="",tags={}}}},
	},
	flags = {
		{name="TableDamage",file="BenchTable.schema",max="7",bits=8,descriptor=require("./BenchTableView")["TableDamageDescriptor"],doc="",tags={},variants={{name="Bleeding",bit=0,doc="",tags={}},{name="Burning",bit=1,doc="",tags={}},{name="Stunned",bit=2,doc="",tags={}},{name="Slowed",bit=3,doc="",tags={}},{name="Poisoned",bit=4,doc="",tags={}},{name="Shielded",bit=5,doc="",tags={}},{name="Airborne",bit=6,doc="",tags={}},{name="Downed",bit=7,doc="",tags={}}}},
	},
	unions = {
		{name="TableEvent",file="BenchTable.schema",max="3",bits=8,descriptor=require("./BenchTableTable")["TableEventDescriptor"],doc="",tags={},arms={{name="None",tag=0,id="0000000000000000",payload="-",record="no",field="no",kind="-",bound="-",overlay="-",doc="",tags={}},{name="hit",tag=1,id="33732819300680aa",payload="TableHitEvent",record="yes",field="no",kind="-",bound="-",overlay="-",doc="",tags={}},{name="chat",tag=2,id="f2a38d910b5b348b",payload="TableChatEvent",record="yes",field="no",kind="-",bound="-",overlay="-",doc="",tags={}},{name="pickup",tag=3,id="9fa3a41c86ecb765",payload="TablePickupEvent",record="yes",field="no",kind="-",bound="-",overlay="-",doc="",tags={}}}},
	},
	types = {
		{name="TableChatEvent",file="BenchTable.schema",doc="",tags={},descriptor=require("./BenchTableTable")["TableChatEventDescriptor"],fields={{name="channel",json="channel",type="int32",id="a5013e9ad5caeda4",optional=false,doc="",tags={}},{name="speaker",json="speaker",type="bits(12)",id="fbf1ac4d96ebd022",optional=false,doc="",tags={}}}},
		{name="TableHitEvent",file="BenchTable.schema",doc="",tags={},descriptor=require("./BenchTableTable")["TableHitEventDescriptor"],fields={{name="target_id",json="target_id",type="bits(12)",id="b7bc9ac015a25050",optional=false,doc="",tags={}},{name="damage",json="damage",type="int32",id="7f6308be8ab37fc0",optional=false,doc="",tags={}},{name="hit_kind",json="hit_kind",type="int32",id="01fbc365b059b925",optional=false,doc="",tags={}},{name="crit",json="crit",type="bool",id="126167908c9aa52d",optional=false,doc="",tags={}}}},
		{name="TablePickupEvent",file="BenchTable.schema",doc="",tags={},descriptor=require("./BenchTableTable")["TablePickupEventDescriptor"],fields={{name="item_id",json="item_id",type="bits(10)",id="9e7fd06d864fbd56",optional=false,doc="",tags={}},{name="amount",json="amount",type="int32",id="8113fe7ea2b16969",optional=false,doc="",tags={}}}},
	},
	tables = {
		{name="TableEntity",file="BenchTable.schema",doc="",tags={},descriptor=require("./BenchTableTable")["TableEntityDescriptor"],fields={{name="entity_id",json="entity_id",type="bits(12)",id="23fcfd6678e36712",optional=false,doc="",tags={}},{name="pos_x",json="pos_x",type="int32",id="cb4b37357667310e",optional=false,doc="",tags={}},{name="pos_y",json="pos_y",type="int32",id="cb4b3835766732c1",optional=false,doc="",tags={}},{name="pos_z",json="pos_z",type="int32",id="cb4b353576672da8",optional=false,doc="",tags={}},{name="yaw",json="yaw",type="bits(9)",id="b54d8e19798e16e8",optional=false,doc="",tags={}},{name="pitch",json="pitch",type="bits(9)",id="53a9f665a90cc1b1",optional=false,doc="",tags={}},{name="vel_x",json="vel_x",type="int32",id="6cede6b6eb60ee67",optional=false,doc="",tags={}},{name="vel_y",json="vel_y",type="int32",id="6cede5b6eb60ecb4",optional=false,doc="",tags={}},{name="vel_z",json="vel_z",type="int32",id="6cede8b6eb60f1cd",optional=false,doc="",tags={}},{name="health",json="health",type="int32",id="7f69d4b5288ba9cf",optional=false,doc="",tags={}},{name="weapon",json="weapon",type="TableWeapon",id="a0b610205f2c6e01",optional=false,doc="",tags={}},{name="damage",json="damage",type="TableDamage",id="7f6308be8ab37fc0",optional=false,doc="",tags={}},{name="moving",json="moving",type="bool",id="11a44fc1d1243da7",optional=false,doc="",tags={}},{name="firing",json="firing",type="bool",id="7674cfd19b9031ca",optional=false,doc="",tags={}}}},
		{name="TableMixed",file="BenchTable.schema",doc="",tags={},descriptor=require("./BenchTableTable")["TableMixedDescriptor"],fields={{name="protocol_magic",json="protocol_magic",type="uint16",id="6a5a70d91aa115fd",optional=false,doc="",tags={}},{name="sequence",json="sequence",type="bits(16)",id="aa38aca481f528a8",optional=false,doc="",tags={}},{name="ack_sequence",json="ack_sequence",type="int32",id="0dbe005c56697c3e",optional=false,doc="",tags={}},{name="ack_bits",json="ack_bits",type="bits(32)",id="9bae0da8b829ee03",optional=false,doc="",tags={}},{name="session_id",json="session_id",type="uint64",id="b7d7b5650a590b05",optional=false,doc="",tags={}},{name="client_id",json="client_id",type="uint32",id="6d7b98e2d095967e",optional=false,doc="",tags={}},{name="nonce",json="nonce",type="uint64",id="73a94c71d60dc0d8",optional=false,doc="",tags={}},{name="world_time",json="world_time",type="int64",id="3eee6b51be54fc85",optional=false,doc="",tags={}},{name="frame_tick",json="frame_tick",type="bits(48)",id="7bbc035f7b6d0112",optional=false,doc="",tags={}},{name="server_time",json="server_time",type="float32",id="3c460475f9be69c6",optional=false,doc="",tags={}},{name="entities",json="entities",type="TableEntity",id="935d0fb07bb3822a",optional=false,doc="",tags={}},{name="stats",json="stats",type="TableStat",id="ee639cad45b1994c",optional=false,doc="",tags={}},{name="game_event",json="game_event",type="TableEvent",id="2e35dc5321aa5790",optional=false,doc="",tags={}},{name="loadout",json="loadout",type="uint8",id="5759ce7586bbb5a3",optional=false,doc="",tags={}},{name="player_name",json="player_name",type="string",id="13a62f542cf8e86e",optional=false,doc="",tags={}},{name="payload",json="payload",type="bytes",id="cfb8a9d063b5e9e5",optional=false,doc="",tags={}},{name="aim_x",json="aim_x",type="float32",id="dbaf7be5e24296c9",optional=false,doc="",tags={}},{name="aim_y",json="aim_y",type="float32",id="dbaf7ae5e2429516",optional=false,doc="",tags={}},{name="aim_z",json="aim_z",type="float32",id="dbaf79e5e2429363",optional=false,doc="",tags={}},{name="recoil",json="recoil",type="float32",id="9cef31fff7e2e457",optional=false,doc="",tags={}},{name="drift",json="drift",type="float64",id="5ab3f9c9341f6c04",optional=false,doc="",tags={}},{name="wide_key",json="wide_key",type="uint64",id="a3348580461faf16",optional=false,doc="",tags={}},{name="flux",json="flux",type="int64",id="d61bdd7908af2642",optional=false,doc="",tags={}},{name="ping",json="ping",type="float32",id="bf30e00dc53307a9",optional=false,doc="",tags={}},{name="crc_hint",json="crc_hint",type="bits(24)",id="560d6527ccd8515f",optional=false,doc="",tags={}},{name="has_extra",json="has_extra",type="bool",id="c08292176cfd8672",optional=false,doc="",tags={}},{name="extra",json="extra",type="int32",id="fd29ee12a979cb69",optional=false,doc="",tags={}},{name="idle_ticks",json="idle_ticks",type="int32",id="78101ac0aa8cbcfe",optional=false,doc="",tags={}}}},
		{name="TableStat",file="BenchTable.schema",doc="",tags={},descriptor=require("./BenchTableTable")["TableStatDescriptor"],fields={{name="stat_id",json="stat_id",type="bits(8)",id="80ab75f0866dbf65",optional=false,doc="",tags={}},{name="delta",json="delta",type="int32",id="52076675ec13a0c1",optional=false,doc="",tags={}}}},
	},
})
local M = {}

local function flatten(doc: string): string
	local escaped = string.gsub(doc, "\\", "\\\\")
	local out = string.gsub(escaped, "\n", "\\n")
	return out
end

local function annotation(row: any): string
	return "tags=[" .. table.concat(row.tags, ",") .. "] doc=" .. flatten(row.doc)
end

local function listing(unit: any): string
	local lines = { string.format("unit package=%s protocol=%s", unit.package, unit.protocol_id) }
	for _, c in unit.constants do
		lines[#lines + 1] = string.format("constant %s file=%s type=%s float=%s int=%s real=%s %s", c.name, c.file, c.storage, tostring(c.is_float), c.int_value, c.real_bits, annotation(c))
	end
	for _, e in unit.enums do
		lines[#lines + 1] = string.format("enum %s file=%s max=%s bits=%d variants=%d %s", e.name, e.file, e.max, e.bits, #e.variants, annotation(e))
		for i, v in e.variants do
			lines[#lines + 1] = string.format("enum %s variant %s %s id=%s %s", e.name, v.value, v.name, v.id, annotation(v))
		end
	end
	for _, f in unit.flags do
		lines[#lines + 1] = string.format("flags %s file=%s max=%s bits=%d variants=%d %s", f.name, f.file, f.max, f.bits, #f.variants, annotation(f))
		for i, v in f.variants do
			lines[#lines + 1] = string.format("flags %s bit %d %s id=0000000000000000 %s", f.name, i - 1, v.name, annotation(v))
		end
	end
	for _, u in unit.unions do
		lines[#lines + 1] = string.format("union %s file=%s max=%s bits=%d variants=%d %s", u.name, u.file, u.max, u.bits, #u.arms, annotation(u))
		for i, a in u.arms do
			lines[#lines + 1] = string.format("union %s arm %d %s id=%s payload=%s record=%s field=%s kind=%s bound=%s overlay=%s %s", u.name, a.tag, a.name, a.id, a.payload, a.record, a.field, a.kind, a.bound, a.overlay, annotation(a))
		end
	end
	for _, kind in { "types", "tables" } do
		local label = if kind == "types" then "type" else "table"
		for _, d in unit[kind] do
			lines[#lines + 1] = string.format("%s %s file=%s fields=%d %s", label, d.name, d.file, #d.fields, annotation(d))
			for _, f in d.fields do
				lines[#lines + 1] = string.format("%s %s field %s json=%s type=%s id=%s optional=%s %s", label, d.name, f.name, f.json, f.type, f.id, tostring(f.optional), annotation(f))
			end
		end
	end
	return table.concat(lines, "\n") .. "\n"
end

local function freeze(value: any, seen: { [any]: boolean })
	if type(value) ~= "table" or seen[value] then return value end
	seen[value] = true
	for key, child in value do
		-- Descriptor graphs are already frozen by schema_tables and may share nodes.
		if key ~= "descriptor" then freeze(child, seen) end
	end
	if not table.isfrozen(value) then table.freeze(value) end
	return value
end

local cached: any = nil
function M.UnitView()
	if cached ~= nil then return cached end
	local unit = table.clone(manifest)
	unit.listing = function() return listing(unit) end
	cached = freeze(unit, {})
	return cached
end

return table.freeze(M)
