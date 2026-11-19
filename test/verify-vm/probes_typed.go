package main

import (
	"fmt"
	"sort"
	"strings"
)

func renderLuaInputsModule(inputs map[string][]byte, isMoo bool) string {
	var b strings.Builder
	b.WriteString("--!strict\nlocal M: { [string]: buffer } = {}\n")
	keys := make([]string, 0, len(inputs))
	for k := range inputs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		data := inputs[k]
		b.WriteString("do\n")
		fmt.Fprintf(&b, "\tlocal b = buffer.create(%d)\n", len(data))
		for i, c := range data {
			fmt.Fprintf(&b, "\tbuffer.writeu8(b, %d, %d)\n", i, c)
		}
		fmt.Fprintf(&b, "\tM[%q] = b\nend\n", k)
	}
	_ = isMoo
	b.WriteString("return M\n")
	return b.String()
}

func buildLuauExProbe() string {
	var b strings.Builder
	b.WriteString(`--!strict
local serialize = require("./serialize")
local inputs = require("./inputs")
local Types = require("./Types")
local Wire = require("./Wire")
local Clauses = require("./Clauses")
local Joins = require("./Joins")
local Constants = require("./Constants")

local function check(ok: boolean, msg: string)
	if not ok then error("FAILED: " .. msg) end
end
local function bufEq(a: buffer, b: buffer): boolean
	if buffer.len(a) ~= buffer.len(b) then return false end
	for i = 0, buffer.len(a) - 1 do
		if buffer.readu8(a, i) ~= buffer.readu8(b, i) then return false end
	end
	return true
end
local function subBuf(src: buffer, off: number, n: number): buffer
	local out = buffer.create(n)
	buffer.copy(out, 0, src, off, n)
	return out
end
local function rt(mod: any, typ: string, name: string)
	local raw = inputs[name]
	local v = mod["New" .. typ]()
	check(mod["Read" .. typ](serialize.ReadStream.new(raw), v), "luau read " .. name)
	local ws = serialize.WriteStream.new(buffer.create(math.max(256, buffer.len(raw) + 64)))
	check(mod["Write" .. typ](ws, v), "luau write " .. name)
	ws:flush()
	check(bufEq(ws:data(), raw), "luau byte-exact " .. name)
end
local function rtSlices(mod: any, name: string, slices: { { any } })
	local raw = inputs[name]
	local off = 0
	for i, item in ipairs(slices) do
		local typ = item[1] :: string
		local n = item[2] :: number
		local sub = subBuf(raw, off, n)
		local v = mod["New" .. typ]()
		check(mod["Read" .. typ](serialize.ReadStream.new(sub), v), name .. "#" .. tostring(i))
		local ws = serialize.WriteStream.new(buffer.create(256))
		check(mod["Write" .. typ](ws, v), name .. " write#" .. tostring(i))
		ws:flush()
		check(bufEq(ws:data(), sub), name .. " bytes#" .. tostring(i))
		off += n
	end
	check(off == buffer.len(raw), name .. " total len")
end
local _ = Constants
`)
	for _, g := range canonicalSingleGoldens {
		if g.Module != "Ludicrous" {
			fmt.Fprintf(&b, "rt(%s, %q, %q)\n", g.Module, g.Type, g.Name)
		}
	}
	fmt.Fprintf(&b, "rtSlices(Clauses, 'clauses', %s)\n", luaSlicesTable(buildClausesSlices()))
	fmt.Fprintf(&b, "rtSlices(Joins, 'joins', %s)\n", luaSlicesTable(buildJoinsSlices()))
	b.WriteString(`
local rb = Types.NewRigidBody()
local mov, rest, badRb = inputs["rigidbody_moving"], inputs["rigidbody_at_rest"], inputs["bad_rb_trunc"]
check(Types.ReadRigidBody(serialize.ReadStream.new(mov), rb) and not rb.AtRest, "luau rb mov 1")
check(Types.ReadRigidBody(serialize.ReadStream.new(rest), rb) and rb.AtRest and rb.LinearVelocity.X == 0 and rb.AngularVelocity.Y == 0, "luau rb rest zeroed")
local ws = serialize.WriteStream.new(buffer.create(256)); check(Types.WriteRigidBody(ws, rb), "luau rb write rest"); ws:flush(); check(bufEq(ws:data(), rest), "luau rb rest bytes")
check(not Types.ReadRigidBody(serialize.ReadStream.new(badRb), rb), "luau rb trunc refused")
check(Types.ReadRigidBody(serialize.ReadStream.new(mov), rb) and not rb.AtRest, "luau rb mov 2")
print("VERIFY_VM_OK")
`)
	return b.String()
}

func buildLuauLudProbe() string {
	return `--!strict
local serialize = require("./serialize")
local inputs = require("./inputs")
local Ludicrous = require("./Ludicrous")

local function check(ok: boolean, msg: string)
	if not ok then error("FAILED: " .. msg) end
end
local function bufEq(a: buffer, b: buffer): boolean
	if buffer.len(a) ~= buffer.len(b) then return false end
	for i = 0, buffer.len(a) - 1 do
		if buffer.readu8(a, i) ~= buffer.readu8(b, i) then return false end
	end
	return true
end
for _, item in ipairs({
	{ "LudicrousState", "ludicrous_state" },
	{ "LudicrousState", "ludicrous_state_untargeted" },
	{ "DegenerateProbe", "degenerate_probe" },
	{ "UnsignedProbe", "unsigned_probe" },
}) do
	local m: any = Ludicrous
	local raw = inputs[item[2]]
	local v = m["New" .. item[1]]()
	check(m["Read" .. item[1]](serialize.ReadStream.new(raw), v), "luau read " .. item[2])
	local ws = serialize.WriteStream.new(buffer.create(512))
	check(m["Write" .. item[1]](ws, v), "luau write " .. item[2])
	ws:flush()
	check(bufEq(ws:data(), raw), "luau byte-exact " .. item[2])
end
local ls = Ludicrous.NewLudicrousState()
check(Ludicrous.ReadLudicrousState(serialize.ReadStream.new(inputs["ludicrous_state"]), ls) and ls.HasTarget and ls.TargetId.lo == 42i, "luau lud targeted")
check(Ludicrous.ReadLudicrousState(serialize.ReadStream.new(inputs["ludicrous_state_untargeted"]), ls) and not ls.HasTarget and ls.TargetId.lo == 0i and ls.TargetId.hi == 0i, "luau lud untargeted")
print("VERIFY_VM_OK")
`
}

