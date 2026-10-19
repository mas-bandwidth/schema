--!strict
-- UnitView returns this unit's constant registry (docs/SPEC-TABLES.md §8.3).
local manifest = require("./unit_view_manifest")
local M = {}

function M.UnitView()
	return table.freeze({
		package = manifest.package,
		protocol_id = manifest.protocol_id,
		rows = manifest.rows,
		listing = function()
			return table.concat(manifest.rows, "\n") .. "\n"
		end,
	})
end

return table.freeze(M)
