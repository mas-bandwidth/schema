--!strict
-- table_refusal.moo — THE STRICT READ'S FORM DISPATCH (docs/SPEC-TABLES.md
-- §6.5, §3, §3.4, The three wires). ReadStrict answers a root's value or
-- refuses with ONE name. A form byte is answered before anything behind it,
-- and NEITHER CLASS BORROWS THE OTHER'S FORM: a declared FIXED root reads
-- through table_load.moo's ReadFixed, which reads form 3 through its fixed
-- reader (table_fixed.moo) and names form 1 previous_form, form 2
-- message_form_as_file, any other byte newer_form, and unknown_form when this
-- build emits no form-3 data for it; a VARIABLE root reads form 1 through
-- table_load.moo's strict read, and names form 3 or any other byte it does
-- not carry newer_form, form 2 message_form_as_file. A form byte the root
-- does not read moves no counter. A port of the Luau leg's refusal.luau.
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local S = require("./schema_tables")
local L = require("./table_load")

local R = {}

local FORM_MESSAGE = 2

-- ReadStrict(T, root, bytes, report): the root value, or nil with
-- report.reason the refusal's name and report.field the field it names. T is
-- the <Base>Table module that declares root. A fixed root's file holds one
-- record; any other count is wire_damaged, as a strict read of one value.
function R.ReadStrict(T: any, root: string, bytes: buffer?, report: any?): any
	local rep: any = report or {}
	local d = S.lookup(root)
	if bytes == nil or buffer.len(bytes) < 1 then
		L.resetReport(rep)
		rep.damaged = true
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
		return L.ReadFixed(d, if T ~= nil then T[root .. "Fixed"] else nil, b, rep)
	end
	return L.ReadStrict(root, b, rep)
end

-- the names this file answers, beside table_load.moo's and table_fixed.moo's
R.ReadStrictNames = table.freeze({
	message_form_as_file = true, newer_form = true, previous_form = true, unknown_form = true,
	wire_damaged = true,
})

return table.freeze(R)
