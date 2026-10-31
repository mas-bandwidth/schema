--!strict
-- THE MOO LEG'S BLOCK SURFACES (test/conformance/README.md, the surfaces;
-- docs/SPEC-TABLES.md §19). main.moo hands this module its context and takes
-- back the surfaces it carries:
--
--   block          open each image, `open\n` or `refuse\n`
--   block-foreign  byte-swap the image's magic word, open, `refuse\n`
--   block-dump     open the image, the canonical row dump
--   forgery        open the forged file at the claimed extent and base
--   block-reason   the same, the refusal's name or `ok`
--
-- every one through the emitted table_blockread.moo (BlockOpen, Read, Triple)
-- and the <Name>BlockInfo descriptors of the generated <Base>Table.moo,
-- never through a generated row type. A block is memory another build wrote:
-- the buffer the harness staged is placed as the Go driver places it, exactly
-- the claimed extent long, its base `lead` bytes into it (or no buffer at
-- all), what fits copied and the rest zero.
--
-- main.moo's Surface.carried is handed the unit's module and its key.
-- ctx is { kind: (string) -> {{string}}, module: (unit, name) -> any?,
-- inputs: (path) -> buffer, say: (case, text?) -> () } (a nil text is the
-- case's named absence).

type Surface = { carried: (any, string) -> boolean, run: () -> () }

-- the table each block fixture is an image of, by the fixture's name
-- (test/conformance/go/blocks.go answers it the same way), and the generated
-- module that declares it
local function tableOf(name: string): (string?, string?)
	if string.sub(name, 1, 12) == "block_render" then
		return "RenderFrame", "RenderTable"
	elseif string.sub(name, 1, 12) == "block_padded" then
		return "PaddedFrame", "PaddedTable"
	end
	return nil, nil
end

-- the descriptor graph as data: one text line per leaf, the way the Go
-- driver's block-dump spells it (test/conformance/go/blockdump.go); a float
-- is its bits
local function text(v: any, used: number): string
	local parts = { "\"" }
	for i = 0, used - 1 do
		local c = if type(v) == "string" then string.byte(v, i + 1) :: number else buffer.readu8(v, i)
		if c >= 0x20 and c < 0x7f and c ~= 34 and c ~= 92 then
			parts[#parts + 1] = string.char(c)
		else
			parts[#parts + 1] = string.format("\\x%02x", c)
		end
	end
	parts[#parts + 1] = "\" len=" .. used
	return table.concat(parts)
end

local function scalar(v: any, kind: number): string
	if kind == 1 then
		return if v then "true" else "false"
	elseif kind == 10 then
		return string.format("0x%08x", v)
	elseif kind == 11 then
		return string.format("0x%08x%08x", v[2], v[1])
	end
	return tostring(v)
end

local function dumpRecord(lines: { string }, info: any, rec: any, path: string)
	for _, f in info.fields do
		if f.outOfLine then
			continue
		end
		local name = if path == "" then f.name else path .. "." .. f.name
		local v = rec[f.name]
		if f.counted then
			local used = if type(v) == "string" then #v else buffer.len(v)
			lines[#lines + 1] = "  " .. name .. " = " .. text(v, used)
		elseif f.isArray then
			for s = 0, f.arrayBound - 1 do
				local at = name .. "[" .. s .. "]"
				if f.element ~= nil then
					dumpRecord(lines, f.element, v[s + 1], at)
				else
					lines[#lines + 1] = "  " .. at .. " = " .. scalar(v[s + 1], f.kind)
				end
			end
		elseif f.element ~= nil then
			dumpRecord(lines, f.element, v, name)
		else
			lines[#lines + 1] = "  " .. name .. " = " .. scalar(v, f.kind)
		end
		if f.optional then
			lines[#lines + 1] = "  " .. name .. "#present = " .. tostring(rec[f.name .. "#present"])
		end
	end
end

local function dumpBlock(B: any, block: any): string
	local info = block.info
	local rec = B.Read(block)
	if rec == nil then
		return "refuse\n"
	end
	local lines: { string } = {}
	lines[#lines + 1] = "projection " .. info.name .. " @0"
	dumpRecord(lines, info, rec, "")
	for _, f in info.fields do
		if f.outOfLine then
			local offsetOf, count, stride = B.Triple(block, f)
			lines[#lines + 1] = string.format("array %s %s @%d count=%d stride=%d", f.name, f.element.name, offsetOf, count, stride)
			for r = 0, count - 1 do
				lines[#lines + 1] = string.format("row %d @%d", r, offsetOf + r * stride)
				dumpRecord(lines, f.element, rec[f.name][r + 1], "")
			end
		end
	end
	return table.concat(lines, "\n") .. "\n"
end

return function(ctx: any): { [string]: Surface }
	local function blocks(): { { string } }
		return ctx.kind("block")
	end

	-- block name -> its line
	local function blockNamed(name: string): { string }?
		for _, b in blocks() do
			if b[2] == name then
				return b
			end
		end
		return nil
	end

	-- BlockOpen over the placed image: `lead` is the base's offset into a
	-- buffer exactly lead + extent long (nil lead: no buffer at all)
	local function open(b: { string }, data: buffer, extent: number, lead: number?): (any, string)
		local B = ctx.module(b[3], "table_blockread")
		local table_, mod = tableOf(b[2])
		if B == nil or table_ == nil or mod == nil then
			return nil, "absent"
		end
		local unit = ctx.module(b[3], mod)
		if unit == nil then
			return nil, "absent"
		end
		local info = unit[table_ .. "BlockInfo"]
		if info == nil then
			return nil, "absent"
		end
		if lead == nil then
			local block, why = B.BlockOpen(info, nil, 0, extent)
			return block, why or "ok"
		end
		local placed = buffer.create(lead + extent)
		buffer.copy(placed, lead, data, 0, math.min(extent, buffer.len(data)))
		local block, why = B.BlockOpen(info, placed, lead, extent)
		return block, why or "ok"
	end

	local function swapped(data: buffer): buffer
		local out = buffer.create(buffer.len(data))
		buffer.copy(out, 0, data)
		for i = 0, 3 do
			local a, b = buffer.readu8(data, i), buffer.readu8(data, 7 - i)
			buffer.writeu8(out, i, b)
			buffer.writeu8(out, 7 - i, a)
		end
		return out
	end

	local function verdict(b: { string }, data: buffer, extent: number, lead: number?): string
		local block = open(b, data, extent, lead)
		return if block ~= nil then "open\n" else "refuse\n"
	end

	-- every block forgery: the subject names the block (and so the unit and the
	-- table) it patches; the file is already patched, the extent and the base
	-- are what the caller holds
	local function eachForgery(answer: (string, { string }, buffer, number, number?) -> string, only: { [string]: boolean }?)
		for _, f in ctx.kind("forgery") do
			if f[3] ~= "block" or (only ~= nil and not only[f[2]]) then
				continue
			end
			local b = blockNamed(f[4])
			if b == nil then
				ctx.say(f[2], nil)
				continue
			end
			local data: buffer = ctx.inputs(f[5])
			local extent = tonumber(f[6]) :: number
			if extent < 0 then
				extent = buffer.len(data)
			end
			local lead: number? = if f[7] == "null" then nil else tonumber(f[7])
			ctx.say(f[2], answer(f[2], b, data, extent, lead))
		end
	end

	local surfaces: { [string]: Surface } = {}
	-- a unit carries the block surfaces when its tree carries the block runtime
	-- (the emitter writes it only for a unit with a block-form table, §2.12)
	local function carried(_: any, key: string): boolean
		return ctx.module(key, "table_blockread") ~= nil
	end

	surfaces["block"] = {
		carried = carried,
		run = function()
			for _, b in blocks() do
				local data: buffer = ctx.inputs(b[4])
				ctx.say(b[2], verdict(b, data, buffer.len(data), 0))
			end
		end,
	}
	surfaces["block-foreign"] = {
		carried = carried,
		run = function()
			for _, b in blocks() do
				local data: buffer = ctx.inputs(b[4])
				ctx.say(b[2], verdict(b, swapped(data), buffer.len(data), 0))
			end
		end,
	}
	surfaces["block-dump"] = {
		carried = carried,
		run = function()
			for _, b in blocks() do
				local data: buffer = ctx.inputs(b[4])
				local block = open(b, data, buffer.len(data), 0)
				local B = ctx.module(b[3], "table_blockread")
				ctx.say(b[2], if block ~= nil and B ~= nil then dumpBlock(B, block) else "refuse\n")
			end
		end,
	}
	surfaces["forgery"] = {
		carried = carried,
		run = function()
			eachForgery(function(_, b, data, extent, lead)
				return verdict(b, data, extent, lead)
			end)
		end,
	}
	surfaces["block-reason"] = {
		carried = carried,
		run = function()
			local reasons: { [string]: boolean } = {}
			for _, r in ctx.kind("refusal") do
				if r[3] == "block" then
					reasons[r[2]] = true
				end
			end
			eachForgery(function(name, b, data, extent, lead)
				local _, why = open(b, data, extent, lead)
				return why .. "\n"
			end, reasons)
		end,
	}
	return surfaces
end
