--!strict
-- refusal.luau — §6.5's strict read and the NAMED refusals it raises
-- (docs/SPEC-TABLES.md §6.5, §3, §2.14; docs/PORT-LUAU.md, THE CARD MAP).
-- It sits beside the tolerant load (load.luau, a later card) and wraps it:
-- a report carrying any of clamped, unknown, kind_mismatch, duplicate or
-- malformed is refused, naming the counter and the first field it saw; the
-- form byte's refusals (previous_form, an unknown form byte) and §2.14's
-- integrity_mismatch are returned before any read.
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local S = require("./schema_tables")
local L = require("./load")

local M = {}

-- The tolerant load's report (docs/SPEC-TABLES.md §4). The counters the
-- strict read turns into refusals.
export type Report = {
	unknown: number,
	kind_mismatch: number,
	kindMismatch: number,
	widened: number,
	clamped: number,
	duplicate: number,
	malformed: boolean,
	refused: number?,
	field: string?,
}

local function newReport(): Report
	return {
		unknown = 0,
		kind_mismatch = 0,
		kindMismatch = 0,
		widened = 0,
		clamped = 0,
		duplicate = 0,
		malformed = false,
		refused = 0,
	}
end

-- The form byte's refusals (docs/SPEC-TABLES.md §3): form 3 is the FIXED
-- form, which a variable root does not read — previous_form; form 2 is the
-- MESSAGE form, never a file's own form — message_form_as_file; anything else
-- is named by its byte.
local function formRefusal(form: number): string
	if form == 3 then
		return "previous_form"
	end
	if form == 2 then
		return "message_form_as_file"
	end
	return "unknown_form_byte"
end

-- ReadStrict is the strict read (docs/SPEC-TABLES.md §6.5): one call, one
-- answer, and a named refusal instead of a slow damaged value. It runs the
-- tolerant load over the form byte, then converts the report's bad counters
-- into the name the caller sees.
-- The first field the strict read can name is the root descriptor's first
-- field, since the landed tolerant load (load.luau) keeps only counters and
-- not the field that raised each one.
local function fieldName(root: string, report: Report): string
	if report.field ~= nil then
		return tostring(report.field)
	end
	local ok, desc = pcall(S.lookup, root)
	if ok and desc ~= nil and desc.fields ~= nil and desc.fields[1] ~= nil then
		return tostring(desc.fields[1].name)
	end
	return "field"
end

function M.ReadStrict(wire: buffer, root: string, load: ((buffer, string, any) -> (any, boolean))?): (any, string?)
	if buffer.len(wire) < 1 then
		return nil, "malformed"
	end
	local form = buffer.readu8(wire, 0)
	if form ~= 1 then
		return nil, formRefusal(form)
	end
	local report = newReport()
	local value
	if load ~= nil then
		local ok
		value, ok = load(wire, root, report)
		if not ok then
			return value, (report.refused :: any) or "wire_damaged"
		end
	else
		value = L.load(root, wire, report)
	end
	if report.clamped > 0 then
		return value, "clamped:" .. fieldName(root, report)
	end
	if report.unknown > 0 then
		return value, "unknown:" .. fieldName(root, report)
	end
	if report.kind_mismatch > 0 or report.kindMismatch > 0 then
		return value, "kind_mismatch:" .. fieldName(root, report)
	end
	if report.duplicate > 0 then
		return value, "duplicate:" .. fieldName(root, report)
	end
	if report.malformed then
		return value, "malformed:" .. fieldName(root, report)
	end
	return value, nil
end

return M