func buildLuauVHProbe() string {
	var b strings.Builder
	b.WriteString(`--!strict
local serialize = require("./serialize")
local Rt = require("./schema_runtime")
local inputs = require("./inputs")
local VH: any = require("./VerifyHostile")

local function check(ok: boolean, msg: string)
	if not ok then error("FAILED: " .. msg) end
end
local function bufEq(a: buffer, b: buffer): boolean
	if buffer.len(a) ~= buffer.len(b) then return false end
	for i = 0, buffer.len(a) - 1 do
		if buffer.readu8(a, i) ~= buffer.readu8(b, i) then return false end
	end
	return true
end
for _, item in ipairs({
	{ "ConstReservedProbe", "valid_const_reserved" },
	{ "AlignProbe", "valid_align" },
	{ "TrailProbe", "valid_trail" },
	{ "RangeProbe", "valid_range" },
	{ "TextProbe", "valid_text" },
	{ "FloatProbe", "valid_float_negzero" },
}) do
	local raw = inputs[item[2]]
	local v = VH["New" .. item[1]]()
	check(VH["Read" .. item[1]](serialize.ReadStream.new(raw), v), "luau read " .. item[2])
	local ws = serialize.WriteStream.new(buffer.create(256))
	check(VH["Write" .. item[1]](ws, v), "luau write " .. item[2])
	ws:flush()
	check(bufEq(ws:data(), raw), "luau byte-exact " .. item[2])
end
`)
	for _, h := range hostileReadCases {
		fmt.Fprintf(&b, "check(not VH.Read%s(serialize.ReadStream.new(inputs[%q]), VH.New%s()), 'luau refuse %s')\n", h.Type, h.Name, h.Type, h.Name)
	}
	b.WriteString(`
local function mustRefuseWrite(fn: (any) -> boolean, what: string)
	local ok, res = pcall(function()
		local ws = serialize.WriteStream.new(buffer.create(128))
		return fn(ws)
	end)
	check(not ok or res == false, "luau must refuse write " .. what)
end
for _, bad in ipairs({ 0/0, math.huge, -math.huge }) do
	mustRefuseWrite(function(ws) local p = VH.NewFloatProbe(); p.F32 = bad; return VH.WriteFloatProbe(ws, p) end, "f32")
	mustRefuseWrite(function(ws) local p = VH.NewFloatProbe(); p.F64 = bad; return VH.WriteFloatProbe(ws, p) end, "f64")
	mustRefuseWrite(function(ws) local p = VH.NewFloatProbe(); p.Cf = bad; return VH.WriteFloatProbe(ws, p) end, "cf")
end
mustRefuseWrite(function(ws) local p = VH.NewTextProbe(); buffer.writeu8(p.Label, 0, 0xff); buffer.writeu8(p.Label, 1, 0x61); p.LabelLength = 2; return VH.WriteTextProbe(ws, p) end, "bad utf8")
mustRefuseWrite(function(ws) local p = VH.NewTextProbe(); buffer.writeu8(p.Label, 0, 0x61); buffer.writeu8(p.Label, 1, 0x00); p.LabelLength = 2; return VH.WriteTextProbe(ws, p) end, "nul utf8")
mustRefuseWrite(function(ws) local p = VH.NewTextProbe(); p.LabelLength = 17; return VH.WriteTextProbe(ws, p) end, "len>16")
mustRefuseWrite(function(ws) local p = VH.NewRangeProbe(); p.Mode = 3; return VH.WriteRangeProbe(ws, p) end, "bad enum")
mustRefuseWrite(function(ws) local p = VH.NewRangeProbe(); p.Small = 9; return VH.WriteRangeProbe(ws, p) end, "small<min")
mustRefuseWrite(function(ws) local p = VH.NewRangeProbe(); p.Small = 21; return VH.WriteRangeProbe(ws, p) end, "small>max")
mustRefuseWrite(function(ws) local p = VH.NewRangeProbe(); p.Wide = Rt.int128(101i, 0i); return VH.WriteRangeProbe(ws, p) end, "wide>max")

local env = VH.NewReuseEnvelope()
for _, st in ipairs({
	{ "reuse_env_1", true, true, 0xdeadbeef, 0, 0 },
	{ "reuse_env_2", true, false, 0, 0x4321, 0 },
	{ "reuse_env_3", false, false, 0, 0, 0xcafebabe },
	{ "reuse_env_4", true, true, 7, 0, 0 },
} :: { { any } }) do
	local k = st[1] :: string
	local raw = inputs[k]
	check(VH.ReadReuseEnvelope(serialize.ReadStream.new(raw), env), "luau read " .. k)
	check(env.Active == st[2] and env.Inner == st[3] and env.A == st[4] and env.B == st[5] and env.C == st[6], "luau state " .. k)
	local ws = serialize.WriteStream.new(buffer.create(128)); check(VH.WriteReuseEnvelope(ws, env), "luau write " .. k); ws:flush(); check(bufEq(ws:data(), raw), "luau bytes " .. k)
	check(not VH.ReadReuseEnvelope(serialize.ReadStream.new(inputs["bad_reuse_trunc"]), env), "luau trunc " .. k)
end
print("VERIFY_VM_OK")
`)
	return b.String()
}

func buildLuauPtrProbe() string {
	return `--!strict
local _GT = require("./GraphTable")
local _MT = require("./MarksTable")
local _PT = require("./PartsTable")
local ST = require("./schema_tables")
local L = require("./schema_load")
local W = require("./schema_save")
local J = require("./schema_json")
local TF = require("./schema_textform")
local V = require("./schema_view")
local _ = { _GT, _MT, _PT }

local function check(ok: boolean, msg: string)
	if not ok then error("FAILED: " .. msg) end
end

local dagJson = '{"name":"dag","version":3,"head":{"&node":1,"value":10,"name":"n1","next":{"&node":2,"value":20,"name":"n2","next":null}},"alias":{"&node":2},"ground":{"depth":4,"head":{"&node":1}}}'
local rep: any = { refused = false }
local s1: any = J.FromJson("Scene", buffer.fromstring(dagJson), rep)
check(s1 ~= nil and not rep.refused, "luau dag FromJson")
check(s1.Head == s1.Ground.Head and s1.Head.Next == s1.Alias, "luau dag pointer identity")
local jb = J.ToJson("Scene", s1, nil)
check(jb ~= nil, "luau dag ToJson")
local j1 = buffer.tostring(jb :: buffer)
check(string.find(j1, '"&node": 1', 1, true) ~= nil and string.find(j1, '"&node": 2', 1, true) ~= nil, "luau dag &node")
local sceneDesc = ST.lookup("Scene")
local wb, werr = W.save(sceneDesc, s1)
check(wb ~= nil and werr == nil, "luau dag Save")
local s2: any = L.load("Scene", wb :: buffer, { refused = false })
check(s2 ~= nil and s2.Head == s2.Ground.Head and s2.Head.Next == s2.Alias, "luau dag Load identity")
local jb2 = J.ToJson("Scene", s2, nil)
check(jb2 ~= nil and buffer.tostring(jb2 :: buffer) == j1, "luau dag wire->json")

for _, item in ipairs({
	{ "self-cycle", '{"head":{"&node":1,"value":1,"next":{"&node":1}}}' },
	{ "forward-ref", '{"head":{"&node":99}}' },
	{ "dup-def", '{"head":{"&node":1,"value":1,"next":null},"alias":{"&node":1,"value":2,"next":null}}' },
}) do
	local br: any = { refused = false }
	local got = J.FromJson("Scene", buffer.fromstring(item[2]), br)
	check(got == nil or br.refused == true, "luau must refuse &node " .. item[1])
end

local sexp = '; comment\n(:name "sexp" :version 7 :head ("&node" 5 :value 42 :name "shared" :next null) :alias ("&node" 5))\n'
local okTf, doc, val, tfErr = pcall(function() return TF.LoadTextForm("Scene", sexp, {}) end)
check(okTf and doc ~= nil and tfErr == nil and val ~= nil and val.Version == 7 and val.Head == val.Alias and val.Head.Value == 42, "luau sexp &node")
local savedSexp = TF.SaveTextForm(doc)
check(savedSexp == sexp, "luau sexp preserved")
local okBad, badDoc, _, badErr = pcall(function() return TF.LoadTextForm("Scene", '(:head ("&node" 99))', {}) end)
check(not okBad or badDoc == nil or badErr ~= nil, "luau sexp forward ref refused")

local uv: any = V.UnitView()
check(uv.package == "graphdemo" and #uv.tables == 11 and #uv.types == 1 and #uv.enums == 1 and #uv.constants == 1, "luau UnitView")
local metaDesc = ST.lookup("Meta")
check(sceneDesc ~= nil and #sceneDesc.fields == 9 and metaDesc ~= nil and #metaDesc.fields == 2, "luau descriptors")
print("VERIFY_VM_OK")
`
}

