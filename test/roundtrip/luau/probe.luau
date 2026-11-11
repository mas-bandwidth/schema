-- The round-trip gate's Luau probe (test/roundtrip). The gate copies it into
-- the generated directory beside roundtrip_inputs.luau, which it writes: the
-- unit's modules, and every case's sample text and engine bytes (the luau CLI
-- has no io library, so the inputs are a module and the bytes the probe saves
-- are printed as `RTB <root> <sample> <ext> <hex>` lines).
-- Every call is the generated API: the runtime modules' entry points over the
-- unit's descriptors, by root name.
local inputs: any = require("./roundtrip_inputs")
local S: any = require("./schema_tables")
for _, m in inputs.modules do
	require("./" .. m)
end
local L: any = require("./schema_load")
local SV: any = require("./schema_save")
local J: any = require("./schema_json")
local F: any = require("./schema_fixed")
local R: any = require("./schema_retain")

local function say(root: string, name: string, verb: string, verdict: string)
	print("RT " .. root .. " " .. name .. " " .. verb .. " " .. (string.gsub(verdict, "%s+", " ")))
end

local function keep(root: string, name: string, ext: string, b: buffer)
	local s = buffer.tostring(b)
	local hex = table.create(#s)
	for i = 1, #s do
		hex[i] = string.format("%02x", string.byte(s, i))
	end
	print("RTB " .. root .. " " .. name .. " " .. ext .. " " .. table.concat(hex))
end

local function why(r: any, e: any): string
	if type(r) == "table" and r.reason ~= nil then
		return tostring(r.reason)
	end
	if type(e) == "table" and e.name ~= nil then
		return tostring(e.name)
	end
	return tostring(e)
end

-- call answers f's verdict, and an error as a refusal by name.
local function call(f: () -> string): string
	local ok, v = pcall(f)
	if ok then
		return v
	end
	return "refused:error " .. why(nil, v)
end

for _, c in inputs.cases do
	local root, name, class = c.root, c.name, c.class
	local okd, d = pcall(S.lookup, root)
	if not okd or d == nil then
		say(root, name, "probe", "refused:the unit generated no such root")
		continue
	end
	local v: any = nil
	say(root, name, "fromjson", call(function()
		local r = {}
		v = J.FromJson(root, buffer.fromstring(c.text), r)
		if v == nil then
			return "refused:" .. why(r)
		end
		return "ok"
	end))
	if v == nil then
		continue
	end
	local function tj(x: any): string?
		local b = J.ToJson(root, x, {})
		return if b then buffer.tostring(b) else nil
	end
	local want = tj(v)
	local function same(x: any): string
		local got = tj(x)
		if want == nil or got == nil then
			return "refused:ToJson refuses the value"
		end
		return if got == want then "ok" else "diff"
	end
	local function loadVerdict(b: buffer): string
		local r = {}
		local got, e = L.load(root, b, r)
		if got == nil then
			return "refused:" .. why(r, e)
		end
		return same(got)
	end
	local eng = if c.eng ~= nil then buffer.fromstring(c.eng) else nil
	local wire: buffer? = nil
	say(root, name, "save", call(function()
		local w, e = SV.save(d, v)
		if w == nil then
			return "refused:" .. why(nil, e)
		end
		wire = w
		keep(root, name, ".save", w)
		return "ok"
	end))
	if wire ~= nil then
		say(root, name, "load", call(function()
			return loadVerdict(wire :: buffer)
		end))
	end
	if eng ~= nil then
		say(root, name, "loadeng", call(function()
			return loadVerdict(eng :: buffer)
		end))
	end
	if class == "fixed" then
		say(root, name, "fixedsave", call(function()
			local fw = F.save(d, nil, { v })
			keep(root, name, ".fixed", fw)
			return "ok"
		end))
		if eng ~= nil then
			say(root, name, "fixedloadeng", call(function()
				local vals = F.load(d, nil, eng :: buffer, {})
				if vals == nil or vals[1] == nil then
					return "refused:no value"
				end
				return same(vals[1])
			end))
		end
	end
	if class == "variable" then
		if wire ~= nil then
			say(root, name, "retain", call(function()
				local r = {}
				local got, store = R.LoadRetain(root, wire :: buffer, r)
				if got == nil then
					return "refused:" .. why(r)
				end
				local back, e = R.SaveRetain(root, got, store, {})
				if back == nil then
					return "refused:" .. why(nil, e)
				end
				if buffer.tostring(back) ~= buffer.tostring(wire :: buffer) then
					return "diff"
				end
				return same(got)
			end))
		end
		say(root, name, "json", call(function()
			if want == nil then
				return "refused:ToJson refuses the value"
			end
			local r = {}
			local j = J.FromJson(root, buffer.fromstring(want), r)
			if j == nil then
				return "refused:" .. why(r)
			end
			return same(j)
		end))
	end
end
