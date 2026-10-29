# make/schema-luau-nightly-fuzz.mk — the LUAU port's NIGHTLY FUZZ (Glenn,
# 2026-10-18; docs/PORT-LUAU.md). Included by the Makefile's wildcard include,
# so the Makefile is not edited.
#
# A SEEDED, COVERAGE-GUIDED MUTATION FUZZER over the port's generated
# readers, run under the native host tools/luaufuzzhost (built below from the
# pinned Luau source): an input that reaches a line of the unit's generated
# Lua no input reached before (lua_getcoverage) joins the mutation pool.
# Readers: load (the
# variable form's tolerant load), strict (refusal.ReadStrict, the form
# registry and integrity) and text (Text.LoadText, the base64 carrier). The
# oracle is the one the card names: no crash, no hang, no assert, and a
# refusal is a CLEAN typed error (nil plus report.reason), never a panic and
# never a silent success on malformed input. Every finding records the seed
# and iteration that produced it, so the same input replays.
#
# THE CORPUS is the schema repository's own table corpus, the set the
# Makefile's generate targets read (the variables below are the Makefile's
# SCHEMAS_TABLES*), never a second corpus. Every table a unit exports is a
# root: a fixed record and a variable table both go through strict and text,
# and the variable ones also through the tolerant load.
#
# THE SEEDS are the Go fuzz corpus (internal/fuzz/testdata) and the
# conformance wire vectors (testdata/wire/tables), plus one valid save (and
# its truncations) per root, so the fuzzer starts from every table shape
# rather than random bytes.
#
# THE BUDGET is LUAU_FUZZ_TIME seconds per port, default ten minutes. CI runs
# the replay on a push (no real time: the clock is an argument) and the
# ten-minute target only on the workflow's schedule (never per push).
#
# THE TOOL is the port's own pinned Luau 0.741 (make/luau.mk, `make
# dist-luau`); the target probes it and refuses by name, never fakes a pass.
#
# SAVED INPUTS (testdata/luau-nightly-fuzz/<name>.bin + .meta) replay on every
# push under the plain luau CLI. A .meta line pending=<fix-card> marks a real
# reader bug carded on its own: its red prints REPLAY-PENDING by name and does
# not fail the replay until that card lands.

LUAU_FUZZ_TIME ?= 600
LUAU_FUZZ_SEED ?= 20261018
LUAU_NF      := build/luau-nightly-fuzz
LUAU_NF_GEN  := $(LUAU_NF)/generated
LUAU_NF_CORPUS := testdata/luau-nightly-fuzz