func buildMooExProbe() string {
	var b strings.Builder
	b.WriteString(`--!strict
local serialize = require("./serialize")
local inputs = require("./inputs")
local Types = require("./Types")
local Wire = require("./Wire")
local Clauses = require("./Clauses")
local Joins = require("./Joins")
local Constants = require("./Constants")
local _ = Constants

local function check(ok: boolean, msg: string)
	if not ok then error("FAILED: " .. msg) end
end
local function bufEq(a: buffer, b: buffer): boolean
	if buffer.len(a) ~= buffer.len(b) then return false end
	for i = 0, buffer.len(a) - 1 do
		if buffer.readu8(a, i) ~= buffer.readu8(b, i) then return false end
	end
	return true
end
local function subBuf(src: buffer, off: number, n: number): buffer
	local out = buffer.create(n)
	buffer.copy(out, 0, src, off, n)
	return out
end
local function rt(mod: any, typ: string, name: string)
	local raw = inputs[name]
	local v = mod["New" .. typ]()
	local rs = serialize.ReadStream.new(raw)
	check(mod["Read" .. typ](rs, v), "moo read " .. name)
	local ws = serialize.WriteStream.new(buffer.create(math.max(256, buffer.len(raw) + 64)))
	check(mod["Write" .. typ](ws, v), "moo write " .. name)
	ws:flush()
	check(bufEq(ws:data(), raw), "moo byte-exact " .. name)
end
local function rtSlices(mod: any, name: string, slices: { { any } })
	local raw = inputs[name]
	local off = 0
	for i, item in ipairs(slices) do
		local typ = item[1] :: string
		local n = item[2] :: number
		local sub = subBuf(raw, off, n)
		local v = mod["New" .. typ]()
		check(mod["Read" .. typ](serialize.ReadStream.new(sub), v), name .. "#" .. tostring(i))
		local ws = serialize.WriteStream.new(buffer.create(256))
		check(mod["Write" .. typ](ws, v), name .. " write#" .. tostring(i))
		ws:flush()
		check(bufEq(ws:data(), sub), name .. " bytes#" .. tostring(i))
		off += n
	end
	check(off == buffer.len(raw), name .. " total len")
end
`)
	for _, g := range canonicalSingleGoldens {
		if g.Module != "Ludicrous" {
			fmt.Fprintf(&b, "rt(%s, %q, %q)\n", g.Module, g.Type, g.Name)
		}
	}
	fmt.Fprintf(&b, "rtSlices(Clauses, 'clauses', %s)\n", luaSlicesTable(buildClausesSlices()))
	fmt.Fprintf(&b, "rtSlices(Joins, 'joins', %s)\n", luaSlicesTable(buildJoinsSlices()))
	b.WriteString(`
local rb = Types.NewRigidBody()
local mov, rest, badRb = inputs["rigidbody_moving"], inputs["rigidbody_at_rest"], inputs["bad_rb_trunc"]
check(Types.ReadRigidBody(serialize.ReadStream.new(mov), rb) and not rb.AtRest, "moo rb mov 1")
check(Types.ReadRigidBody(serialize.ReadStream.new(rest), rb) and rb.AtRest, "moo rb rest")
local ws = serialize.WriteStream.new(buffer.create(256)); check(Types.WriteRigidBody(ws, rb), "moo rb write rest"); ws:flush(); check(bufEq(ws:data(), rest), "moo rb rest bytes")
check(not Types.ReadRigidBody(serialize.ReadStream.new(badRb), rb), "moo rb trunc refused")
check(Types.ReadRigidBody(serialize.ReadStream.new(mov), rb) and not rb.AtRest, "moo rb mov 2")
print("VERIFY_VM_OK")
`)
	return b.String()
}

func buildMooLudProbe() string {
	return `--!strict
local serialize = require("./serialize")
local inputs = require("./inputs")
local Ludicrous: any = require("./Ludicrous")

local function check(ok: boolean, msg: string)
	if not ok then error("FAILED: " .. msg) end
end
local function bufEq(a: buffer, b: buffer): boolean
	if buffer.len(a) ~= buffer.len(b) then return false end
	for i = 0, buffer.len(a) - 1 do
		if buffer.readu8(a, i) ~= buffer.readu8(b, i) then return false end
	end
	return true
end
for _, item in ipairs({
	{ "LudicrousState", "ludicrous_state" },
	{ "LudicrousState", "ludicrous_state_untargeted" },
	{ "DegenerateProbe", "degenerate_probe" },
	{ "UnsignedProbe", "unsigned_probe" },
}) do
	local raw = inputs[item[2]]
	local v = Ludicrous["New" .. item[1]]()
	check(Ludicrous["Read" .. item[1]](serialize.ReadStream.new(raw), v), "moo read " .. item[2])
	local ws = serialize.WriteStream.new(buffer.create(512))
	check(Ludicrous["Write" .. item[1]](ws, v), "moo write " .. item[2])
	ws:flush()
	check(bufEq(ws:data(), raw), "moo byte-exact " .. item[2])
end
local ls = Ludicrous.NewLudicrousState()
check(Ludicrous.ReadLudicrousState(serialize.ReadStream.new(inputs["ludicrous_state"]), ls) and ls.HasTarget, "moo lud targeted")
check(Ludicrous.ReadLudicrousState(serialize.ReadStream.new(inputs["ludicrous_state_untargeted"]), ls) and not ls.HasTarget, "moo lud untargeted")
local ws = serialize.WriteStream.new(buffer.create(512)); check(Ludicrous.WriteLudicrousState(ws, ls), "moo lud write untargeted"); ws:flush(); check(bufEq(ws:data(), inputs["ludicrous_state_untargeted"]), "moo lud untargeted bytes")
print("VERIFY_VM_OK")
`
}

