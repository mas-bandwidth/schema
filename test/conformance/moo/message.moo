--!strict
-- THE MOO LEG'S MESSAGE SURFACE (test/conformance/README.md, the surfaces;
-- docs/SPEC-TABLES.md §3.3). main.moo hands this module its context and takes
-- back the surfaces it carries:
--
--   message  AnnounceRead the connection's announcement into one direction's
--            vocabulary, LoadMessages the message-form batch against it,
--            SaveMessages, the bytes
--
-- through the emitted table_message.moo and the descriptors the unit's
-- <Base>Table modules register. `message` is the one surface that reads two
-- files: the `connection` line names the announcement, the `message` line the
-- pair of wires, and the two are joined by key here, never by a vocabulary of
-- this driver's own. The verbs are plural: a batch of one is still a batch.
-- ctx is the one block.moo documents, with tables(key) added.

type Surface = { carried: (any, string) -> boolean, run: () -> () }

-- the batch capacity LoadMessages is handed (the runtime's own batch bound
-- refuses anything larger)
local BATCH = 256

return function(ctx: any): { [string]: Surface }
	local function carried(_: any, key: string): boolean
		return ctx.module(key, "table_message") ~= nil
	end

	-- one read for both surfaces: `message` on the `connection` and
	-- `message` lines, `message-prior` on `prior-connection` and
	-- `prior-message`. A prior row may owe a refusal, so its refused read is
	-- answered `<case> refused`, which split.pl writes as <case>.refused and the
	-- harness never compares (docs/rulings.json `refusal-verdict-only`)
	local function run(connectionKind: string, messageKind: string, prior: boolean)
		local connections: { [string]: { string } } = {}
		for _, c in ctx.kind(connectionKind) do
			connections[c[2]] = c
		end
		for _, f in ctx.kind(messageKind) do
			-- message <name> <connection> <root> <file-form wire> <message-form wire>
			local conn = connections[f[3]]
			if conn == nil then
				error("driver: " .. messageKind .. " " .. f[2] .. " names no " .. connectionKind .. " " .. f[3], 0)
			end
			local unit = conn[3]
			if f[6] == "-" then
				-- a named refusal row: the root is a variable table (§3.3)
				ctx.refuse(f[2], "variable table: the message form is fixed tables only (SPEC-TABLES §3.3)")
				continue
			end
			if not carried(nil, unit) then
				ctx.say(f[2], nil)
				continue
			end
			ctx.tables(unit)
			local St: any = ctx.module(unit, "schema_tables")
			local root = St.lookup(f[4])
			if not (root.table and root.fixed) then
				ctx.refuse(f[2], "variable table: the message form is fixed tables only (SPEC-TABLES §3.3)")
				continue
			end
			local MS: any = ctx.module(unit, "table_message")
			local report: any = {}
			local vocabulary = MS.NewVocabulary()
			if not MS.AnnounceRead(vocabulary, ctx.inputs(conn[5]), report) then
				error("driver: " .. f[2] .. ": the announcement was refused: " .. tostring(report.reason), 0)
			end
			-- THE ONE READ (SPEC-TABLES §3.3, §6.5): the batch is read whole or
			-- refused whole, and the verdict is the contract, never the reason
			local count, values = MS.LoadMessages(vocabulary, f[4], ctx.inputs(f[6]), BATCH, report)
			if count < 1 or report.damaged or report.refused or #values ~= count then
				if prior then
					print(f[2] .. " refused " .. tostring(report.reason))
				else
					ctx.refuse(f[2], "refused")
				end
				continue
			end
			local body: { any } = table.move(values, 1, count, 1, {})
			local size = MS.MeasureMessages(f[4], body, report)
			if size < 0 then
				error("driver: " .. f[2] .. ": message measure: " .. tostring(report.reason), 0)
			end
			local out = MS.SaveMessages(f[4], body, report)
			if out == nil then
				error("driver: " .. f[2] .. ": message save: " .. tostring(report.reason), 0)
			end
			ctx.say(f[2], buffer.tostring(out))
		end
	end

	local surfaces: { [string]: Surface } = {}
	surfaces["message"] = {
		carried = carried,
		run = function()
			run("connection", "message", false)
		end,
	}
	-- `message-prior` (testdata/conformance/tables/FORMAT.md): the same read of a
	-- PRIOR WRITER's batch, handed on its own keywords
	surfaces["message-prior"] = {
		carried = carried,
		run = function()
			run("prior-connection", "prior-message", true)
		end,
	}
	return surfaces
end
