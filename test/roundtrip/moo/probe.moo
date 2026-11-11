-- The round-trip gate's moo probe (test/roundtrip). The gate copies it into
-- the generated directory beside roundtrip_inputs.moo, which it writes: the
-- unit's <Base>Table modules, and every case's sample text and engine bytes
-- (the moo CLI has no io library, so the inputs are a module and the bytes the
-- probe saves are printed as `RTB <root> <sample> <ext> <hex>` lines).
-- Every call is the generated API: the unit module's per-root entry points
-- (New<Root>, <Root>FromJson, <Root>ToJson, the <Root>Fixed descriptor) found
-- by the root's name, and the runtime modules' entry points over the root's
-- descriptor.
local load = require :: any
local inputs: any = load("./roundtrip_inputs")
local S: any = load("./schema_tables")
local mods: { any } = {}
for _, m in inputs.modules do
	table.insert(mods, load("./" .. m))
end
local L: any = load("./table_load")
local W: any = load("./table_save")
local F: any = load("./table_fixed")
local RF: any = load("./table_refusal")
local RT: any = load("./table_retain")

-- the retaining load's capacities, roomy (test/conformance/moo/retain.moo)
local CAPACITY = 1048576
local IDS = 131072

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

-- moduleOf is the unit module that declares root.
local function moduleOf(root: string): any?
	for _, m in mods do
		if type(m) == "table" and m[root .. "FromJson"] ~= nil then
			return m
		end
	end
	return nil
end

for _, c in inputs.cases do
	local root, name, class = c.root, c.name, c.class
	local M = moduleOf(root)
	local okd, d = pcall(S.lookup, root)
	if M == nil or not okd or d == nil then
		say(root, name, "probe", "refused:the unit generated no such root")
		continue
	end
	local function new(): any
		return M["New" .. root]()
	end
	local function tj(x: any): string?
		local n = M[root .. "ToJsonMeasure"](x)
		if n == nil or n < 0 then
			return nil
		end
		local b = buffer.create(n)
		if M[root .. "ToJson"](x, b) ~= n then
			return nil
		end
		return buffer.tostring(b)
	end
	local v: any = nil
	say(root, name, "fromjson", call(function()
		local r = L.newReport()
		local x = new()
		if not M[root .. "FromJson"](x, c.text, r) then
			return "refused:" .. why(r)
		end
		v = x
		return "ok"
	end))
	if v == nil then
		continue
	end
	local want = tj(v)
	local function same(x: any): string
		local got = tj(x)
		if want == nil or got == nil then
			return "refused:ToJson refuses the value"
		end
		return if got == want then "ok" else "diff"
	end
	-- a read that names a refusal or damage answered no value, whatever it
	-- handed back beside the name
	local function verdictOf(got: any, r: any): string
		if got == nil or r.damaged or r.reason ~= nil then
			return "refused:" .. why(r, got)
		end
		return same(got)
	end
	local function loadVerdict(b: buffer): string
		local r = L.newReport()
		return verdictOf(L.load(root, b, r), r)
	end
	local function refusalVerdict(b: buffer): string
		local r = L.newReport()
		return verdictOf(RF.ReadStrict(M, root, b, r), r)
	end
	local eng = if c.eng ~= nil then buffer.fromstring(c.eng) else nil
	local wire: buffer? = nil
	say(root, name, "save", call(function()
		local w, e = W.save(d, v)
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
		say(root, name, "load.refusal", call(function()
			return refusalVerdict(wire :: buffer)
		end))
	end
	if eng ~= nil then
		say(root, name, "loadeng", call(function()
			return loadVerdict(eng :: buffer)
		end))
		say(root, name, "load.refusaleng", call(function()
			return refusalVerdict(eng :: buffer)
		end))
	end
	if class == "fixed" then
		local fixed = M[root .. "Fixed"]
		say(root, name, "fixedsave", call(function()
			if fixed == nil then
				return "refused:the unit generated no fixed form for this root"
			end
			local fw, e = F.save(d, fixed, { v })
			if fw == nil then
				return "refused:" .. why(nil, e)
			end
			keep(root, name, ".fixed", fw)
			return "ok"
		end))
		if eng ~= nil then
			say(root, name, "fixedloadeng", call(function()
				if fixed == nil then
					return "refused:the unit generated no fixed form for this root"
				end
				local r = L.newReport()
				local vals = F.load(d, fixed, eng :: buffer, r)
				if vals == nil or #vals ~= 1 then
					return "refused:" .. why(r, vals)
				end
				return verdictOf(vals[1], r)
			end))
		end
	end
	if class == "variable" then
		if wire ~= nil then
			say(root, name, "retain", call(function()
				local store = RT.newStore(CAPACITY, IDS)
				local r = L.newReport()
				local got = RT.LoadRetain(root, wire :: buffer, r, store)
				if got == nil or r.damaged or r.reason ~= nil then
					return "refused:" .. why(r)
				end
				local sr = L.newReport()
				local back, e = RT.SaveRetain(root, got, store, sr)
				if back == nil then
					return "refused:" .. why(sr, e)
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
			local r = L.newReport()
			local j = new()
			if not M[root .. "FromJson"](j, want, r) then
				return "refused:" .. why(r)
			end
			return same(j)
		end))
	end
end