func buildMooVHProbe() string {
	var b strings.Builder
	b.WriteString(`--!strict
local serialize = require("./serialize")
local wide = require("./serialize_wide")
local inputs = require("./inputs")
local VH: any = require("./VerifyHostile")

local function check(ok: boolean, msg: string)
	if not ok then error("FAILED: " .. msg) end
end
local function bufEq(a: buffer, b: buffer): boolean
	if buffer.len(a) ~= buffer.len(b) then return false end
	for i = 0, buffer.len(a) - 1 do
		if buffer.readu8(a, i) ~= buffer.readu8(b, i) then return false end
	end
	return true
end
for _, item in ipairs({
	{ "ConstReservedProbe", "valid_const_reserved" },
	{ "AlignProbe", "valid_align" },
	{ "TrailProbe", "valid_trail" },
	{ "RangeProbe", "valid_range" },
	{ "TextProbe", "valid_text" },
	{ "FloatProbe", "valid_float_negzero" },
}) do
	local raw = inputs[item[2]]
	local v = VH["New" .. item[1]]()
	check(VH["Read" .. item[1]](serialize.ReadStream.new(raw), v), "moo read " .. item[2])
	local ws = serialize.WriteStream.new(buffer.create(256))
	check(VH["Write" .. item[1]](ws, v), "moo write " .. item[2])
	ws:flush()
	check(bufEq(ws:data(), raw), "moo byte-exact " .. item[2])
end
`)
	for _, h := range hostileReadCases {
		fmt.Fprintf(&b, "check(not VH.Read%s(serialize.ReadStream.new(inputs[%q]), VH.New%s()), 'moo refuse %s')\n", h.Type, h.Name, h.Type, h.Name)
	}
	b.WriteString(`
local function mustRefuseWrite(fn: (any) -> boolean, what: string)
	local ok, res = pcall(function()
		local ws = serialize.WriteStream.new(buffer.create(128))
		return fn(ws)
	end)
	check(not ok or res == false, "moo must refuse write " .. what)
end
for _, f32bits in ipairs({ 0x7FC00000, 0x7F800000, 0xFF800000 }) do
	mustRefuseWrite(function(ws) local p = VH.NewFloatProbe(); p.F32 = f32bits; return VH.WriteFloatProbe(ws, p) end, "f32")
	mustRefuseWrite(function(ws) local p = VH.NewFloatProbe(); p.Cf = f32bits; return VH.WriteFloatProbe(ws, p) end, "cf")
end
for _, f64hi in ipairs({ 0x7FF80000, 0x7FF00000, 0xFFF00000 }) do
	mustRefuseWrite(function(ws) local p = VH.NewFloatProbe(); p.F64 = serialize.Uint64.fromWords(0, f64hi); return VH.WriteFloatProbe(ws, p) end, "f64")
end
mustRefuseWrite(function(ws) local p = VH.NewTextProbe(); p.Label = "\xffa"; return VH.WriteTextProbe(ws, p) end, "bad utf8")
mustRefuseWrite(function(ws) local p = VH.NewTextProbe(); p.Label = "a\x00b"; return VH.WriteTextProbe(ws, p) end, "nul utf8")
mustRefuseWrite(function(ws) local p = VH.NewTextProbe(); p.Label = string.rep("a", 17); return VH.WriteTextProbe(ws, p) end, "len>16")
mustRefuseWrite(function(ws) local p = VH.NewRangeProbe(); p.Mode = 3; return VH.WriteRangeProbe(ws, p) end, "bad enum")
mustRefuseWrite(function(ws) local p = VH.NewRangeProbe(); p.Small = 9; return VH.WriteRangeProbe(ws, p) end, "small<min")
mustRefuseWrite(function(ws) local p = VH.NewRangeProbe(); p.Small = 21; return VH.WriteRangeProbe(ws, p) end, "small>max")
mustRefuseWrite(function(ws) local p = VH.NewRangeProbe(); p.Wide = wide.Int128.fromWords(101, 0, 0, 0); return VH.WriteRangeProbe(ws, p) end, "wide>max")

local env = VH.NewReuseEnvelope()
for _, st in ipairs({
	{ "reuse_env_1", true, true, 0xdeadbeef, 0, 0 },
	{ "reuse_env_2", true, false, 0, 0x4321, 0 },
	{ "reuse_env_3", false, false, 0, 0, 0xcafebabe },
	{ "reuse_env_4", true, true, 7, 0, 0 },
} :: { { any } }) do
	local k = st[1] :: string
	local raw = inputs[k]
	check(VH.ReadReuseEnvelope(serialize.ReadStream.new(raw), env), "moo read " .. k)
	check(env.Active == st[2] and env.Inner == st[3] and env.A == st[4] and env.B == st[5] and env.C == st[6], "moo state " .. k)
	local ws = serialize.WriteStream.new(buffer.create(128)); check(VH.WriteReuseEnvelope(ws, env), "moo write " .. k); ws:flush(); check(bufEq(ws:data(), raw), "moo bytes " .. k)
	check(not VH.ReadReuseEnvelope(serialize.ReadStream.new(inputs["bad_reuse_trunc"]), env), "moo trunc " .. k)
end
print("VERIFY_VM_OK")
`)
	return b.String()
}

func buildMooPtrProbe() string {
	return `--!strict
local GT = require("./GraphTable")
local _MT = require("./MarksTable")
local _PT = require("./PartsTable")
local ST = require("./schema_tables")
local L = require("./table_load")
local W = require("./table_save")
local GV = require("./GraphdemoView")
local _ = { _MT, _PT }

local function check(ok: boolean, msg: string)
	if not ok then error("FAILED: " .. msg) end
end

local dagJson = '{"name":"dag","version":3,"head":{"&node":1,"value":10,"name":"n1","next":{"&node":2,"value":20,"name":"n2","next":null}},"alias":{"&node":2},"ground":{"depth":4,"head":{"&node":1}}}'
local s1 = GT.NewScene()
local rep: any = { refused = false }
check(GT.SceneFromJson(s1, dagJson, rep) and not rep.refused, "moo dag FromJson")
check(s1.Head == s1.Ground.Head and s1.Head ~= nil and (s1.Head :: any).Next == s1.Alias, "moo dag pointer identity")
local jlen = GT.SceneToJsonMeasure(s1)
check(jlen > 0, "moo dag ToJsonMeasure")
local jbuf = buffer.create(jlen)
check(GT.SceneToJson(s1, jbuf) == jlen, "moo dag ToJson")
local j1 = buffer.tostring(jbuf)
check(string.find(j1, '"&node": 1', 1, true) ~= nil and string.find(j1, '"&node": 2', 1, true) ~= nil, "moo dag &node")
local sceneDesc = ST.lookup("Scene")
local wb = W.save(sceneDesc, s1)
check(wb ~= nil, "moo dag Save")
local s2: any = L.load("Scene", wb :: buffer, { refused = false })
check(s2 ~= nil and s2.Head == s2.Ground.Head and s2.Head.Next == s2.Alias, "moo dag Load identity")
local jbuf2 = buffer.create(GT.SceneToJsonMeasure(s2))
GT.SceneToJson(s2, jbuf2)
check(buffer.tostring(jbuf2) == j1, "moo dag wire->json")

for _, item in ipairs({
	{ "self-cycle", '{"head":{"&node":1,"value":1,"next":{"&node":1}}}' },
	{ "forward-ref", '{"head":{"&node":99}}' },
	{ "dup-def", '{"head":{"&node":1,"value":1,"next":null},"alias":{"&node":1,"value":2,"next":null}}' },
}) do
	local br: any = { refused = false }
	local ok = GT.SceneFromJson(GT.NewScene(), item[2], br)
	check(not ok or br.refused == true, "moo must refuse &node " .. item[1])
end

local uv = GV.UnitView()
check(uv.package == "graphdemo" and #uv.tables == 11 and #uv.types == 1 and #uv.enums == 1 and #uv.constants == 1, "moo UnitView")
local metaDesc = ST.lookup("Meta")
check(sceneDesc ~= nil and #sceneDesc.fields == 9 and metaDesc ~= nil and #metaDesc.fields == 2, "moo descriptors")
print("VERIFY_VM_OK")
`
}

