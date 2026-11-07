--!strict
-- table_refusal.moo — THE STRICT READ'S FORM DISPATCH (docs/SPEC-TABLES.md
-- §6.5, §3, §3.4, The three wires). ReadStrict answers a root's value or
-- refuses with ONE name. A form byte is answered before anything behind it,
-- and NEITHER CLASS BORROWS THE OTHER'S FORM: a declared FIXED root reads form
-- 3 through its fixed reader (table_fixed.moo) and names form 1
-- previous_form, form 2 message_form_as_file, any other byte newer_form, and
-- unknown_form when this build emits no form-3 data for it; a VARIABLE root
-- reads form 1 through table_load.moo's strict read, and names form 3 or any
-- other byte it does not carry newer_form, form 2 message_form_as_file. A form
-- byte the root does not read moves no counter. A port of the Luau leg's
-- refusal.luau.
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local S = require("./schema_tables")
local L = require("./table_load")
local F = require("./table_fixed")

local R = {}

local FORM_VARIABLE = 1
local FORM_MESSAGE = 2
local FORM_FIXED = 3

-- ReadStrict(T, root, bytes, report): the root value, or nil with
-- report.reason the refusal's name and report.field the field it names. T is
-- the <Base>Table module that declares root. A fixed root's file holds one
-- record; any other count is wire_damaged, as a strict read of one value.
function R.ReadStrict(T: any, root: string, bytes: buffer?, report: any?): any
	local rep: any = report or {}
	local d = S.lookup(root)
	if bytes == nil or buffer.len(bytes) < 1 then
		L.resetReport(rep)
		rep.malformed = true
		rep.reason = "wire_damaged"
		return nil
	end
	local b = bytes :: buffer
	local form = buffer.readu8(b, 0)
	if form == FORM_MESSAGE then
		L.resetReport(rep)
		rep.reason = "message_form_as_file"
		return nil
	end
	if d.fixed then
		L.resetReport(rep)
		if form == FORM_VARIABLE then
			rep.reason = "previous_form"
			return nil
		end
		if form ~= FORM_FIXED then
			rep.reason = "newer_form"
			return nil
		end
		local fixed = if T ~= nil then T[root .. "Fixed"] else nil
		if fixed == nil then
			rep.reason = "unknown_form"
			return nil
		end
		local values = F.load(d, fixed, b, rep)
		if values == nil then
			return nil
		end
		if #values ~= 1 then
			L.resetReport(rep)
			rep.malformed = true
			rep.reason = "wire_damaged"
			return nil
		end
		-- the fixed load's counters refuse as the variable read's do (§6.5)
		if rep.clamped > 0 then
			rep.reason = "clamped"
			return nil
		end
		if rep.kindMismatch > 0 then
			rep.reason = "kind_mismatch"
			return nil
		end
		return values[1]
	end
	return L.ReadStrict(root, b, rep)
end

-- the names this file answers, beside table_load.moo's and table_fixed.moo's
R.ReadStrictNames = table.freeze({
	message_form_as_file = true, newer_form = true, previous_form = true, unknown_form = true,
	wire_damaged = true, clamped = true, kind_mismatch = true,
})

return table.freeze(R)
