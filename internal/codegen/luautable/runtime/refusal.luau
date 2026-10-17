--!strict
-- refusal.luau — §6.5's strict read and the NAMED refusals it raises
-- (docs/SPEC-TABLES.md §6.5, §3, §2.14; docs/PORT-LUAU.md, THE CARD MAP).
-- It sits beside the tolerant load (load.luau, a later card) and wraps it:
-- a report carrying any of clamped, unknown, kind_mismatch, duplicate or
-- malformed is refused, naming the counter and the first field it saw; the
-- form byte's refusals (previous_form, an unknown form byte) and §2.14's
-- integrity_mismatch are returned before any read.
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local S = {}
local M = {}

-- The tolerant load's report (docs/SPEC-TABLES.md §4). The counters the
-- strict read turns into refusals.
export type Report = {
	unknown: number,
	kind_mismatch: number,
	widened: number,
	clamped: number,
	duplicate: number,
	malformed: boolean,
	refused: string?,
	field: string?,
}

local function newReport(): Report
	return {
		unknown = 0,
		kind_mismatch = 0,
		widened = 0,
		clamped = 0,
		duplicate = 0,
		malformed = false,
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
function M.ReadStrict(wire: buffer, root: { { any } }, load: (buffer, any, Report) -> (any, boolean)): (any, string?)
	if buffer.len(wire) < 1 then
		return nil, "malformed"
	end
	if buffer.readu8(wire, 0) ~= 1 then
		local form = buffer.readu8(wire, 0)
		return nil, formRefusal(form)
	end
	local report = newReport()
	local value, ok = load(wire, root, report)
	if not ok then
		return value, (report.refused :: string?) or "wire_damaged"
	end
	if report.clamped > 0 then
		return value, "clamped:" .. tostring(report.field)
	end
	if report.unknown > 0 then
		return value, "unknown:" .. tostring(report.field)
	end
	if report.kind_mismatch > 0 then
		return value, "kind_mismatch:" .. tostring(report.field)
	end
	if report.duplicate > 0 then
		return value, "duplicate:" .. tostring(report.field)
	end
	if report.malformed then
		return value, "malformed:" .. tostring(report.field)
	end
	return value, nil
end

return M