func buildGooseExProbe(inputsDir string) string {
	var b strings.Builder
	b.WriteString(`import os;
import .Types;
import .Wire;
import .Clauses;
import .Joins;
import .Constants;
import .serialize;

fn check(ok: bool, msg: const u8[:]) {
    if !ok { write_stderr(str("FAILED: ", msg, "\n")); exit(1); }
}
fn same(a: const u8[:], b: const u8[:]) -> bool {
    if a.len != b.len { return false; }
    for i in a.len { if a[i] != b[i] { return false; } }
    return true;
}
`)
	fmt.Fprintf(&b, "fn load_bin(name: const u8[:], out: u8[>..]&) {\n    out.clear();\n    check(read_file(str(%q, \"/\", name, \".bin\"), out), str(\"load \", name));\n}\n", inputsDir)
	b.WriteString("fn main() {\n    check(example::ProtocolId == 0x2bf51af687ac0abd, \"goose examples ProtocolId\");\n    var raw: u8[>..] = [];\n    var pbuf: u8[4112] = [0; 4112];\n    var wbuf: u8[4096] = [0; 4096];\n")
	for _, g := range canonicalSingleGoldens {
		if g.Module == "Ludicrous" {
			continue
		}
		fmt.Fprintf(&b, `    {
        load_bin(%q, raw);
        var v = example::New%s();
        var rs = serialize_read_stream_init_padded(pbuf, raw);
        check(example::Read%s(rs, v), "goose read %s");
        for i in 4096 { wbuf[i] = 0; }
        var ws = serialize_write_stream_init(wbuf);
        check(example::Write%s(ws, v) == SERIALIZE_ERROR_NONE, "goose write %s");
        serialize_flush(ws);
        check(same(wbuf[0..serialize_bytes_processed(ws)], raw), "goose bytes %s");
    }
`, g.Name, g.Type, g.Type, g.Name, g.Type, g.Name, g.Name)
	}
	b.WriteString("    // clauses.bin slices\n    load_bin(\"clauses\", raw);\n    var off: i64 = 0;\n")
	for i, s := range buildClausesSlices() {
		fmt.Fprintf(&b, `    {
        let sub = raw[off..off + %d];
        var v = example::New%s();
        var rs = serialize_read_stream_init_padded(pbuf, sub);
        check(example::Read%s(rs, v), "clauses#%d");
        for k in 512 { wbuf[k] = 0; }
        var ws = serialize_write_stream_init(wbuf);
        check(example::Write%s(ws, v) == SERIALIZE_ERROR_NONE, "clauses write#%d");
        serialize_flush(ws);
        check(same(wbuf[0..serialize_bytes_processed(ws)], sub), "clauses bytes#%d");
        off += %d;
    }
`, s.Bytes, s.TypeName, s.TypeName, i, s.TypeName, i, i, s.Bytes)
	}
	b.WriteString("    check(off == raw.len, \"clauses total\");\n    load_bin(\"joins\", raw);\n    off = 0;\n")
	for i, s := range buildJoinsSlices() {
		fmt.Fprintf(&b, `    {
        let sub = raw[off..off + %d];
        var v = example::New%s();
        var rs = serialize_read_stream_init_padded(pbuf, sub);
        check(example::Read%s(rs, v), "joins#%d");
        for k in 512 { wbuf[k] = 0; }
        var ws = serialize_write_stream_init(wbuf);
        check(example::Write%s(ws, v) == SERIALIZE_ERROR_NONE, "joins write#%d");
        serialize_flush(ws);
        check(same(wbuf[0..serialize_bytes_processed(ws)], sub), "joins bytes#%d");
        off += %d;
    }
`, s.Bytes, s.TypeName, s.TypeName, i, s.TypeName, i, i, s.Bytes)
	}
	b.WriteString(`    check(off == raw.len, "joins total");
    // Stateful RigidBody reuse
    var mov: u8[>..] = []; var rest: u8[>..] = []; var bad_rb: u8[>..] = [];
    load_bin("rigidbody_moving", mov); load_bin("rigidbody_at_rest", rest); load_bin("bad_rb_trunc", bad_rb);
    var rb = example::NewRigidBody();
    var rs1 = serialize_read_stream_init_padded(pbuf, mov);
    check(example::ReadRigidBody(rs1, rb) && !rb.AtRest, "goose rb mov 1");
    var rs2 = serialize_read_stream_init_padded(pbuf, rest);
    check(example::ReadRigidBody(rs2, rb) && rb.AtRest && rb.LinearVelocity.X == 0.0 && rb.AngularVelocity.Y == 0.0, "goose rb rest zeroed");
    for k in 512 { wbuf[k] = 0; }
    var ws_r = serialize_write_stream_init(wbuf);
    check(example::WriteRigidBody(ws_r, rb) == SERIALIZE_ERROR_NONE, "goose rb write rest");
    serialize_flush(ws_r);
    check(same(wbuf[0..serialize_bytes_processed(ws_r)], rest), "goose rb rest bytes");
    var rs3 = serialize_read_stream_init_padded(pbuf, bad_rb);
    check(!example::ReadRigidBody(rs3, rb), "goose rb trunc refused");
    var rs4 = serialize_read_stream_init_padded(pbuf, mov);
    check(example::ReadRigidBody(rs4, rb) && !rb.AtRest, "goose rb mov 2");
    print("VERIFY_VM_OK");
}
`)
	return b.String()
}

func buildGooseLudProbe(inputsDir string) string {
	return fmt.Sprintf(`import os;
import .Ludicrous;
import .serialize;

fn check(ok: bool, msg: const u8[:]) {
    if !ok { write_stderr(str("FAILED: ", msg, "\n")); exit(1); }
}
fn same(a: const u8[:], b: const u8[:]) -> bool {
    if a.len != b.len { return false; }
    for i in a.len { if a[i] != b[i] { return false; } }
    return true;
}
fn main() {
    check(ludicrous::ProtocolId == 0xd2ded2cc8a482aa1, "goose ludicrous ProtocolId");
    var tw: u8[>..] = []; var uw: u8[>..] = []; var dp: u8[>..] = []; var up: u8[>..] = [];
    check(read_file(%q, tw), "tw"); check(read_file(%q, uw), "uw");
    check(read_file(%q, dp), "dp"); check(read_file(%q, up), "up");
    var pbuf: u8[528] = [0; 528];
    var wbuf: u8[512] = [0; 512];
    var ls = ludicrous::NewLudicrousState();
    var r1 = serialize_read_stream_init_padded(pbuf, tw);
    check(ludicrous::ReadLudicrousState(r1, ls) && ls.HasTarget && ls.TargetId.lo == 42, "goose lud targeted");
    var w1 = serialize_write_stream_init(wbuf); check(ludicrous::WriteLudicrousState(w1, ls) == SERIALIZE_ERROR_NONE, "w1"); serialize_flush(w1);
    check(same(wbuf[0..serialize_bytes_processed(w1)], tw), "tw bytes");
    var r2 = serialize_read_stream_init_padded(pbuf, uw);
    check(ludicrous::ReadLudicrousState(r2, ls) && !ls.HasTarget && ls.TargetId.lo == 0 && ls.TargetId.hi == 0, "goose lud untargeted");
    for k in 512 { wbuf[k] = 0; }
    var w2 = serialize_write_stream_init(wbuf); check(ludicrous::WriteLudicrousState(w2, ls) == SERIALIZE_ERROR_NONE, "w2"); serialize_flush(w2);
    check(same(wbuf[0..serialize_bytes_processed(w2)], uw), "uw bytes");
    var dv = ludicrous::NewDegenerateProbe(); var r3 = serialize_read_stream_init_padded(pbuf, dp);
    check(ludicrous::ReadDegenerateProbe(r3, dv), "dp");
    var uv = ludicrous::NewUnsignedProbe(); var r4 = serialize_read_stream_init_padded(pbuf, up);
    check(ludicrous::ReadUnsignedProbe(r4, uv), "up");
    print("VERIFY_VM_OK");
}
`, inputsDir+"/ludicrous_state.bin", inputsDir+"/ludicrous_state_untargeted.bin", inputsDir+"/degenerate_probe.bin", inputsDir+"/unsigned_probe.bin")
}