# THE CORPUS: these are exactly the directories in the Makefile's
# SCHEMAS_TABLES* and SCHEMAS_WIDE generate prerequisites, plus test/tables.
# A directory is ONE unit (its files share one lock and one closure), and the
# Makefile's tables_generate generates it as one, so each directory is
# generated whole; each test/tables schema is its own unit, as there.
LUAU_NF_SCHEMAS := $(sort $(wildcard test/tables/*.schema) $(SCHEMAS_WIDE) \
	$(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOCK) \
	$(SCHEMAS_TABLES_MESSAGES) $(SCHEMAS_TABLES_BLOBS) $(SCHEMAS_TABLES_SCALARS) \
	$(SCHEMAS_TABLES_MAPS) $(SCHEMAS_TABLES_LISTS) $(SCHEMAS_TABLES_ARMS) \
	$(SCHEMAS_TABLES_BACKEND) $(SCHEMAS_TABLES_VOCAB) $(SCHEMAS_TABLES_VOCAB9))
LUAU_NF_UNITS := $(patsubst %/,%,$(sort $(dir $(filter-out test/tables/%,$(LUAU_NF_SCHEMAS))))) \
	$(filter test/tables/%,$(LUAU_NF_SCHEMAS))

# ---- the harness, generated here so it lives and dies with build/ ----------
define LUAU_NF_FUZZ_LUAU
-- The LUAU nightly fuzz harness (make/schema-luau-nightly-fuzz.mk).
-- Args: <seconds> <seed>. Requires ./manifest.luau and ./seeds.luau.
local ARGS = { ... }
local BUDGET = tonumber(ARGS[1]) or 600
local SEED = tonumber(ARGS[2]) or 20261018

local manifest = require("./manifest")
local seeds = require("./seeds")

-- COVERAGE GUIDED only under the native host (tools/luaufuzzhost): it
-- compiles every chunk with coverageLevel 1 and answers lua_getcoverage's
-- new lines. Under the plain luau CLI this harness refuses rather than run
-- an unguided loop under the guided name.
local HOST = __luaufuzz
if type(HOST) ~= "table" or type(HOST.coverage) ~= "function" or type(HOST.tick) ~= "function" then
	error("not coverage guided: run fuzz.luau under the native host tools/luaufuzzhost (make luau-nightly-fuzz builds it)")
end

local S = nil
local Save = nil
local Load = nil
local Refusal = nil
local Text = nil

local state = (SEED % 2147483647) + 1
local function rng(n)
	state = (state * 48271) % 2147483647
	if state <= 0 then state = state + 2147483646 end
	return (state % n) + 1
end

local function newReport()
	return {
		malformed = false, refused = 0, unknown = 0, kindMismatch = 0,
		clamped = 0, widened = 0, duplicate = 0, reason = nil,
	}
end

local roots = {}
local runtimes = {}

-- Every generated unit emits its OWN runtime beside its table modules, and a
-- descriptor registers into that unit's schema_tables. A root is therefore
-- driven by ITS unit's load/strict/text modules, never another unit's.
local function runtimeFor(unit)
	if runtimes[unit] ~= nil then return runtimes[unit] end
	local base = "./generated/" .. unit .. "/"
	local ok, S, Sv, Ld, Rf, Tx = pcall(function()
		return require(base .. "schema_tables"), require(base .. "schema_save"),
			require(base .. "schema_load"), require(base .. "schema_refusal"), require(base .. "schema_text")
	end)
	if not ok then error("fuzz runtime missing for " .. unit .. ": " .. tostring(S)) end
	local rt = { S = S, save = Sv, load = Ld, strict = Rf, text = Tx }
	runtimes[unit] = rt
	return rt
end

local function requireUnit(e)
	local mods = {}
	for _, name in e.modules do
		local ok, m = pcall(function() return require("./generated/" .. e.unit .. "/" .. name) end)
		if not ok or type(m) ~= "table" then
			error("fuzz table module missing for " .. e.unit .. "/" .. name .. ": " .. tostring(m))
		end
		mods[name] = m
		for key, d in pairs(m) do
				if type(key) == "string" and string.sub(key, -10) == "Descriptor"
					and type(d) == "table" and d.table == true and (d.fixed == true or d.variable == true) then
					local r = { unit = e.unit, name = d.name, d = d, module = m, rt = runtimeFor(e.unit) }
					roots[#roots + 1] = r
				end
		end
	end
end

for _, e in manifest do
	requireUnit(e)
end


local function hex(s)
	local t = {}
	for i = 1, #s do t[i] = string.format("%02x", string.byte(s, i)) end
	return table.concat(t)
end

-- A refusal is a clean typed error: nil WITH a named reason. A value is a
-- success; damage after a success is a silent success and is a finding.
local function outcome(kind, v, rep)
	if kind == "load" then
		-- the tolerant load's contract is nil plus the counters (or a value);
		-- only a nil that moved NOTHING is a silent refusal.
		if v == nil and rep.malformed ~= true and type(rep.reason) ~= "string"
			and (rep.refused or 0) == 0 and (rep.unknown or 0) == 0
			and (rep.kindMismatch or 0) == 0 and (rep.clamped or 0) == 0
			and (rep.widened or 0) == 0 and (rep.duplicate or 0) == 0 then
			return false, "silent_nil"
		end
	else
		if v == nil and (type(rep.reason) ~= "string" or #rep.reason == 0) then
			return false, "refusal_without_reason"
		end
		if v ~= nil and rep.malformed == true then
			return false, "success_after_damage"
		end
	end
	return true
end

local function signature(kind, v, rep)
	return kind .. "|" .. tostring(rep.reason) .. "|" .. tostring(rep.malformed) ..
		"|" .. (v ~= nil and "v" or "n") .. "|" .. tostring(rep.refused) ..
		"|" .. tostring(rep.unknown) .. "|" .. tostring(rep.kindMismatch) ..
		"|" .. tostring(rep.clamped) .. "|" .. tostring(rep.widened) ..
		"|" .. tostring(rep.duplicate)
end

-- THE LOAD KIND IS THE TOLERANT WALK: L.loadTolerant where the runtime has
-- it (L.load became the strict read, docs/SPEC-TABLES.md §6.5), L.load on a
-- runtime from before that rename, so "load" never silently re-tests strict.
-- Every reader takes every input: the tolerant load's open() refuses a
-- malformed id table itself (luau-load-negative-id-count landed the sign
-- check), so the harness adds no framing guard of its own in front of it.

local function run(kind, r, data)
	local rep = newReport()
	local v
	local ok
	HOST.tick()
	if kind == "load" then
		ok, v = pcall(r.rt.load.loadTolerant or r.rt.load.load, r.name, buffer.fromstring(data), rep)
	elseif kind == "strict" then
		ok, v = pcall(r.rt.strict.ReadStrict, r.module, r.name, buffer.fromstring(data), rep)
	else
		ok, v = pcall(r.rt.text.LoadText, r.module, r.name, data, rep)
	end
	-- coverage is read after EVERY run, good or bad, so a line is credited
	-- to the input that first hit it and never to the next good one.
	local fresh = HOST.coverage("/generated/" .. r.unit .. "/")
	if not ok then
		return false, "error:" .. tostring(v), rep, nil, fresh
	end
	local good, why = outcome(kind, v, rep)
	if not good then return false, why, rep, v, fresh end
	return true, signature(kind, v, rep), rep, v, fresh
end

local function mutate(s)
	local n = #s
	local ops = rng(4)
	for _ = 1, ops do
		local pick = rng(8)
		if pick == 1 and n > 0 then
			local i = rng(n)
			local b = buffer.fromstring(string.sub(s, i, i))
			local v = buffer.readu8(b, 0)
			buffer.writeu8(b, 0, bit32.bxor(v, math.floor(2 ^ (rng(8) - 1))))
			s = string.sub(s, 1, i - 1) .. buffer.tostring(b) .. string.sub(s, i + 1)
		elseif pick == 2 and n > 0 then
			local i = rng(n)
			s = string.sub(s, 1, i - 1) .. string.char(rng(256) - 1) .. string.sub(s, i + 1)
		elseif pick == 3 and n < 65536 then
			local i = rng(n + 1)
			local k = rng(16)
			local chunk = string.rep(string.char(rng(256) - 1), k)
			s = string.sub(s, 1, i - 1) .. chunk .. string.sub(s, i)
		elseif pick == 4 and n > 1 then
			local i = rng(n)
			local k = rng(math.min(n - i + 1, 32))
			s = string.sub(s, 1, i - 1) .. string.sub(s, i + k)
		elseif pick == 5 and n > 1 then
			s = string.sub(s, 1, rng(n - 1))
		elseif pick == 6 and n > 0 then
			local i = rng(n)
			local j = rng(n)
			s = string.sub(s, 1, i - 1) .. string.sub(s, j, j) .. string.sub(s, i)
		elseif pick == 7 and n > 0 then
			local i = rng(n)
			local j = rng(n)
			if i > j then i, j = j, i end
			-- move the block s[i..j] to the end: same bytes, new order
			s = string.sub(s, 1, i - 1) .. string.sub(s, j + 1) .. string.sub(s, i, j)
		elseif n > 0 then
			local i = rng(n)
			local v = string.byte(s, i)
			s = string.sub(s, 1, i - 1) .. string.char(bit32.bxor(v, 0x80)) .. string.sub(s, i + 1)
		end
		n = #s
	end
	return s
end

local pool = {}
local seen = {}

local function add(kind, data, unit, root)
	if #data == 0 or #data > 65536 then return end
	pool[#pool + 1] = { kind = kind, data = data, unit = unit, root = root }
end

-- the roots' own valid saves, and their truncations: the shortest path to
-- every bounds check in every reader.
for _, r in roots do
	if r.rt ~= nil then
		local ok, w = pcall(function() return r.rt.save.save(r.d, r.d.new()) end)
		if ok and w ~= nil then
			local b = buffer.tostring(w)
			add("bin", b, r.unit, r.name)
			for _, k in { 1, 2, 3, 4, 5, 8, 12, 16, math.floor(#b / 2), #b - 1 } do
				if k > 0 and k < #b then add("bin", string.sub(b, 1, k), r.unit, r.name) end
			end
		end
		local tok, t = pcall(function() return r.rt.text.SaveText(r.d, r.d.new()) end)
		if tok and t ~= nil then
			add("txt", t, r.unit, r.name)
		end
	end
end

-- the Go fuzz corpus and the conformance wire vectors.
for _, s in seeds do
	add(s.kind, (s.kind == "txt") and buffer.tostring(s.data) or buffer.tostring(s.data), nil, nil)
end

local function rootsFor(kind)
	local out = {}
	for _, r in roots do
		if kind ~= "load" or r.d.variable == true then out[#out + 1] = r end
	end
	return out
end

local binaryRoots = rootsFor("strict")
local variableRoots = rootsFor("load")
local textRoots = rootsFor("txt")

local kinds = { "load", "strict", "txt" }
if #manifest == 0 or #roots == 0 or #binaryRoots == 0 or #variableRoots == 0 or #textRoots == 0 or #pool == 0 then
	error("fuzz corpus incomplete: units=" .. #manifest .. " roots=" .. #roots ..
		" load=" .. #variableRoots .. " strict=" .. #binaryRoots ..
		" text=" .. #textRoots .. " seeds=" .. #pool)
end
-- A finding's identity: its reason with the unit's directory folded, so the
-- same reader bug in every unit's runtime copy is ONE finding. The loop
-- keeps fuzzing past a known finding and stops at 20 distinct ones.
local function findingKey(kind, why)
	return kind .. "|" .. string.gsub(tostring(why), "generated/[^/:]+/", "generated/*/")
end

-- Minimise a failing input: delete chunks, halving the chunk size, while the
-- SAME reason reproduces. Bounded by tries AND by CPU time (MINIMISE_SECONDS
-- per finding, never past the run's budget), so a slow reader cannot eat it.
local MINIMISE_SECONDS = 30
local function minimise(kind, r, data, why, stopAt)
	local key = findingKey(kind, why)
	local tries = 0
	local deadline = math.min(os.clock() + MINIMISE_SECONDS, stopAt)
	local function more() return tries < 4000 and os.clock() < deadline end
	local chunk = math.max(1, math.floor(#data / 2))
	while chunk >= 1 and more() do
		local i = 1
		local shrunk = false
		while i <= #data and more() do
			local cand = string.sub(data, 1, i - 1) .. string.sub(data, i + chunk)
			tries = tries + 1
			local good, cwhy
			if #cand > 0 then
				good, cwhy = run(kind, r, cand)
			else
				good = true
			end
			if not good and findingKey(kind, cwhy) == key then
				data = cand
				shrunk = true
			else
				i = i + chunk
			end
		end
		if not shrunk then chunk = math.floor(chunk / 2) end
	end
	return data
end

-- the seed saves above ran reader and writer code: absorb those lines now,
-- so the first fuzzed input is not credited with them.
HOST.coverage()
local start = os.clock()
local iter, findings, total, guided = 0, 0, 0, 0
local known = {}
while os.clock() - start < BUDGET and findings < 20 do
	iter = iter + 1
	local kind = kinds[(iter % 3) + 1]
	local rset = (kind == "load") and variableRoots or (kind == "txt") and textRoots or binaryRoots
	if #rset > 0 and #pool > 0 then
		local pick = pool[rng(#pool)]
		local r = rset[rng(#rset)]
		if pick.root ~= nil then
			for _, candidate in rset do
				if candidate.unit == pick.unit and candidate.name == pick.root then r = candidate break end
			end
		end
		local data = mutate(pick.data)
		local good, why, rep, _, fresh = run(kind, r, data)
		if not good then
			total = total + 1
			local key = findingKey(kind, why)
			if known[key] == nil then
				known[key] = true
				findings = findings + 1
				-- the raw input is printed FIRST, so a job killed while it
				-- minimises still keeps a replayable input; FINDING-MIN then
				-- replaces it with the smaller one under the same name.
				local head = kind .. "|" .. r.unit .. "|" .. r.name .. "|" .. tostring(SEED) .. "|" .. tostring(iter) .. "|" .. tostring(why) .. "|"
				print("FINDING|" .. head .. hex(data))
				local small = minimise(kind, r, data, why, start + BUDGET)
				print("FINDING-MIN|" .. head .. hex(small))
			end
		else
			-- THE STEERING: an input that reached a line no input reached
			-- before (lua_getcoverage, over this root's unit) joins the pool.
			local keep = fresh > 0
			if keep then guided = guided + 1 end
			local skey = kind .. "|" .. r.unit .. "|" .. r.name .. "|" .. why
			if seen[skey] == nil then
				seen[skey] = true
				keep = true
			end
			if keep then
				if #pool < 4000 then
					add(kind, data, r.unit, r.name)
				else
					pool[rng(#pool)] = { kind = kind, data = data, unit = r.unit, root = r.name }
				end
			end
		end
	end
end
print("FUZZ-DONE|iterations=" .. tostring(iter) .. "|findings=" .. tostring(findings) .. "|hits=" .. tostring(total) .. "|roots=" .. tostring(#roots) .. "|seeds=" .. tostring(#pool) .. "|guided=coverage|lines=" .. tostring(HOST.lines()) .. "|newcover=" .. tostring(guided))
endef

define LUAU_NF_REPLAY_LUAU
-- Replay every saved regression input once. No real time: every input is
-- replayed, the clock is not an argument.
local manifest = require("./manifest")
local replay = require("./replaydata")

local runtimes = {}
local function runtimeFor(unit)
	if runtimes[unit] ~= nil then return runtimes[unit] end
	local base = "./generated/" .. unit .. "/"
	local ok, S, Ld, Rf, Tx = pcall(function()
		return require(base .. "schema_tables"), require(base .. "schema_load"),
			require(base .. "schema_refusal"), require(base .. "schema_text")
	end)
	if not ok then return nil end
	local rt = { S = S, load = Ld, strict = Rf, text = Tx }
	runtimes[unit] = rt
	return rt
end

local roots = {}
for _, e in manifest do
	local rt = runtimeFor(e.unit)
	for _, name in e.modules do
		local ok, m = pcall(function() return require("./generated/" .. e.unit .. "/" .. name) end)
		if ok and type(m) == "table" then
			for key, d in pairs(m) do
				if type(key) == "string" and string.sub(key, -10) == "Descriptor"
					and type(d) == "table" and d.table == true and (d.fixed == true or d.variable == true) then
					roots[e.unit .. "\0" .. d.name] = { unit = e.unit, name = d.name, d = d, module = m, rt = rt }
				end
			end
		end
	end
end

local function newReport()
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0, reason = nil }
end

local function hex(s)
	local t = {}
	for i = 1, #s do t[i] = string.format("%02x", string.byte(s, i)) end
	return table.concat(t)
end

-- the reason with the path up to and including the unit's directory folded:
-- the same bug reads "./generated/<unit>/" from one working directory and
-- "build/.../generated/<unit>/" from another.
local function fold(why)
	local w = string.gsub(tostring(why), "^error:[^:]-generated/[^/:]+/", "error:generated/*/")
	return w
end

local failed, ran, pending = 0, 0, 0
for _, e in replay do
	ran = ran + 1
	local r = roots[e.unit .. "\0" .. e.root]
	if r == nil then
		print("REPLAY-FAIL|" .. e.name .. "|unknown unit/root " .. e.unit .. "/" .. e.root)
		failed = failed + 1
	else
		local data = buffer.tostring(e.data)
		local rep = newReport()
		local ok, v
		if e.kind == "load" then
			ok, v = pcall(r.rt.load.loadTolerant or r.rt.load.load, r.name, buffer.fromstring(data), rep)
		elseif e.kind == "strict" then
			ok, v = pcall(r.rt.strict.ReadStrict, r.module, r.name, buffer.fromstring(data), rep)
		else
			ok, v = pcall(r.rt.text.LoadText, r.module, r.name, data, rep)
		end
		local why = nil
		if not ok then
			why = "error:" .. tostring(v)
		elseif e.kind == "load" then
			if v == nil and rep.malformed ~= true and type(rep.reason) ~= "string"
				and (rep.refused or 0) == 0 and (rep.unknown or 0) == 0
				and (rep.kindMismatch or 0) == 0 and (rep.clamped or 0) == 0
				and (rep.widened or 0) == 0 and (rep.duplicate or 0) == 0 then
				why = "silent_nil"
			end
		else
			if v == nil and (type(rep.reason) ~= "string" or #rep.reason == 0) then why = "refusal_without_reason" end
			if v ~= nil and rep.malformed == true then why = "success_after_damage" end
		end
		-- A PENDING input (its .meta names the fix card with pending=) is a
		-- real reader bug carded on its own: its red is reported by name and
		-- does not fail the replay until that card lands, but ONLY while it
		-- fails the SAME way as its .meta reason (the path to the unit folded):
		-- a different red is a new regression and fails.
		-- Once it replays green the line says so; remove the pending= line.
		if why ~= nil and e.pending ~= "" and fold(why) ~= fold(e.reason) then
			print("REPLAY-FAIL|" .. e.name .. "|pending " .. e.pending .. " but the reason changed: " .. why .. " (recorded: " .. tostring(e.reason) .. ")")
			failed = failed + 1
		elseif why ~= nil and e.pending ~= "" then
			print("REPLAY-PENDING|" .. e.name .. "|" .. e.pending .. "|" .. why)
			pending = pending + 1
		elseif why ~= nil then
			print("REPLAY-FAIL|" .. e.name .. "|" .. why .. "|" .. hex(data))
			failed = failed + 1
		elseif e.pending ~= "" then
			print("REPLAY-PENDING-GREEN|" .. e.name .. "|" .. e.pending .. "|the fix replays clean: remove its pending= line")
		end
	end
end
print("REPLAY-DONE|inputs=" .. tostring(ran) .. "|failed=" .. tostring(failed) .. "|pending=" .. tostring(pending))
if failed > 0 then error("REPLAY-RED: " .. tostring(failed) .. " saved input(s) failed") end
endef

define LUAU_NF_SEEDS
#!/bin/sh
set -eu
out=$$1
mkdir -p "$$(dirname "$$out")"
{
	echo "return {"
	find testdata/wire/tables -type f -name '*.bin' | sort | head -120 | while read -r f; do
		size=$$(wc -c < "$$f")
		[ "$$size" -le 8192 ] || continue
		esc=$$(od -An -v -tx1 "$$f" | tr -d ' \n' | sed 's/../\\x&/g')
		name=$$(basename "$$f")
		printf '\t{ name = "%s", kind = "bin", data = buffer.fromstring("%s") },\n' "$$name" "$$esc"
	done
	find internal/fuzz/testdata -type f | sort | head -64 | while read -r f; do
		size=$$(wc -c < "$$f")
		[ "$$size" -le 8192 ] || continue
		esc=$$(od -An -v -tx1 "$$f" | tr -d ' \n' | sed 's/../\\x&/g')
		name=$$(basename "$$f")
		printf '\t{ name = "%s", kind = "txt", data = buffer.fromstring("%s") },\n' "$$name" "$$esc"
	done
	echo "}"
} > "$$out"
endef

define LUAU_NF_REPLAYGEN
#!/bin/sh
set -eu
dir=$$1
out=$$2
mkdir -p "$$(dirname "$$out")"
{
	echo "return {"
	for f in "$$dir"/*.bin; do
		[ -e "$$f" ] || continue
		meta="$$f.meta"
		[ -f "$$meta" ] || continue
		kind=$$(sed -n 's/^kind=//p' "$$meta")
		unit=$$(sed -n 's/^unit=//p' "$$meta")
		root=$$(sed -n 's/^root=//p' "$$meta")
		pending=$$(sed -n 's/^pending=//p' "$$meta")
		reason=$$(printf '%s' "$$(sed -n 's/^reason=//p' "$$meta")" | od -An -v -tx1 | tr -d ' \n' | sed 's/../\\x&/g')
		name=$$(basename "$$f")
		esc=$$(od -An -v -tx1 "$$f" | tr -d ' \n' | sed 's/../\\x&/g')
		printf '\t{ name = "%s", kind = "%s", unit = "%s", root = "%s", pending = "%s", reason = "%s", data = buffer.fromstring("%s") },\n' \
			"$$name" "$$kind" "$$unit" "$$root" "$$pending" "$$reason" "$$esc"
	done
	echo "}"
} > "$$out"
endef

.PHONY: luau-nightly-fuzz luau-nightly-fuzz-replay luau-nightly-fuzz-negative-control luau-nightly-fuzz-toolchain luau-nf-dir

luau-nf-dir:
	@mkdir -p $(LUAU_NF)

luau-nightly-fuzz-toolchain:
	@if [ ! -x "$(LUAU)" ]; then \
		echo "tool missing: luau (LUAU=$(LUAU), the pinned 0.741 release)"; \
		echo "  install it with make dist-luau, or override LUAU=..."; \
		exit 1; fi

# THE NATIVE HOST (tools/luaufuzzhost/main.cpp): the one C++ driver, linked
# against the pinned Luau 0.741's own VM and compiler built from the official
# source tag, checked against its sha256 before it is unpacked. It compiles
# every chunk with coverageLevel 1 and answers lua_getcoverage's new lines to
# the harness, which is what makes the loop coverage guided.
LUAU_SRC_URL := https://github.com/luau-lang/luau/archive/refs/tags/$(LUAU_VERSION).tar.gz
LUAU_SRC_SHA := deee653d0f5971b809ef67f50dbe8109ad369d7575e344cfe2013774fed2f4c6
LUAU_SRC     := build/luau-src-$(LUAU_VERSION)
LUAU_NF_HOST := build/luaufuzzhost/luaufuzzhost
LUAU_NF_HOST_OBJ := build/luaufuzzhost/obj
LUAU_NF_HOST_INC := -I$(LUAU_SRC)/Common/include -I$(LUAU_SRC)/Ast/include -I$(LUAU_SRC)/Bytecode/include \
	-I$(LUAU_SRC)/Compiler/include -I$(LUAU_SRC)/VM/include -I$(LUAU_SRC)/VM/src

$(LUAU_SRC)/.stamp:
	@rm -rf $(LUAU_SRC) build/luau-src.tar.gz && mkdir -p $(LUAU_SRC)
	@curl -fsSL -o build/luau-src.tar.gz $(LUAU_SRC_URL)
	@got=$$(sha256sum build/luau-src.tar.gz | cut -d' ' -f1); \
	if [ "$$got" != "$(LUAU_SRC_SHA)" ]; then \
		echo "luaufuzzhost REFUSES: the Luau $(LUAU_VERSION) source sha256 $$got is not the pin $(LUAU_SRC_SHA)"; \
		rm -f build/luau-src.tar.gz; exit 1; fi
	@tar -xzf build/luau-src.tar.gz -C $(LUAU_SRC) --strip-components=1 && rm -f build/luau-src.tar.gz
	@touch $@

# THE STRICT FLAGS (zero warnings, Glenn 2026-10-20: vendored code included).
# Our own host compiles with them as errors. The vendored Luau source compiles
# with them too, warnings shown and counted rather than silenced: 1425 distinct
# warning sites at 0.741 (gcc 13, mostly -Wshadow, -Wpedantic and
# -Wsign-conversion) are the roadmap row schema-luau-fuzzhost-vendored-warnings,
# and until it lands they are not errors. For the same reason the vendored
# headers reach main.cpp as -isystem, so its own lines are held to -Werror.
LUAU_NF_STRICT := -Wall -Wextra -Wpedantic -Wshadow -Wconversion -Wsign-conversion -Wcast-qual -Wformat=2 -Wundef -Wdouble-promotion

$(LUAU_NF_HOST): tools/luaufuzzhost/main.cpp $(LUAU_SRC)/.stamp
	@rm -rf $(LUAU_NF_HOST_OBJ) && mkdir -p $(LUAU_NF_HOST_OBJ)
	@cd $(LUAU_SRC) && ls Common/src/*.cpp Ast/src/*.cpp Bytecode/src/*.cpp Compiler/src/*.cpp VM/src/*.cpp \
		| xargs -P 4 -I{} sh -c 'o=$(CURDIR)/$(LUAU_NF_HOST_OBJ)/$$(echo {} | tr / _).o; \
			nice -n 19 $(CXX) -std=c++17 -O2 -DNDEBUG $(LUAU_NF_STRICT) $(patsubst -I%,-I$(CURDIR)/%,$(LUAU_NF_HOST_INC)) -c {} -o "$$o" 2>"$$o.log"'
	@echo "luaufuzzhost: vendored Luau $(LUAU_VERSION): $$(cat $(LUAU_NF_HOST_OBJ)/*.log | grep -c 'warning:') warning lines (roadmap schema-luau-fuzzhost-vendored-warnings)"
	@nice -n 19 $(CXX) -std=c++17 -O2 -DNDEBUG $(LUAU_NF_STRICT) -Werror $(patsubst -I%,-isystem %,$(LUAU_NF_HOST_INC)) -c tools/luaufuzzhost/main.cpp -o $(LUAU_NF_HOST_OBJ)/main.o
	@$(CXX) -o $@ $(LUAU_NF_HOST_OBJ)/*.o -lm -lpthread
	@echo "luaufuzzhost: built against Luau $(LUAU_VERSION) ($(LUAU_SRC_SHA))"

$(LUAU_NF)/seeds.luau: make/schema-luau-nightly-fuzz.mk $(shell find testdata/wire/tables internal/fuzz/testdata -type f 2>/dev/null) | luau-nf-dir
	$(file >$(LUAU_NF)/gen-seeds.sh,$(LUAU_NF_SEEDS))
	@sh $(LUAU_NF)/gen-seeds.sh $@

$(LUAU_NF)/fuzz.luau: make/schema-luau-nightly-fuzz.mk | luau-nf-dir
	$(file >$@,$(LUAU_NF_FUZZ_LUAU))

$(LUAU_NF)/replay.luau: make/schema-luau-nightly-fuzz.mk | luau-nf-dir
	$(file >$@,$(LUAU_NF_REPLAY_LUAU))

$(LUAU_NF)/gen-replay.sh: make/schema-luau-nightly-fuzz.mk | luau-nf-dir
	$(file >$@,$(LUAU_NF_REPLAYGEN))

$(LUAU_NF_GEN)/.stamp: make/schema-luau-nightly-fuzz.mk build/schema-luau-nightly $(LUAU_NF_SCHEMAS)
	@rm -rf $(LUAU_NF_GEN) && mkdir -p $(LUAU_NF_GEN)
	@for p in $(LUAU_NF_UNITS); do \
		k=$$(echo "$$p" | sed 's#[/.]#_#g'); \
		./build/schema-luau-nightly generate --lang luau --out $(LUAU_NF_GEN)/$$k "$$p" >/dev/null \
			|| { echo "luau nightly fuzz: the target refused $$p"; exit 1; }; \
	done
	@touch $@

$(LUAU_NF)/manifest.luau: $(LUAU_NF_GEN)/.stamp
	@mkdir -p $(@D)
	@{ echo "return {"; \
	for d in $(LUAU_NF_GEN)/*; do \
		[ -d "$$d" ] || continue; k=$$(basename $$d); \
		mods=$$(find "$$d" -maxdepth 1 -name '*Table.luau' -printf '"%f", ' 2>/dev/null | sed 's/\.luau"/"/g'); \
		printf '\t{ unit = "%s", modules = { %s } },\n' "$$k" "$$mods"; \
	done; echo "}"; } > $@

build/schema-luau-nightly: $(GO_SOURCES) cmd/schema/*.go
	nice -n 19 go build -o $@ ./cmd/schema

luau-nightly-fuzz: luau-nightly-fuzz-toolchain $(LUAU_NF_HOST) build/schema-luau-nightly $(LUAU_NF_GEN)/.stamp $(LUAU_NF)/manifest.luau $(LUAU_NF)/seeds.luau $(LUAU_NF)/fuzz.luau
	@mkdir -p $(LUAU_NF)/findings
	@set +e; \
	$(LUAU_NF_HOST) $(LUAU_NF)/fuzz.luau $(LUAU_FUZZ_TIME) $(LUAU_FUZZ_SEED) > $(LUAU_NF)/fuzz.log 2>&1; \
	status=$$?; cat $(LUAU_NF)/fuzz.log; \
	if [ $$status -ne 0 ]; then echo "luau nightly fuzz: the harness died (exit $$status)"; exit $$status; fi; \
	if grep -q '^FINDING|' $(LUAU_NF)/fuzz.log; then \
		grep '^FINDING' $(LUAU_NF)/fuzz.log | while IFS='|' read -r tag kind unit root seed iter why hex; do \
			f=$(LUAU_NF)/findings/$$kind-$$unit-$$root-$$seed-$$iter.bin; \
			printf '%s' "$$hex" | xxd -r -p > "$$f"; \
			{ echo seed=$$seed; echo iteration=$$iter; echo kind=$$kind; echo unit=$$unit; \
			  echo root=$$root; echo "reason=$$why"; \
			  [ "$$tag" = FINDING-MIN ] && echo minimised=yes; true; } > "$$f.meta"; \
		done; \
		echo "luau nightly fuzz FOUND findings in $(LUAU_NF)/findings"; \
		exit 1; \
	fi; \
	if ! grep -q '^FUZZ-DONE|' $(LUAU_NF)/fuzz.log; then \
		echo "luau nightly fuzz: no FUZZ-DONE line (hang, assert or a dead harness)"; exit 1; fi; \
	echo "luau nightly fuzz: clean ($$(grep '^FUZZ-DONE|' $(LUAU_NF)/fuzz.log))"

luau-nightly-fuzz-replay: luau-nightly-fuzz-toolchain build/schema-luau-nightly $(LUAU_NF_GEN)/.stamp $(LUAU_NF)/manifest.luau $(LUAU_NF)/fuzz.luau $(LUAU_NF)/replay.luau $(LUAU_NF)/gen-replay.sh
	@sh $(LUAU_NF)/gen-replay.sh $(LUAU_NF_CORPUS) $(LUAU_NF)/replaydata.luau
	@$(LUAU) $(LUAU_NF)/replay.luau > $(LUAU_NF)/replay.log 2>&1; status=$$?; cat $(LUAU_NF)/replay.log; exit $$status

# THE NEGATIVE CONTROL (make/negative-controls.json, tools/negativecontrols):
# a scratch copy of the generated readers has the id-table count's sign check
# removed from schema_load.luau's open() (the bug luau-load-negative-id-count fixed:
# a high-bit count passes the upper bound and walks past the buffer), and the
# coverage-guided fuzzer must find it inside LUAU_NF_NC_TIME; the found input
# must then FAIL the replay on the same sabotaged copy. The unplanted copy
# must stay green. The red line is kept: the plant is never in the committed
# reader. (An r_u8 plant was tried first and is NOT used: every r_u8 caller
# is already behind a span check, so removing it changes no outcome and the
# fuzzer rightly found nothing in 20 s.)
LUAU_NF_NC := build/luau-nightly-fuzz-negative
LUAU_NF_NC_TIME ?= 20
LUAU_NF_NC_SEED ?= 1
luau-nightly-fuzz-negative-control: luau-nightly-fuzz-toolchain $(LUAU_NF_HOST) build/schema-luau-nightly $(LUAU_NF_GEN)/.stamp $(LUAU_NF)/manifest.luau $(LUAU_NF)/seeds.luau $(LUAU_NF)/fuzz.luau $(LUAU_NF)/replay.luau $(LUAU_NF)/gen-replay.sh
	@rm -rf $(LUAU_NF_NC) && mkdir -p $(LUAU_NF_NC) \
		&& cp -r $(LUAU_NF)/generated $(LUAU_NF_NC)/generated \
		&& cp $(LUAU_NF)/manifest.luau $(LUAU_NF)/seeds.luau $(LUAU_NF)/fuzz.luau $(LUAU_NF)/replay.luau $(LUAU_NF_NC)/ \
		&& cp $(LUAU_NF)/gen-replay.sh $(LUAU_NF_NC)/
	@planted=0; for f in $(LUAU_NF_NC)/generated/*/schema_load.luau; do \
		if grep -q 'if count < 0 or count > math.floor' "$$f"; then \
			perl -0pi -e 's/if count < 0 or count > math\.floor/if count > math.floor/; s/^(local function open\b)/-- PLANTED: the id-table count sign check is removed\n$$1/m' "$$f"; \
			planted=$$((planted + 1)); fi; done; \
	if [ $$planted -lt 1 ]; then echo "negative control: no id-table count sign check was planted" >&2; exit 1; fi; \
	echo "negative control: planted in $$planted generated schema_load.luau copies"
	@set +e; \
	$(LUAU_NF_HOST) $(LUAU_NF_NC)/fuzz.luau $(LUAU_NF_NC_TIME) $(LUAU_NF_NC_SEED) > $(LUAU_NF_NC)/fuzz.log 2>&1; \
	status=$$?; cat $(LUAU_NF_NC)/fuzz.log; \
	if [ $$status -ne 0 ] || ! grep -q '^FUZZ-DONE|' $(LUAU_NF_NC)/fuzz.log; then \
		echo "negative control failed: the planted run did not end cleanly (exit $$status, FUZZ-DONE missing?)" >&2; exit 1; fi; \
	if ! grep -q '^FINDING|' $(LUAU_NF_NC)/fuzz.log; then \
		echo "negative control failed: the fuzzer did not find the planted reader bug" >&2; exit 1; fi; \
	echo "negative control: the fuzzer found the planted reader bug"
	@rm -rf $(LUAU_NF_NC)/findings && mkdir -p $(LUAU_NF_NC)/findings; \
	line=$$(grep '^FINDING-MIN|' $(LUAU_NF_NC)/fuzz.log | head -1); \
	[ -n "$$line" ] || line=$$(grep '^FINDING|' $(LUAU_NF_NC)/fuzz.log | head -1); \
	echo "$$line" | while IFS='|' read -r _ kind unit root seed iter why hex; do \
		f=$(LUAU_NF_NC)/findings/nc-input.bin; \
		printf '%s' "$$hex" | xxd -r -p > "$$f"; \
		{ echo seed=$$seed; echo iteration=$$iter; echo kind=$$kind; echo unit=$$unit; \
		  echo root=$$root; echo "reason=$$why"; } > "$$f.meta"; \
	done; \
	[ -s $(LUAU_NF_NC)/findings/nc-input.bin ] || { echo "negative control failed: no input saved" >&2; exit 1; }
	@sh $(LUAU_NF_NC)/gen-replay.sh $(LUAU_NF_NC)/findings $(LUAU_NF_NC)/replaydata.luau
	@set +e; \
	$(LUAU) $(LUAU_NF_NC)/replay.luau > $(LUAU_NF_NC)/replay.log 2>&1; status=$$?; \
	cat $(LUAU_NF_NC)/replay.log; \
	if [ $$status -eq 0 ] || ! grep -q '^REPLAY-FAIL|nc-input.bin|' $(LUAU_NF_NC)/replay.log; then \
		echo "negative control failed: the saved input did not fail replay by name on the sabotaged reader" >&2; exit 1; fi; \
	echo "negative control: the saved input fails replay (REPLAY-FAIL|nc-input.bin) on the sabotaged reader"
	@rm -rf $(LUAU_NF_NC)/clean && mkdir -p $(LUAU_NF_NC)/clean \
		&& cp -r $(LUAU_NF)/generated $(LUAU_NF_NC)/clean/generated \
		&& cp $(LUAU_NF)/manifest.luau $(LUAU_NF)/replay.luau $(LUAU_NF_NC)/replaydata.luau $(LUAU_NF_NC)/clean/
	@set +e; \
	$(LUAU) $(LUAU_NF_NC)/clean/replay.luau > $(LUAU_NF_NC)/clean/replay.log 2>&1; status=$$?; \
	cat $(LUAU_NF_NC)/clean/replay.log; \
	if [ $$status -ne 0 ] || ! grep -q '^REPLAY-DONE|inputs=1|failed=0|' $(LUAU_NF_NC)/clean/replay.log; then \
		echo "negative control failed: the saved input does not replay green on the unplanted reader" >&2; exit 1; fi; \
	echo "negative control: the same input replays green on the unplanted reader"
	@set +e; \
	$(LUAU_NF_HOST) $(LUAU_NF)/fuzz.luau $(LUAU_NF_NC_TIME) $(LUAU_NF_NC_SEED) > $(LUAU_NF_NC)/clean/fuzz.log 2>&1; \
	status=$$?; grep -v '^FINDING' $(LUAU_NF_NC)/clean/fuzz.log | tail -3; \
	if [ $$status -ne 0 ] || ! grep -q '^FUZZ-DONE|' $(LUAU_NF_NC)/clean/fuzz.log; then \
		echo "negative control failed: the unplanted run did not end cleanly (exit $$status)" >&2; exit 1; fi; \
	if grep -q '^FINDING|' $(LUAU_NF_NC)/clean/fuzz.log; then \
		grep '^FINDING|' $(LUAU_NF_NC)/clean/fuzz.log | cut -c1-300; \
		echo "negative control failed: the unplanted reader went red at the same seed and time" >&2; exit 1; fi; \
	echo "negative control: the unplanted reader stays green at the same seed and time"
