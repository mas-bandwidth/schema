--!nocheck
-- messagefuzz.luau — Form-2 differential message-fuzz pipe driver for Luau.
local units = require("../../../build/luau-driver/units")

type Unit = {
	S: any,
	M: any,
	announcement: buffer,
	ownVocab: any,
}

type Entry = {
	u: Unit,
	root: string,
}

local loaded: { [string]: Unit | false } = {}

local function openUnit(key: string): Unit?
	local have = loaded[key]
	if have ~= nil then
		return if have == false then nil else have
	end
	local base = "../../../build/conformance-luau/" .. key .. "/"
	local ok, got = pcall(function(): Unit?
		local S = require(base .. "schema_tables")
		local announcement: buffer? = nil
		for _, name in units[key] or {} do
			local okm, mod = pcall(function(): any return require(base .. name) end)
			if okm and mod ~= nil and announcement == nil and mod.TableMessageAnnouncement ~= nil then
				announcement = mod.TableMessageAnnouncement
			end
		end
		if announcement == nil then
			return nil
		end
		local okM, M = pcall(function(): any return require(base .. "schema_message") end)
		if not okM or M == nil then
			return nil
		end
		local ownVocab = M.NewVocabulary()
		local r = {}
		if not M.AnnounceRead(ownVocab, announcement, r) then
			return nil
		end
		return {
			S = S,
			M = M,
			announcement = announcement,
			ownVocab = ownVocab,
		}
	end)
	if ok and got ~= nil then
		loaded[key] = got
		return got
	end
	loaded[key] = false
	return nil
end

local function makeReply(ok: boolean, unknown: number, widened: number, malformed: boolean, refused: boolean, saved: buffer?): buffer
	local slen = if saved ~= nil then buffer.len(saved) else -1
	local out = buffer.create(47 + (if saved ~= nil then slen else 0))
	buffer.writeu8(out, 0, if ok then 1 else 0)
	buffer.writei32(out, 1, unknown)
	buffer.writei32(out, 5, 0)
	buffer.writei32(out, 9, widened)
	buffer.writei32(out, 13, 0)
	buffer.writei32(out, 17, 0)
	buffer.writeu8(out, 21, if malformed then 1 else 0)
	buffer.writeu8(out, 22, if refused then 1 else 0)
	buffer.writeu32(out, 23, 0xffffffff)
	buffer.writeu32(out, 27, 0xffffffff)
	buffer.writei32(out, 31, 0)
	buffer.writei32(out, 35, 0)
	if saved == nil then
		buffer.writeu32(out, 39, 0xffffffff)
		buffer.writeu32(out, 43, 0xffffffff)
	else
		buffer.writeu32(out, 39, slen)
		buffer.writeu32(out, 43, 0)
		if slen > 0 then
			buffer.copy(out, 47, saved, 0, slen)
		end
	end
	return out
end

local function answer(e: Entry, frame: buffer): buffer
	local wlen = buffer.len(frame)
	if wlen < 4 then
		return makeReply(false, 0, 0, true, true, nil)
	end
	local alen = buffer.readu32(frame, 0)
	if 4 + alen > wlen then
		return makeReply(false, 0, 0, true, true, nil)
	end
	local u = e.u
	local vocab = u.ownVocab
	local rep: any = {}
	if alen > 0 then
		local ann = buffer.create(alen)
		buffer.copy(ann, 0, frame, 4, alen)
		vocab = u.M.NewVocabulary()
		if not u.M.AnnounceRead(vocab, ann, rep) or rep.malformed or (rep.verdict or 0) ~= 0 or (rep.reason ~= nil and rep.reason ~= "") then
			return makeReply(false, rep.unknown or 0, rep.widened or 0, rep.malformed == true, true, nil)
		end
		rep = {}
	end
	local blen = wlen - 4 - alen
	local batch = buffer.create(blen)
	if blen > 0 then
		buffer.copy(batch, 0, frame, 4 + alen, blen)
	end
	local count, bodies = u.M.LoadMessages(vocab, e.root, batch, 256, rep)
	if count < 1 or rep.malformed or (rep.verdict or 0) ~= 0 or (rep.reason ~= nil and rep.reason ~= "") then
		return makeReply(false, rep.unknown or 0, rep.widened or 0, rep.malformed == true, true, nil)
	end
	local srep: any = {}
	local sz = u.M.MeasureMessages(u.ownVocab, e.root, bodies, srep)
	if sz < 0 then
		return makeReply(false, rep.unknown or 0, rep.widened or 0, false, true, nil)
	end
	local out, n = u.M.SaveMessages(u.ownVocab, e.root, bodies, srep)
	if out == nil or n ~= sz then
		return makeReply(false, rep.unknown or 0, rep.widened or 0, false, true, nil)
	end
	return makeReply(true, rep.unknown or 0, rep.widened or 0, false, false, out)
end

local host = (_G :: any).__luaufuzz
assert(host ~= nil and host.read ~= nil, "messagefuzz.luau: run under luaufuzzhost")

local function readExact(n: number): buffer?
	local b: buffer = host.read(n)
	if buffer.len(b) < n then
		return nil
	end
	return b
end

local function u32(): number?
	local b = readExact(4)
	return if b ~= nil then buffer.readu32(b, 0) else nil
end

local function str(): string
	local nb = readExact(2)
	assert(nb ~= nil, "messagefuzz: short name")
	local b = readExact(buffer.readu16(nb, 0))
	assert(b ~= nil, "messagefuzz: short name")
	return buffer.tostring(b)
end

local count = u32()
assert(count ~= nil, "messagefuzz: no roster")
local roster: { Entry | false } = {}
local reply = buffer.create(count)
for i = 1, count do
	local key = str()
	local root = str()
	local fb = readExact(2)
	assert(fb ~= nil, "messagefuzz: short entry")
	local form, retain = buffer.readu8(fb, 0), buffer.readu8(fb, 1)
	local entry: Entry | false = false
	if form == 5 and retain == 0 then
		local u = openUnit(key)
		if u ~= nil then
			local okd, d = pcall(u.S.lookup, root)
			if okd and d ~= nil and d.table and d.fixed then
				entry = { u = u, root = root }
			end
		end
	end
	roster[i] = entry
	buffer.writeu8(reply, i - 1, if entry then 1 else 0)
end
host.write(reply)

while true do
	local index = u32()
	if index == nil then break end
	local n = u32()
	assert(n ~= nil, "messagefuzz: short mutant header")
	local frame = readExact(n)
	assert(frame ~= nil, "messagefuzz: short mutant")
	local entry = roster[index + 1]
	assert(entry, "messagefuzz: absent entry")
	host.write(answer(entry, frame))
end