func buildGooseVHProbe(inputsDir string) string {
	var b strings.Builder
	b.WriteString(`import os;
import .VerifyHostile;
import .schema_rt;
import .serialize;

fn check(ok: bool, msg: const u8[:]) {
    if !ok { write_stderr(str("FAILED: ", msg, "\n")); exit(1); }
}
fn same(a: const u8[:], b: const u8[:]) -> bool {
    if a.len != b.len { return false; }
    for i in a.len { if a[i] != b[i] { return false; } }
    return true;
}
`)
	fmt.Fprintf(&b, "fn load_bin(name: const u8[:], out: u8[>..]&) {\n    out.clear();\n    check(read_file(str(%q, \"/\", name, \".bin\"), out), name);\n}\n", inputsDir)
	b.WriteString("fn main() {\n    check(verifypkt::ProtocolId == 0x962f976c95dcf12e, \"goose verifypkt ProtocolId\");\n    var raw: u8[>..] = [];\n    var pbuf: u8[272] = [0; 272];\n    var wbuf: u8[256] = [0; 256];\n")
	for _, v := range []struct{ n, t string }{
		{"valid_const_reserved", "ConstReservedProbe"},
		{"valid_align", "AlignProbe"},
		{"valid_trail", "TrailProbe"},
		{"valid_range", "RangeProbe"},
		{"valid_text", "TextProbe"},
		{"valid_float_negzero", "FloatProbe"},
	} {
		fmt.Fprintf(&b, `    {
        load_bin(%q, raw);
        var val = verifypkt::New%s();
        var rs = serialize_read_stream_init_padded(pbuf, raw);
        check(verifypkt::Read%s(rs, val), "read %s");
        for k in 256 { wbuf[k] = 0; }
        var ws = serialize_write_stream_init(wbuf);
        check(verifypkt::Write%s(ws, val) == SERIALIZE_ERROR_NONE, "write %s");
        serialize_flush(ws);
        check(same(wbuf[0..serialize_bytes_processed(ws)], raw), "bytes %s");
    }
`, v.n, v.t, v.t, v.n, v.t, v.n, v.n)
	}
	for _, h := range hostileReadCases {
		fmt.Fprintf(&b, `    {
        load_bin(%q, raw);
        var val = verifypkt::New%s();
        var rs = serialize_read_stream_init_padded(pbuf, raw);
        check(!verifypkt::Read%s(rs, val), "refuse %s");
    }
`, h.Name, h.Type, h.Type, h.Name)
	}
	b.WriteString(`    // Hostile write refusals
    {
        let zero64: f64 = 0.0;
        let nan64: f64 = 0.0 / zero64;
        let inf64: f64 = 1.0 / zero64;
        let nan32: f32 = nan64 as! f32;
        let inf32: f32 = inf64 as! f32;
        var ws = serialize_write_stream_init(wbuf);
        var p1 = verifypkt::NewFloatProbe(); p1.F32 = nan32; check(verifypkt::WriteFloatProbe(ws, p1) != SERIALIZE_ERROR_NONE, "w f32 nan");
        var p2 = verifypkt::NewFloatProbe(); p2.F32 = inf32; check(verifypkt::WriteFloatProbe(ws, p2) != SERIALIZE_ERROR_NONE, "w f32 inf");
        var p3 = verifypkt::NewFloatProbe(); p3.F64 = nan64; check(verifypkt::WriteFloatProbe(ws, p3) != SERIALIZE_ERROR_NONE, "w f64 nan");
        var p4 = verifypkt::NewFloatProbe(); p4.F64 = inf64; check(verifypkt::WriteFloatProbe(ws, p4) != SERIALIZE_ERROR_NONE, "w f64 inf");
        var p5 = verifypkt::NewFloatProbe(); p5.Cf = nan32; check(verifypkt::WriteFloatProbe(ws, p5) != SERIALIZE_ERROR_NONE, "w cf nan");
        var t1 = verifypkt::NewTextProbe(); t1.Label[0] = 0xff; t1.Label[1] = 0x61; t1.LabelLength = 2; check(verifypkt::WriteTextProbe(ws, t1) != SERIALIZE_ERROR_NONE, "w utf8");
        var t2 = verifypkt::NewTextProbe(); t2.Label[0] = 0x61; t2.Label[1] = 0x00; t2.LabelLength = 2; check(verifypkt::WriteTextProbe(ws, t2) != SERIALIZE_ERROR_NONE, "w nul");
        var t3 = verifypkt::NewTextProbe(); t3.LabelLength = 17; check(verifypkt::WriteTextProbe(ws, t3) != SERIALIZE_ERROR_NONE, "w len>16");
        var r1 = verifypkt::NewRangeProbe(); r1.Mode = 3; check(verifypkt::WriteRangeProbe(ws, r1) != SERIALIZE_ERROR_NONE, "w enum");
        var r2 = verifypkt::NewRangeProbe(); r2.Small = 9; check(verifypkt::WriteRangeProbe(ws, r2) != SERIALIZE_ERROR_NONE, "w small<min");
        var r3 = verifypkt::NewRangeProbe(); r3.Small = 21; check(verifypkt::WriteRangeProbe(ws, r3) != SERIALIZE_ERROR_NONE, "w small>max");
        var r4 = verifypkt::NewRangeProbe(); r4.Wide = schema_rt::Int128 { lo: 101, hi: 0 }; check(verifypkt::WriteRangeProbe(ws, r4) != SERIALIZE_ERROR_NONE, "w wide>max");
    }
    // Stateful ReuseEnvelope reuse
    var env = verifypkt::NewReuseEnvelope();
    var trunc_b: u8[>..] = []; load_bin("bad_reuse_trunc", trunc_b);
    {
        load_bin("reuse_env_1", raw); var rs = serialize_read_stream_init_padded(pbuf, raw);
        check(verifypkt::ReadReuseEnvelope(rs, env) && env.Active && env.Inner && env.A == 0xdeadbeef && env.B == 0 && env.C == 0, "goose env 1");
        load_bin("reuse_env_2", raw); rs = serialize_read_stream_init_padded(pbuf, raw);
        check(verifypkt::ReadReuseEnvelope(rs, env) && env.Active && !env.Inner && env.A == 0 && env.B == 0x4321 && env.C == 0, "goose env 2");
        var rb = serialize_read_stream_init_padded(pbuf, trunc_b);
        check(!verifypkt::ReadReuseEnvelope(rb, env), "goose env trunc");
        load_bin("reuse_env_3", raw); rs = serialize_read_stream_init_padded(pbuf, raw);
        check(verifypkt::ReadReuseEnvelope(rs, env) && !env.Active && !env.Inner && env.A == 0 && env.B == 0 && env.C == 0xcafebabe, "goose env 3");
    }
    print("VERIFY_VM_OK");
}
`)
	return b.String()
}

