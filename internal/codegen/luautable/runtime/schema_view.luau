--!strict
-- UnitView returns the unit registry as constant data (docs/SPEC-TABLES.md §8.3).
local manifest: any = table.freeze({}) -- __UNIT_VIEW_MANIFEST__
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
