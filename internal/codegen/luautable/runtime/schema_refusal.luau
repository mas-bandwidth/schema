--!strict
-- schema_refusal.luau — THE STRICT READ AND ITS NAMED REFUSALS (docs/SPEC-TABLES.md
-- §6.5, §11, §2.14; docs/PORT-LUAU.md, THE CARD MAP). Emitted beside the
-- unit's <Base>Table.luau modules by internal/codegen/luautable, which embeds
-- runtime/*.luau by glob, and required as require("./schema_refusal").
--
-- ReadStrict answers the root or refuses with ONE name, the set §6.5 states
-- and Go's TableReport.Err() answers: wire_damaged (the file's own framing),
-- malformed, clamped and kind_mismatch, each with the first field the walk saw
-- move it; a form byte the root's own form does not read (message_form_as_file,
-- newer_form, previous_form, unknown_form); integrity_mismatch; and the fixed reader's own
-- names for a form-3 file. It does NOT refuse unknown, widened or duplicate
-- alone (§6.5: unknown is evolution, widened decoded exactly; a repeated map
-- key is last-wins, §2.8, §4): those stay counted on the report, with the
-- field each first moved on.
--
-- For form 1 it is a WRAPPER over schema_load.luau's strict variable read (which
-- judges the tolerant walk L.loadTolerant, §6.5); this file keeps the form
-- dispatch and the fixed reader's path. There is no require cycle: refusal
-- requires load, and load never requires refusal.
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

local S = require("./schema_tables")
local L = require("./schema_load")
local F = require("./schema_fixed")

local R = {}

local FORM_VARIABLE = 1
local FORM_MESSAGE = 2
local FORM_FIXED = 3

-- the fixed reader (docs/SPEC-TABLES.md §3.4): one record, through the
-- emitted layout, its own refusals by name
local function readFixed(T: any, root: string, bytes: buffer, report: any): any
	local d = T[root .. "Descriptor"]
	local fixed = {
		hash = T[root .. "FixedHash"], recordBytes = T[root .. "FixedRecordBytes"], layout = T[root .. "FixedLayout"],
		lineage = T[root .. "FixedLineage"], floor = T[root .. "FixedFloor"],
	}
	local fx: any = {}
	local out = F.read(d, fixed, bytes, fx)
	if out == nil then
		report.reason = fx.reason
		-- non_finite_float names its field (docs/SPEC-TABLES.md §3.3)
		if fx.field ~= nil then report.field = fx.field end
		-- truncated and reserved_not_zero keep report.malformed set beside
		-- the name, as Go's fixed load keeps Malformed (§5.3)
		if fx.malformed == true then report.malformed = true end
		return nil
	end
	if #out ~= 1 then
		report.malformed = true
		report.reason = "wire_damaged"
		return nil
	end
	return out[1]
end

-- ReadStrict(T, root, bytes, report): the root value or nil, with
-- report.reason the name of the first refusal (§6.5's vocabulary, the same
-- spellings in every port) and report.field the first field the walk saw move
-- that counter (nil for a form byte, a trailer, or damage no field holds). T
-- is the <Base>Table module that declares root; report is the caller's.
--
-- A FORM BYTE THE ROOT DOES NOT READ IS unknown_form (docs/SPEC-TABLES.md
-- §6.5, strict error identity): the strict read folds the report's form names
-- into the one strict name. A DECLARED FIXED root sends form 3 to its fixed
-- reader (unknown_form when this build emits no fixed layout for it) and
-- answers form 1 previous_form; any other byte is unknown_form. A VARIABLE
-- root reads form 1, and any other byte is unknown_form. The form byte is
-- answered before anything behind it, so a one-byte file names its form. A
-- form byte the root does not read moves no counter (§3, §6.5).
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
	if form == FORM_MESSAGE then L.resetReport(rep); rep.reason = "unknown_form"; return nil end
	if d.fixed then
		-- the fixed reader's form registry (docs/SPEC-TABLES.md §3, §3.4,
		-- The three wires): 1 is behind it, previous_form
		L.resetReport(rep)
		if form == FORM_VARIABLE then rep.reason = "previous_form"; return nil end
		if form ~= FORM_FIXED then rep.reason = "unknown_form"; return nil end
		if T == nil or T[root .. "FixedLayout"] == nil then rep.reason = "unknown_form"; return nil end
		return readFixed(T, root, b, rep)
	end
	-- the VARIABLE form's whole strict read lives beside the walk, in
	-- schema_load.luau, because a require cycle does not load (docs/SPEC-TABLES.md §6.5)
	return L.ReadStrict(root, b, rep)
end

-- the names this file answers, for a caller that wants the spelling rather
-- than the reason string (docs/SPEC-TABLES.md §11, §6.5)
R.ReadStrictNames = table.freeze({
	previous_form = true, unknown_form = true,
	integrity_mismatch = true, wire_damaged = true,
	missing_required = true, clamped = true, kind_mismatch = true, non_finite_float = true, blob_shared = true,
	-- the storage cap refusal (§2.9): a `[]` or map count past the int32
	-- storage cap, answered by schema_load.luau
	count_over_extent_cap = true,
	-- the fixed reader's own, answered through schema_fixed.luau for a form-3 file
	-- (§3.4, §6.5): Go's names, every framing-damage return included, and the
	-- form names its own registry spells
	message_form_as_file = true, newer_form = true,
	layout_malformed = true, layout_record_too_large = true, layout_unknown = true, layout_unsupported = true,
	layout_kind_invalid = true,
	plan_too_large = true, count_over_bound = true,
	truncated = true, reserved_not_zero = true, no_layout = true,
	length_over_bound = true, text_ill_formed = true,
})

return table.freeze(R)