func buildGoosePtrProbe() string {
	return `import os;
import .GraphTable;
import .MarksTable;
import .PartsTable;
import .GraphdemoView;
import .graphdemo_unit;
import .graphdemo_json;
import .json_value;
import .wire_load;
import .wire_save;
import .schema_rt;

fn check(ok: bool, msg: const u8[:]) {
    if !ok { write_stderr(str("FAILED: ", msg, "\n")); exit(1); }
}
fn has_sub(hay: const u8[:], needle: const u8[:]) -> bool {
    if needle.len > hay.len { return false; }
    for i in (hay.len - needle.len + 1) {
        var ok = true;
        for j in needle.len { if hay[i + j] != needle[j] { ok = false; break; } }
        if ok { return true; }
    }
    return false;
}
fn main() {
    let dag_json = "{\"name\":\"dag\",\"version\":3,\"head\":{\"&node\":1,\"value\":10,\"name\":\"n1\",\"next\":{\"&node\":2,\"value\":20,\"name\":\"n2\",\"next\":null}},\"alias\":{\"&node\":2},\"ground\":{\"depth\":4,\"head\":{\"&node\":1}}}";
    var data: u8[>..] = []; var vals: schema_rt::LoadVal[>..] = []; var nodes: schema_rt::LoadNode[>..] = [];
    var jr = schema_rt::json_report();
    let root = schema_rt::json_load_table(graphdemo::TableUnit, graphdemo::TextUnit, "Scene", dag_json, data, vals, nodes, jr);
    check(root >= 0 && !jr.damaged, "goose dag json_load_table");
    var j1: u8[>..] = [];
    check(schema_rt::json_save_table(graphdemo::TableUnit, graphdemo::TextUnit, "Scene", data, vals, nodes, root, j1) >= 0, "goose dag json_save_table");
    check(has_sub(j1, "\"&node\": 1") && has_sub(j1, "\"&node\": 2"), "goose dag &node emitted");
    var w1: u8[>..<] = [];
    check(schema_rt::save_table(graphdemo::TableUnit, "Scene", data, vals, nodes, root, w1) == 0, "goose dag save_table");
    var d2: u8[>..] = []; d2.append(w1); var v2: schema_rt::LoadVal[>..] = []; var n2: schema_rt::LoadNode[>..] = [];
    var lrep = schema_rt::LoadReport { .. };
    let r2 = schema_rt::load_table(graphdemo::TableUnit, "Scene", d2, v2, n2, lrep);
    check(r2 >= 0 && !lrep.refused, "goose dag load_table");
    var j2: u8[>..] = [];
    check(schema_rt::json_save_table(graphdemo::TableUnit, graphdemo::TextUnit, "Scene", d2, v2, n2, r2, j2) >= 0, "goose dag j2");
    check(j1.len == j2.len, "goose dag wire->json len");
    for i in j1.len { check(j1[i] == j2[i], "goose dag wire->json byte"); }

    let bads: (const u8[:])[3] = [
        "{\"head\":{\"&node\":1,\"value\":1,\"next\":{\"&node\":1}}}",
        "{\"head\":{\"&node\":99}}",
        "{\"head\":{\"&node\":1,\"value\":1,\"next\":null},\"alias\":{\"&node\":1,\"value\":2,\"next\":null}}"
    ];
    for b in bads {
        var bd: u8[>..] = []; var bv: schema_rt::LoadVal[>..] = []; var bn: schema_rt::LoadNode[>..] = [];
        var br = schema_rt::json_report();
        let broot = schema_rt::json_load_table(graphdemo::TableUnit, graphdemo::TextUnit, "Scene", b, bd, bv, bn, br);
        check(broot < 0 || br.damaged, "goose must refuse bad &node");
    }

    let uv = graphdemo::UnitView();
    check(uv.num_tables == 11 && uv.num_types == 1 && uv.num_enums == 1 && uv.num_constants == 1, "goose UnitView");
    check(graphdemo::SceneDescriptor.fields.len == 9 && graphdemo::MetaDescriptor.fields.len == 2, "goose descriptors");
    print("VERIFY_VM_OK");
}
`
}

func buildLobsterExProbe(inputsDir string) string {
	var b strings.Builder
	b.WriteString(`import .Types
import .Wire
import .Clauses
import .Joins
import .Constants
import .serialize_stream

def check(ok:bool, msg:string):
    if not ok:
        print("FAILED: " + msg)
        assert false

def load_bin(name:string) -> string:
    let d = read_file("` + inputsDir + `/" + name + ".bin")
    assert d
    return d

def again_eq(raw:string, cb) -> bool:
    let w = serialize_write_stream(schema_rt.zeros(max(256, length(raw) + 64)))
    if not cb(w):
        return false
    w.flush()
    return substring(w.buffer, 0, w.bytes_processed()) == raw

check(example.ProtocolId == 0x2bf51af687ac0abd, "lobster examples ProtocolId")
`)
	for _, g := range canonicalSingleGoldens {
		if g.Module == "Ludicrous" {
			continue
		}
		fmt.Fprintf(&b, `if true:
    let raw = load_bin(%q)
    let v = example.New%s()
    let rs = serialize_read_stream_padded(raw)
    check(example.Read%s(rs, v) and rs.serialize_end(), "lobster read %s")
    check(again_eq(raw, fn(w): example.Write%s(w, v)), "lobster write %s")
`, g.Name, g.Type, g.Type, g.Name, g.Type, g.Name)
	}
	b.WriteString("let clRaw = load_bin(\"clauses\")\nvar clOff = 0\n")
	for i, s := range buildClausesSlices() {
		fmt.Fprintf(&b, `if true:
    let sub = substring(clRaw, clOff, %d)
    let v = example.New%s()
    let rs = serialize_read_stream_padded(sub)
    check(example.Read%s(rs, v) and rs.serialize_end(), "clauses#%d")
    check(again_eq(sub, fn(w): example.Write%s(w, v)), "clauses write#%d")
    clOff += %d
`, s.Bytes, s.TypeName, s.TypeName, i, s.TypeName, i, s.Bytes)
	}
	b.WriteString("check(clOff == length(clRaw), \"clauses total\")\nlet jRaw = load_bin(\"joins\")\nvar jOff = 0\n")
	for i, s := range buildJoinsSlices() {
		fmt.Fprintf(&b, `if true:
    let sub = substring(jRaw, jOff, %d)
    let v = example.New%s()
    let rs = serialize_read_stream_padded(sub)
    check(example.Read%s(rs, v) and rs.serialize_end(), "joins#%d")
    check(again_eq(sub, fn(w): example.Write%s(w, v)), "joins write#%d")
    jOff += %d
`, s.Bytes, s.TypeName, s.TypeName, i, s.TypeName, i, s.Bytes)
	}
	b.WriteString(`check(jOff == length(jRaw), "joins total")
let rb = example.NewRigidBody()
let mov = load_bin("rigidbody_moving")
let rest = load_bin("rigidbody_at_rest")
let badRb = load_bin("bad_rb_trunc")
let r1 = serialize_read_stream_padded(mov)
check(example.ReadRigidBody(r1, rb) and r1.serialize_end() and not rb.AtRest, "lobster rb mov 1")
let r2 = serialize_read_stream_padded(rest)
check(example.ReadRigidBody(r2, rb) and r2.serialize_end() and rb.AtRest and rb.LinearVelocity.X == 0.0 and rb.AngularVelocity.Y == 0.0, "lobster rb rest zeroed")
check(again_eq(rest, fn(w): example.WriteRigidBody(w, rb)), "lobster rb rest bytes")
let r3 = serialize_read_stream_padded(badRb)
check(not (example.ReadRigidBody(r3, rb) and r3.serialize_end()), "lobster rb trunc")
let r4 = serialize_read_stream_padded(mov)
check(example.ReadRigidBody(r4, rb) and r4.serialize_end() and not rb.AtRest, "lobster rb mov 2")
print("VERIFY_VM_OK")
`)
	return b.String()
}

func buildLobsterLudProbe(inputsDir string) string {
	return fmt.Sprintf(`import .Ludicrous
import .serialize_stream

def check(ok:bool, msg:string):
    if not ok:
        print("FAILED: " + msg)
        assert false

def load_bin(name:string) -> string:
    let d = read_file(%q + "/" + name + ".bin")
    assert d
    return d

check(ludicrous.ProtocolId == 0xd2ded2cc8a482aa1, "lobster ludicrous ProtocolId")
let tw = load_bin("ludicrous_state")
let uw = load_bin("ludicrous_state_untargeted")
let ls = ludicrous.NewLudicrousState()
let r1 = serialize_read_stream_padded(tw)
check(ludicrous.ReadLudicrousState(r1, ls) and r1.serialize_end() and ls.HasTarget and ls.TargetId.lo == 42, "lobster lud targeted")
let w1 = serialize_write_stream(schema_rt.zeros(512))
check(ludicrous.WriteLudicrousState(w1, ls), "w1")
w1.flush()
check(substring(w1.buffer, 0, w1.bytes_processed()) == tw, "tw bytes")
let r2 = serialize_read_stream_padded(uw)
check(ludicrous.ReadLudicrousState(r2, ls) and r2.serialize_end() and not ls.HasTarget and ls.TargetId.lo == 0 and ls.TargetId.hi == 0, "lobster lud untargeted")
let w2 = serialize_write_stream(schema_rt.zeros(512))
check(ludicrous.WriteLudicrousState(w2, ls), "w2")
w2.flush()
check(substring(w2.buffer, 0, w2.bytes_processed()) == uw, "uw bytes")
print("VERIFY_VM_OK")
`, inputsDir)
}

