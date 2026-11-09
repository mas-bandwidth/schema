--!strict
-- THE MOO LEG'S VARIABLE-WIRE SURFACES (test/conformance/README.md, the
-- surfaces; docs/SPEC-TABLES.md §3, §4). main.moo hands this module its context
-- and takes back the surfaces it carries:
--
--   wire    Load the instance's wire, Save, the bytes
--   report  Load the wire, the verdict `read` or `refused`, then the §16 text
--           of the value and its write-back wire when the read reads
--   framing the same answer over the framing vectors, without the text when
--           the row says `no-value` (docs/LAYERS/1-framing.md); a framing
--           vector is a form-1 file read as the variable form even at a
--           fixed root, and its value is written back in the root's own form
--
-- through the emitted table_load.moo and table_save.moo and the descriptors the
-- unit's <Base>Table modules register. ctx is the one block.moo documents, with
-- tables(key) added: it requires every <Base>Table module of a unit.

type Surface = { carried: (any, string) -> boolean, run: () -> () }

return function(ctx: any): { [string]: Surface }
	local function carried(_: any, key: string): boolean
		return ctx.module(key, "table_load") ~= nil and ctx.module(key, "table_save") ~= nil
	end

	-- the one read of one wire at its root (main.moo's ctx.read): the value,
	-- or nil when the read refused, and the report
	local function loadWire(f: { string }): (any?, any)
		ctx.tables(f[3])
		return ctx.read(f[3], f[4], ctx.inputs(f[5]))
	end

	local surfaces: { [string]: Surface } = {}
	surfaces["wire"] = {
		carried = carried,
		run = function()
			for _, f in ctx.kind("instance") do
				if not carried(nil, f[3]) then
					ctx.say(f[2], nil)
					continue
				end
				-- a read the wire's own row expects refused (`wire-refused`) is a
				-- named absence: the harness scores it a pass there, and on any
				-- other row an absence the matrix counts, never a pass
				local v, rep = loadWire(f)
				if v == nil then
					ctx.refuse(f[2], tostring(rep.reason))
					continue
				end
				local out, why = ctx.write(f[3], f[4], v)
				if out ~= nil then
					ctx.say(f[2], buffer.tostring(out))
				elseif why ~= nil and why.name == "blob_shared" then
					-- the writer refusal every writer owes (docs/SPEC-TABLES.md §2.5)
					ctx.refuse(f[2], why.name)
				else
					ctx.say(f[2], "refused")
				end
			end
		end,
	}
	-- REPORT and FRAMING answer one verdict (docs/SPEC-TABLES.md §4, strict
	-- only): `read` then the §16 text of the whole value and, beside it as
	-- `<case>.wire`, the canonical write-back wire; or `refused` alone, with no
	-- value. A unit with no text module answers the verdict alone beside its
	-- absence, so the verdict is held and the value is the missing feature
	-- (test/conformance/README.md). A framing line is
	-- `framing <case> <unit> <root> <wire> [no-value]`; a no-value row owes the
	-- verdict and the write-back wire, never the text.
	local function answer(f: { string }, withText: boolean, variable: boolean)
		local v: any
		if variable then
			ctx.tables(f[3])
			v = ctx.readVariable(f[3], f[4], ctx.inputs(f[5]))
		else
			v = loadWire(f)
		end
		if v == nil then
			ctx.say(f[2], "refused\n")
			return
		end
		local root = (ctx.module(f[3], "schema_tables") :: any).lookup(f[4])
		local out = ctx.write(f[3], f[4], v)
		if out == nil then
			error("driver: " .. f[2] .. ": Save refused a value the read accepted", 0)
		end
		ctx.say(f[2] .. ".wire", buffer.tostring(out))
		if not withText then
			ctx.say(f[2], "read\n")
			return
		end
		local J: any = ctx.module(f[3], "json")
		if J == nil then
			ctx.say(f[2], "read\n")
			ctx.say(f[2], nil)
			return
		end
		ctx.say(f[2], "read\n" .. J.toJson(root, v))
	end
	surfaces["report"] = {
		carried = carried,
		run = function()
			for _, f in ctx.kind("report") do
				if not carried(nil, f[3]) then
					ctx.say(f[2], nil)
					continue
				end
				answer(f, true, false)
			end
		end,
	}
	surfaces["framing"] = {
		carried = carried,
		run = function()
			for _, f in ctx.kind("framing") do
				if not carried(nil, f[3]) or ctx.module(f[3], "json") == nil then
					ctx.say(f[2], nil)
					continue
				end
				answer(f, f[6] ~= "no-value", true)
			end
		end,
	}
	return surfaces
end
