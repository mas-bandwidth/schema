--!strict
-- json.luau — the Luau table runtime's TEXT FORM (docs/SPEC-TABLES.md §16,
-- §16.1–§16.7; docs/PORT-LUAU.md, THE CARD MAP). This attempt's wall ended
-- before the walk was written: FromJson and ToJson are the surface the tests
-- below call, and they refuse by name until the walk lands.
-- SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

-- A float32's shortest round-trip text is C's %.*g at this precision
-- (docs/SPEC-TABLES.md §16.1; internal/tabletext/write.go's formatG). The
-- negative control patches this one anchor to six significant digits and
-- requires the json-write comparison to go red.
local FLOAT32_DIGITS = 17

local J = {}

-- FromJson fills one instance from one text (docs/SPEC-TABLES.md §16.1,
-- §16.2, §16.7). Not written by this attempt.
function J.FromJson(root: string, text: buffer, report: any): any
	error("schema: json.luau FromJson is not implemented yet (schema-luau-json-text-form)")
end

-- ToJson writes one instance as one canonical, pretty-printed text with one
-- trailing newline (docs/SPEC-TABLES.md §16.1, §16.3). Not written by this
-- attempt.
function J.ToJson(root: string, v: any): buffer
	error("schema: json.luau ToJson is not implemented yet (schema-luau-json-text-form)")
end

return table.freeze(J)
