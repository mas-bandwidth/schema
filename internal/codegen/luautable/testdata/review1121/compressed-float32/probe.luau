-- A compressed float32's range at float32, and its negative zero, in every
-- form (TestLuauCompressedFloatF32Range). inputs.cases is cases.tsv's rows;
-- each prints id=verdict.
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
local negzero = -1 / math.huge
assert(negzero == 0 and 1 / negzero < 0, "negative zero")
local function num(s: string): number
	if s == "-0" then return negzero end
	local x = tonumber(s)
	assert(x ~= nil, "not a number: " .. s)
	return x
end
local function sign(x: any): string
	if x == nil then return "nil" end
	if x == 0 then return if 1 / x < 0 then "-0" else "+0" end
	return string.format("%.9g", x)
end
local function hex(b: buffer?): string
	if b == nil then return "nil" end
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do parts[i + 1] = string.format("%02x", buffer.readu8(b, i)) end
	return table.concat(parts)
end
local V = St.lookup("V")

-- THE VARIABLE FORM
local function var(cf: number): any
	local v: any = V.new()
	v.Cf = cf
	return v
end
local function var_rt(label: string, x: number)
	local b = save.save(V, var(x))
	if b == nil then return out(label, "refused") end
	local r: any = {}
	local back = R.ReadStrict(nil, "V", b, r)
	if back == nil then return out(label, "load_refused:" .. tostring(r.reason)) end
	out(label, "read:" .. sign(back.Cf))
end
local function var_save_same(label: string, pair: string)
	local w = string.split(pair, " ")
	out(label, if hex(save.save(V, var(num(w[1])))) == hex(save.save(V, var(num(w[2])))) then "as_plus" else "differs")
end

-- THE JSON READ AND WRITE, AND THE TEXT FORM
local function json_row(label: string, text: string)
	local r: any = {}
	local v = J.FromJson("V", buffer.fromstring(text), r)
	if v == nil then return out(label, "refused:" .. tostring(r.reason)) end
	out(label, "read:" .. sign(v.Cf))
end
local function json_write_sign(label: string, x: number)
	local jt = J.ToJson("V", var(x), {})
	out(label, if jt == nil then "write_refused" elseif string.find(buffer.tostring(jt), "-0", 1, true) then "minus" else "plus")
end
local function json_roundtrip(label: string, x: number)
	local jt = J.ToJson("V", var(x), {})
	if jt == nil then return out(label, "write_refused") end
	json_row(label, buffer.tostring(jt))
end
local function json_write(label: string, x: number)
	out(label, if J.ToJson("V", var(x), {}) == nil then "write_refused" else "written")
end
local function text_row(label: string, text: string)
	local _, v, err = TF.LoadTextForm("V", text)
	if v == nil then return out(label, "refused:" .. tostring(err and err.name)) end
	out(label, "read:" .. sign(v.Cf))
end

-- THE FIXED FORM
local fixed = { hash = T.FFixedHash, recordBytes = T.FFixedRecordBytes, layout = T.FFixedLayout }
local function fixed_rt(label: string, x: number)
	local v: any = T.FDescriptor.new()
	v.Cf = x
	local ok, b = pcall(F.save, T.FDescriptor, fixed, { v })
	if not ok or b == nil then return out(label, "refused") end
	local ok2, rows = pcall(F.load, T.FDescriptor, fixed, b)
	if not ok2 or rows == nil then return out(label, "load_refused") end
	out(label, "read:" .. sign(rows[1].Cf))
end

-- THE MESSAGE FORM
local voc = M.NewVocabulary()
assert(M.AnnounceRead(voc, inputs.announce, {}), "announce")
local function msg_rt(label: string, x: number)
	local v: any = T.FDescriptor.new()
	v.Cf = x
	local r: any = {}
	local b = M.SaveMessages(voc, "F", { v }, r)
	if b == nil then return out(label, "refused:" .. tostring(r.reason)) end
	local r2: any = {}
	local n = M.LoadMessages(voc, "F", b, 256, r2)
	out(label, if n == 1 then "read" else "load_refused:" .. tostring(r2.reason))
end

for line in string.gmatch(buffer.tostring(inputs.cases), "[^\n]+") do
	local w = string.split(line, "\t")
	local id, form, input = w[1], w[2], w[3]
	if form == "var" then var_rt(id, num(input))
	elseif form == "var-save-same" then var_save_same(id, input)
	elseif form == "json" then json_row(id, input)
	elseif form == "json-write-sign" then json_write_sign(id, num(input))
	elseif form == "json-roundtrip" then json_roundtrip(id, num(input))
	elseif form == "json-write" then json_write(id, num(input))
	elseif form == "text" then text_row(id, input)
	elseif form == "fixed" then fixed_rt(id, num(input))
	elseif form == "message" then msg_rt(id, num(input))
	else error("unknown form " .. form) end
end