func buildLobsterVHProbe(inputsDir string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `import .VerifyHostile
import .serialize_stream

let trapCase = if length(command_line_arguments()) > 0: command_line_arguments()[0] else: ""
if trapCase == "f32_nan":
    let w = serialize_write_stream(schema_rt.zeros(128))
    let p = verifypkt.NewFloatProbe()
    p.F32 = 0.0 / 0.0
    if not verifypkt.WriteFloatProbe(w, p): assert false
    return
if trapCase == "bad_utf8":
    let w = serialize_write_stream(schema_rt.zeros(128))
    let p = verifypkt.NewTextProbe()
    p.Label = "\xFF\x61" + schema_rt.zeros(14)
    p.LabelLength = 2
    if not verifypkt.WriteTextProbe(w, p): assert false
    return
if trapCase == "bad_range":
    let w = serialize_write_stream(schema_rt.zeros(128))
    let p = verifypkt.NewRangeProbe()
    p.Small = 21
    if not verifypkt.WriteRangeProbe(w, p): assert false
    return

def check(ok:bool, msg:string):
    if not ok:
        print("FAILED: " + msg)
        assert false

def load_bin(name:string) -> string:
    let d = read_file(%q + "/" + name + ".bin")
    assert d
    return d

check(verifypkt.ProtocolId == 0x962f976c95dcf12e, "lobster verifypkt ProtocolId")
`, inputsDir)
	for _, v := range []struct{ n, t string }{
		{"valid_const_reserved", "ConstReservedProbe"},
		{"valid_align", "AlignProbe"},
		{"valid_trail", "TrailProbe"},
		{"valid_range", "RangeProbe"},
		{"valid_text", "TextProbe"},
		{"valid_float_negzero", "FloatProbe"},
	} {
		fmt.Fprintf(&b, `if true:
    let raw = load_bin(%q)
    let val = verifypkt.New%s()
    let rs = serialize_read_stream_padded(raw)
    check(verifypkt.Read%s(rs, val) and rs.serialize_end(), "read %s")
    let ws = serialize_write_stream(schema_rt.zeros(256))
    check(verifypkt.Write%s(ws, val), "write %s")
    ws.flush()
    check(substring(ws.buffer, 0, ws.bytes_processed()) == raw, "bytes %s")
`, v.n, v.t, v.t, v.n, v.t, v.n, v.n)
	}
	for _, h := range hostileReadCases {
		fmt.Fprintf(&b, `if true:
    let raw = load_bin(%q)
    let val = verifypkt.New%s()
    let rs = serialize_read_stream_padded(raw)
    check(not (verifypkt.Read%s(rs, val) and rs.serialize_end()), "refuse %s")
`, h.Name, h.Type, h.Type, h.Name)
	}
	b.WriteString(`let env = verifypkt.NewReuseEnvelope()
let truncB = load_bin("bad_reuse_trunc")
let envNames = ["reuse_env_1", "reuse_env_2", "reuse_env_3", "reuse_env_4"]
let envVals = [
    [1, 1, 0xdeadbeef, 0, 0],
    [1, 0, 0, 0x4321, 0],
    [0, 0, 0, 0, 0xcafebabe],
    [1, 1, 7, 0, 0],
]
for(envNames) name, idx:
    let st = envVals[idx]
    let raw = load_bin(name)
    let rs = serialize_read_stream_padded(raw)
    check(verifypkt.ReadReuseEnvelope(rs, env) and rs.serialize_end(), "read " + name)
    check((if env.Active: 1 else: 0) == st[0] and (if env.Inner: 1 else: 0) == st[1] and env.A == st[2] and env.B == st[3] and env.C == st[4], "state " + name)
    let ws = serialize_write_stream(schema_rt.zeros(128))
    check(verifypkt.WriteReuseEnvelope(ws, env), "write " + name)
    ws.flush()
    check(substring(ws.buffer, 0, ws.bytes_processed()) == raw, "bytes " + name)
    let rb = serialize_read_stream_padded(truncB)
    check(not (verifypkt.ReadReuseEnvelope(rb, env) and rb.serialize_end()), "trunc " + name)
print("VERIFY_VM_OK")
`)
	return b.String()
}

func buildLobsterPtrProbe() string {
	return `import .GraphTable
import .MarksTable
import .PartsTable
import .graphdemoView
import .json
import .load
import .schema_save
import .text_form

def check(ok:bool, msg:string):
    if not ok:
        print("FAILED: " + msg)
        assert false

let dagJson = "\{\"name\":\"dag\",\"version\":3,\"head\":\{\"&node\":1,\"value\":10,\"name\":\"n1\",\"next\":\{\"&node\":2,\"value\":20,\"name\":\"n2\",\"next\":null\}\},\"alias\":\{\"&node\":2\},\"ground\":\{\"depth\":4,\"head\":\{\"&node\":1\}\}\}"
let rep = schema_rt.LoadReport {}
let s1 = schema_rt.json_read("Scene", dagJson, rep)
check(s1 != nil and not rep.refused, "lobster dag json_read")
if s1:
    let j1, jwhy = schema_rt.json_write(s1)
    check(jwhy == "" and find_string(j1, "\"&node\": 1") >= 0 and find_string(j1, "\"&node\": 2") >= 0, "lobster dag &node")
    let w1, wwhy = schema_rt.save_reasoned(s1)
    check(wwhy == "" and length(w1) > 0, "lobster dag save")
    let s2 = schema_rt.load("Scene", w1, schema_rt.LoadReport {})
    check(s2 != nil, "lobster dag load")
    if s2:
        let j2, jwhy2 = schema_rt.json_write(s2)
        check(jwhy2 == "" and j2 == j1, "lobster dag wire->json")

for([
    "\{\"head\":\{\"&node\":1,\"value\":1,\"next\":\{\"&node\":1\}\}\}",
    "\{\"head\":\{\"&node\":99\}\}",
    "\{\"head\":\{\"&node\":1,\"value\":1,\"next\":null\},\"alias\":\{\"&node\":1,\"value\":2,\"next\":null\}\}",
]) bad:
    let br = schema_rt.LoadReport {}
    let got = schema_rt.json_read("Scene", bad, br)
    check(got == nil or br.refused, "lobster must refuse bad &node")

let sexp = "; comment\n(:name \"sexp\" :version 7 :head (\"&node\" 5 :value 42 :name \"shared\" :next null) :alias (\"&node\" 5))\n"
let doc = schema_rt.text_load("Scene", sexp)
check(doc != nil, "lobster sexp &node")
if doc:
    check(schema_rt.text_save(doc) == sexp, "lobster sexp preserved")
check(schema_rt.text_load("Scene", "(:head (\"&node\" 99))") == nil, "lobster sexp forward ref refused")

let uv = graphdemo.UnitView()
check(uv.package == "graphdemo" and length(uv.tables) == 11 and length(uv.types) == 1 and length(uv.enums) == 1 and length(uv.constants) == 1, "lobster UnitView")
check(length(graphdemo.SceneDescriptor.fields) == 9 and length(graphdemo.MetaDescriptor.fields) == 2, "lobster descriptors")
print("VERIFY_VM_OK")
`
}
