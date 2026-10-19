# make/schema-luau-nightly-fuzz.mk — the LUAU port's NIGHTLY FUZZ (Glenn,
# 2026-10-18; docs/PORT-LUAU.md). Included by the Makefile's wildcard include,
# so the Makefile is not edited.
#
# A SEEDED MUTATION FUZZER over the port's generated readers: load (the
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

LUAU_FUZZ_TIME ?= 600
LUAU_FUZZ_SEED ?= 20261018
LUAU_NF      := build/luau-nightly-fuzz
LUAU_NF_GEN  := $(LUAU_NF)/generated
LUAU_NF_CORPUS := testdata/luau-nightly-fuzz

# THE CORPUS: these are exactly the directories in the Makefile's
# SCHEMAS_TABLES* and SCHEMAS_WIDE generate prerequisites, plus test/tables.
# Sort removes the same schema reached by more than one prerequisite.
LUAU_NF_SCHEMAS := $(sort $(wildcard test/tables/*.schema) $(SCHEMAS_WIDE) \
	$(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOCK) \
	$(SCHEMAS_TABLES_MESSAGES) $(SCHEMAS_TABLES_BLOBS) $(SCHEMAS_TABLES_SCALARS) \
	$(SCHEMAS_TABLES_MAPS) $(SCHEMAS_TABLES_LISTS) $(SCHEMAS_TABLES_ARMS) \
	$(SCHEMAS_TABLES_BACKEND) $(SCHEMAS_TABLES_VOCAB) $(SCHEMAS_TABLES_VOCAB9))

# ---- the harness, generated here so it lives and dies with build/ ----------
define LUAU_NF_FUZZ_LUAU
-- The LUAU nightly fuzz harness (make/schema-luau-nightly-fuzz.mk).
-- Args: <seconds> <seed>. Requires ./manifest.luau and ./seeds.luau.
local ARGS = { ... }
local BUDGET = tonumber(ARGS[1]) or 600
local SEED = tonumber(ARGS[2]) or 20261018

local manifest = require("./manifest")
local seeds = require("./seeds")

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
		return require(base .. "schema_tables"), require(base .. "save"),
			require(base .. "load"), require(base .. "refusal"), require(base .. "text")
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

-- L.load is the tolerant WALK, not the untrusted-input door: its framing
-- precondition is Refusal.ReadStrict's (a form-1 byte, a non-negative id
-- count that fits the file). The fuzzer feeds it only framed inputs, and the
-- malformed framing is strict's target; otherwise a negative count makes the
-- walk read past the buffer (FIX-CARD in the report).
local function framed(s)
	if #s < 9 or string.byte(s, 1) ~= 1 then return false end
	local b = buffer.fromstring(s)
	local count = integer.tonumber(buffer.readinteger(b, #s - 8))
	return count >= 0 and count <= math.floor((#s - 9) / 8)
end

local function run(kind, r, data)
	local rep = newReport()
	local v
	local ok
	if kind == "load" then
		ok, v = pcall(r.rt.load.load, r.name, buffer.fromstring(data), rep)
	elseif kind == "strict" then
		ok, v = pcall(r.rt.strict.ReadStrict, r.module, r.name, buffer.fromstring(data), rep)
	else
		ok, v = pcall(r.rt.text.LoadText, r.module, r.name, data, rep)
	end
	if not ok then
		return false, "error:" .. tostring(v), rep, nil
	end
	local good, why = outcome(kind, v, rep)
	if not good then return false, why, rep, v end
	return true, signature(kind, v, rep), rep, v
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
			s = string.sub(s, 1, i - 1) .. string.sub(s, j, i, -1) .. string.sub(s, i, j - 1) .. string.sub(s, j + 1)
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
local start = os.clock()
local iter, findings = 0, 0
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
		local good, why, rep
		if kind == "load" and not framed(data) then
			good, why = true, "framing_refused"
		else
			good, why, rep = run(kind, r, data)
		end
		if not good then
			findings = findings + 1
			print("FINDING|" .. kind .. "|" .. r.unit .. "|" .. r.name .. "|" .. tostring(SEED) .. "|" .. tostring(iter) .. "|" .. tostring(why) .. "|" .. hex(data))
		else
			local key = kind .. "|" .. r.unit .. "|" .. r.name .. "|" .. why
			if seen[key] == nil then
				seen[key] = true
				if #pool < 400 then
					add(kind, data, r.unit, r.name)
				end
			end
		end
	end
end
print("FUZZ-DONE|iterations=" .. tostring(iter) .. "|findings=" .. tostring(findings) .. "|roots=" .. tostring(#roots) .. "|seeds=" .. tostring(#pool))
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
		return require(base .. "schema_tables"), require(base .. "load"),
			require(base .. "refusal"), require(base .. "text")
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

local failed, ran = 0, 0
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
			ok, v = pcall(r.rt.load.load, r.name, buffer.fromstring(data), rep)
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
		if why ~= nil then
			print("REPLAY-FAIL|" .. e.name .. "|" .. why .. "|" .. hex(data))
			failed = failed + 1
		end
	end
end
print("REPLAY-DONE|inputs=" .. tostring(ran) .. "|failed=" .. tostring(failed))
if failed > 0 then os.exit(1) end
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
		name=$$(basename "$$f")
		esc=$$(od -An -v -tx1 "$$f" | tr -d ' \n' | sed 's/../\\x&/g')
		printf '\t{ name = "%s", kind = "%s", unit = "%s", root = "%s", data = buffer.fromstring("%s") },\n' \
			"$$name" "$$kind" "$$unit" "$$root" "$$esc"
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

$(LUAU_NF)/seeds.luau: make/schema-luau-nightly-fuzz.mk | luau-nf-dir
	$(file >$(LUAU_NF)/gen-seeds.sh,$(LUAU_NF_SEEDS))
	@sh $(LUAU_NF)/gen-seeds.sh $@

$(LUAU_NF)/fuzz.luau: make/schema-luau-nightly-fuzz.mk | luau-nf-dir
	$(file >$@,$(LUAU_NF_FUZZ_LUAU))

$(LUAU_NF)/replay.luau: make/schema-luau-nightly-fuzz.mk | luau-nf-dir
	$(file >$@,$(LUAU_NF_REPLAY_LUAU))

$(LUAU_NF)/gen-replay.sh: make/schema-luau-nightly-fuzz.mk | luau-nf-dir
	$(file >$@,$(LUAU_NF_REPLAYGEN))

$(LUAU_NF_GEN)/.stamp: make/schema-luau-nightly-fuzz.mk $(LUAU_NF_SCHEMAS)
	@rm -rf $(LUAU_NF_GEN) && mkdir -p $(LUAU_NF_GEN)
	@for p in $(LUAU_NF_SCHEMAS); do \
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
	nice -n 19 go build -tags schema_leg_luau -o $@ ./cmd/schema

luau-nightly-fuzz: luau-nightly-fuzz-toolchain build/schema-luau-nightly $(LUAU_NF_GEN)/.stamp $(LUAU_NF)/manifest.luau $(LUAU_NF)/seeds.luau $(LUAU_NF)/fuzz.luau
	@mkdir -p $(LUAU_NF)/findings
	@set +e; \
	$(LUAU) $(LUAU_NF)/fuzz.luau -a $(LUAU_FUZZ_TIME) $(LUAU_FUZZ_SEED) > $(LUAU_NF)/fuzz.log 2>&1; \
	status=$$?; cat $(LUAU_NF)/fuzz.log; \
	if [ $$status -ne 0 ]; then echo "luau nightly fuzz: the harness died (exit $$status)"; exit $$status; fi; \
	if grep -q '^FINDING|' $(LUAU_NF)/fuzz.log; then \
		grep '^FINDING|' $(LUAU_NF)/fuzz.log | while IFS='|' read -r _ kind unit root seed iter why hex; do \
			f=$(LUAU_NF)/findings/$$kind-$$unit-$$root-$$seed-$$iter.bin; \
			printf '%s' "$$hex" | xxd -r -p > "$$f"; \
			{ echo seed=$$seed; echo iteration=$$iter; echo kind=$$kind; echo unit=$$unit; \
			  echo root=$$root; echo reason=$$why; } > "$$f.meta"; \
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
# a scratch copy of the generated readers has the r_u8 bounds check removed,
# and the fuzzer must find it; the found input must then FAIL the replay on
# the same sabotaged copy. The unplanted copy must stay green. The red line is
# kept: the plant is never in the committed reader.
LUAU_NF_NC := build/luau-nightly-fuzz-negative
luau-nightly-fuzz-negative-control: luau-nightly-fuzz-toolchain build/schema-luau-nightly $(LUAU_NF_GEN)/.stamp $(LUAU_NF)/manifest.luau $(LUAU_NF)/seeds.luau $(LUAU_NF)/fuzz.luau $(LUAU_NF)/replay.luau $(LUAU_NF)/gen-replay.sh
	@rm -rf $(LUAU_NF_NC) && mkdir -p $(LUAU_NF_NC) \
		&& cp -r $(LUAU_NF)/generated $(LUAU_NF_NC)/generated \
		&& cp $(LUAU_NF)/manifest.luau $(LUAU_NF)/seeds.luau $(LUAU_NF)/fuzz.luau $(LUAU_NF)/replay.luau $(LUAU_NF_NC)/ \
		&& cp $(LUAU_NF)/gen-replay.sh $(LUAU_NF_NC)/
	@planted=0; for f in $(LUAU_NF_NC)/generated/*/load.luau; do \
		if grep -q 'if r.at >= r.e then r.bad = true; return 0 end' "$$f"; then \
			perl -0pi -e 's/if r\.at >= r\.e then r\.bad = true; return 0 end/-- PLANTED: the r_u8 bounds check is removed/' "$$f"; \
			planted=1; fi; done; \
	if [ $$planted -ne 1 ]; then echo "negative control: no r_u8 bounds check was planted" >&2; exit 1; fi
	@set +e; \
	$(LUAU) $(LUAU_NF_NC)/fuzz.luau -a 20 1 > $(LUAU_NF_NC)/fuzz.log 2>&1; \
	cat $(LUAU_NF_NC)/fuzz.log; \
	if ! grep -q '^FINDING|' $(LUAU_NF_NC)/fuzz.log; then \
		echo "negative control failed: the fuzzer did not find the planted reader bug" >&2; exit 1; fi; \
	echo "negative control: the fuzzer found the planted reader bug"
	@mkdir -p $(LUAU_NF_NC)/findings; \
	grep '^FINDING|' $(LUAU_NF_NC)/fuzz.log | head -1 | while IFS='|' read -r _ kind unit root seed iter why hex; do \
		f=$(LUAU_NF_NC)/findings/$$kind-$$unit-$$root-$$seed-$$iter.bin; \
		printf '%s' "$$hex" | xxd -r -p > "$$f"; \
		{ echo seed=$$seed; echo iteration=$$iter; echo kind=$$kind; echo unit=$$unit; \
		  echo root=$$root; echo reason=$$why; } > "$$f.meta"; \
	done
	@sh $(LUAU_NF_NC)/gen-replay.sh $(LUAU_NF_NC)/findings $(LUAU_NF_NC)/replaydata.luau
	@set +e; \
	if $(LUAU) $(LUAU_NF_NC)/replay.luau > $(LUAU_NF_NC)/replay.log 2>&1; then \
		cat $(LUAU_NF_NC)/replay.log; \
		echo "negative control failed: the saved input passed replay on the sabotaged reader" >&2; exit 1; fi; \
	cat $(LUAU_NF_NC)/replay.log; \
	echo "negative control: the saved input fails replay on the sabotaged reader"
	@if $(LUAU) $(LUAU_NF)/fuzz.luau -a 3 $(LUAU_FUZZ_SEED) 2>&1 | grep -q '^FINDING|'; then \
		echo "negative control failed: the unplanted reader went red" >&2; exit 1; fi; \
	echo "negative control: the unplanted reader stays green"
