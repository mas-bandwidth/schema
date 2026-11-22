-- A counted array below the reader's minimum in every form
-- (TestLuauCountBelowMinRefused). The modules are generated from
-- reader/Probe.schema; inputs.announce is the writer's announcement and
-- inputs.cases is cases.tsv's rows. Each row prints id=verdict.
local inputs = require("./inputs")
local T = require("./ProbeTable")
local St = require("./schema_tables")
local save = require("./schema_save")
local R = require("./schema_refusal")
local M = require("./schema_message")
local F = require("./schema_fixed")
local J = require("./schema_json")
local TF = require("./schema_textform")

local function out(label: string, v: string) print(label .. "=" .. v) end
local function count(v: any): string
	if v == nil then return "nil" end
	if typeof(v.ItemsCount) == "number" then return tostring(v.ItemsCount) end
	local xs = v.Items
	if typeof(xs) == "buffer" then return tostring(buffer.len(xs) // 4) end
	if typeof(xs) == "table" then return tostring(xs.n or #xs) end
	return typeof(xs)
end
local function unhex(s: string): buffer
	local b = buffer.create(#s // 2)
	for i = 0, #s // 2 - 1 do buffer.writeu8(b, i, tonumber(string.sub(s, 2 * i + 1, 2 * i + 2), 16) :: number) end
	return b
end
local function items(s: string): { number }
	local xs = {}
	for _, w in string.split(s, " ") do table.insert(xs, tonumber(w) :: number) end
	return xs
end
local P = St.lookup("P")
local fixed = { hash = T.MFixedHash, recordBytes = T.MFixedRecordBytes, layout = T.MFixedLayout }

local function var_load(label: string, wire: buffer)
	local r: any = {}
	local ok, v = pcall(R.ReadStrict, nil, "P", wire, r)
	out(label, if ok and v ~= nil then "read:" .. count(v) else "refused")
end
local function json_load(label: string, text: string)
	local r: any = {}
	local ok, v = pcall(J.FromJson, "P", buffer.fromstring(text), r)
	out(label, if ok and v ~= nil then "read:" .. count(v) else "refused")
end
local function text_load(label: string, text: string)
	local ok, _, v = pcall(TF.LoadTextForm, "P", text)
	out(label, if ok and v ~= nil then "read:" .. count(v) else "refused")
end
local function var_save(label: string, xs: { number })
	local v: any = P.new()
	v.Items = xs
	v.ItemsCount = #xs
	local ok, b = pcall(save.save, P, v)
	out(label, if ok and b ~= nil then "written" else "refused")
end
local function fixed_image(xs: { number }): (boolean, buffer?)
	local v: any = T.MDescriptor.new()
	v.Items = xs
	v.ItemsCount = #xs
	local ok, b = pcall(F.save, T.MDescriptor, fixed, { v })
	return ok and b ~= nil, if ok then b else nil
end
local function fixed_save(label: string, xs: { number })
	local ok = fixed_image(xs)
	out(label, if ok then "written" else "refused")
end
-- THE FIXED READ: the lawful image of the items 7 8 with its count word set
-- to the case's count and every slot past that count zeroed.
local function fixed_read(label: string, n: number)
	local ok, image = fixed_image({ 7, 8 })
	assert(ok and image ~= nil, "the lawful fixed image")
	local s = buffer.tostring(image)
	local two = string.pack("<i4i4i4", 2, 7, 8)
	local at = string.find(s, two, 1, true)
	assert(at ~= nil, "count word not found")
	local forged = string.sub(s, 1, at - 1) .. string.pack("<i4i4i4", n, 7, if n >= 2 then 8 else 0) .. string.sub(s, at + #two)
	local okl, rows = pcall(F.load, T.MDescriptor, fixed, buffer.fromstring(forged))
	out(label, if okl and rows ~= nil then "read:" .. count(rows[1]) else "refused")
end
local voc = M.NewVocabulary()
assert(M.AnnounceRead(voc, inputs.announce, {}), "announce")
local function msg_load(label: string, batch: buffer)
	local r: any = {}
	local ok, n = pcall(M.LoadMessages, voc, "M", batch, 256, r)
	out(label, if ok and n == 1 then "read" else "refused")
end

for line in string.gmatch(buffer.tostring(inputs.cases), "[^\n]+") do
	local w = string.split(line, "\t")
	local id, form, input, wire = w[1], w[2], w[3], w[4]
	if form == "born-var" then out(id, count(P.new()))
	elseif form == "born-fixed" then out(id, count(T.MDescriptor.new()))
	elseif form == "var-read" then var_load(id, unhex(wire))
	elseif form == "json-read" then json_load(id, input)
	elseif form == "text-read" then text_load(id, input)
	elseif form == "var-write" then var_save(id, items(input))
	elseif form == "fixed-write" then fixed_save(id, items(input))
	elseif form == "fixed-read" then fixed_read(id, tonumber(input) :: number)
	elseif form == "message-read" then msg_load(id, unhex(wire))
	else error("unknown form " .. form) end
end
