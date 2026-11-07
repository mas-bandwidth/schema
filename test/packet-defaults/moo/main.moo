--!strict
-- Packet defaults, the moo leg (test/packet-defaults/README.md). The C++
-- oracle's eight byte and bit pins arrive as inputs ("<name>.bin" and
-- "<name>.bits", testdata/wire/packet-defaults); the generated Defaults and
-- Plain modules sit beside this main. moo has one codec tier (the runtime
-- streams), so every pin is written byte and bit exact and read back with its
-- consumed bits checked, into fresh and into reused storage.
--
-- moo storage (docs/PORT-MOO.md, THE VALUE MAPPING): a string(N) is a moo
-- string, replaced whole on a read; a bytes(N) is a buffer of N with its used
-- length beside it, written in place, so a short read keeps the buffer's
-- unused tail; a flags set is a serialize.Uint64.
--
-- Each failed check prints one line, "FAILED: <what>", and the run exits
-- nonzero at the end; the negative control greps its exact marker.
local serialize = require("./serialize")
local d = require("./Defaults")
local p = require("./Plain")
local inputs = require("./inputs")

local U64 = serialize.Uint64
local failed = 0

local function check(ok: boolean, what: string)
	if not ok then
		print("FAILED: " .. what)
		failed += 1
	end
end

-- same: deep equality over the generated values — buffers by content, wide
-- values by class and value, tables key by key both ways.
local function same(a: any, b: any): boolean
	if type(a) == "buffer" or type(b) == "buffer" then
		return type(a) == "buffer" and type(b) == "buffer" and buffer.tostring(a) == buffer.tostring(b)
	end
	if U64.is(a) or U64.is(b) then
		return U64.is(a) and U64.is(b) and U64.cmp(a, b) == 0
	end
	if type(a) == "table" and type(b) == "table" then
		for k, v in a do
			if not same(v, b[k]) then
				return false
			end
		end
		for k in b do
			if a[k] == nil then
				return false
			end
		end
		return true
	end
	return a == b
end

local function filled(n: number, byte: number): buffer
	local b = buffer.create(n)
	buffer.fill(b, 0, byte)
	return b
end

local function prefixed(n: number, s: string): buffer
	local b = buffer.create(n)
	buffer.writestring(b, 0, s)
	return b
end

-- The expected Sample values, built here and never by the generated code.
local function zero(): any
	return {
		Name = "",
		Token = buffer.create(4),
		TokenLength = 0,
		Caps = U64.new(0, 0),
		EmptyName = "",
		EmptyToken = buffer.create(2),
		EmptyTokenLength = 0,
		EmptyCaps = U64.new(0, 0),
	}
end

local function sample(): any
	local v = zero()
	v.Name = "\xc3\xa9\xf0\x90\x80\x80"
	v.Token = prefixed(4, "\x5c\x6e\x5c\x74")
	v.TokenLength = 4
	v.Caps = U64.new(5, 0)
	return v
end

local function short(): any
	local v = zero()
	v.Name = "A"
	v.Token = prefixed(4, "\x00\xff")
	v.TokenLength = 2
	v.Caps = U64.new(2, 0)
	return v
end

local function dirty(): any
	return {
		Name = "dddddd",
		Token = filled(4, 0xa1),
		TokenLength = 4,
		Caps = U64.new(7, 0),
		EmptyName = "ooo",
		EmptyToken = filled(2, 0xb1),
		EmptyTokenLength = 2,
		EmptyCaps = U64.new(7, 0),
	}
end

-- overlay is what a read of sent leaves in initial: text replaced, the used
-- bytes written over the buffer's prefix, the unused tail kept.
local function overlay(initial: any, sent: any): any
	initial.Name = sent.Name
	buffer.copy(initial.Token, 0, sent.Token, 0, sent.TokenLength)
	initial.TokenLength = sent.TokenLength
	initial.Caps = sent.Caps
	initial.EmptyName = sent.EmptyName
	buffer.copy(initial.EmptyToken, 0, sent.EmptyToken, 0, sent.EmptyTokenLength)
	initial.EmptyTokenLength = sent.EmptyTokenLength
	initial.EmptyCaps = sent.EmptyCaps
	return initial
end

local function write(mod: any, name: string, value: any): (string, number)
	local stream = serialize.WriteStream.new(buffer.create(4096))
	check(mod["Write" .. name](stream, value), "write " .. name)
	local bits = stream:bitsProcessed()
	stream:flush()
	return buffer.tostring(stream:data()), bits
end

local function checkRead(name: string, bytes: string, bits: number, initial: any, want: any, what: string)
	local stream = serialize.ReadStream.new(buffer.fromstring(bytes))
	check((d :: any)["Read" .. name](stream, initial), what .. ": read " .. name)
	check(stream:bitsProcessed() == bits, what .. ": read consumed bits")
	check(stream:serializeEnd(), what .. ": serializeEnd")
	check(same(initial, want), what .. ": values and backing storage")
end

local function pin(name: string, golden: string, value: any, initial: () -> any, want: () -> any)
	local bytes = buffer.tostring((inputs :: any)[golden .. ".bin"])
	local bits = tonumber(string.match(buffer.tostring((inputs :: any)[golden .. ".bits"]), "%d+")) :: number
	local got, gotBits = write(d, name, value)
	check(got == bytes, "C++ pin " .. golden .. ": bytes")
	check(gotBits == bits, "C++ pin " .. golden .. ": bits")
	checkRead(name, bytes, bits, initial(), want(), "C++ pin " .. golden)
end

-- Construction, in-place reset (storage kept) and the zero form.
check(same(d.NewSample(), sample()), "packet-default constructor bytes")
local reused = dirty()
local token, emptyToken = reused.Token, reused.EmptyToken
d.InitSample(reused)
check(same(reused, sample()), "InitSample restores the defaults")
check(rawequal(reused.Token, token) and rawequal(reused.EmptyToken, emptyToken), "InitSample keeps the buffers")
d.ZeroSample(reused)
check(same(reused, zero()), "ZeroSample is the zero form")
check(rawequal(reused.Token, token) and rawequal(reused.EmptyToken, emptyToken), "ZeroSample keeps the buffers")

-- Explicit empty defaults and a short literal with a zero tail.
check(same(d.NewEmptyOnly(), { Name = "", Token = buffer.create(1), TokenLength = 0, Caps = U64.new(0, 0) }), "explicit empty defaults")
local prefix = d.NewPrefix()
check(same(prefix, { Name = "\xc3\xa9", Token = prefixed(5, "\x5c\x6e"), TokenLength = 2 }), "short literals with zero tails")
prefix.Name = "zzzzz"
buffer.fill(prefix.Token, 0, 255)
d.InitPrefix(prefix)
check(same(prefix, d.NewPrefix()), "InitPrefix clears the tail")

-- The unsigned bit-63 and full 64-bit masks, and a 33-bit mask between
-- three leading and two trailing bits: 5 | ((1 << 32) << 3) | (2 << 36).
local wide = d.NewWideMask()
check(same(wide.High, U64.new(0, 0x80000000)), "WideMask high is bit 63")
check(same(wide.All, U64.new(0xffffffff, 0xffffffff)), "WideMask all is every bit")
local wideBytes = "\x00\x00\x00\x00\x00\x00\x00\x80\xff\xff\xff\xff\xff\xff\xff\xff"
local wideGot, wideBits = write(d, "WideMask", wide)
check(wideGot == wideBytes and wideBits == 128, "WideMask wire")
checkRead("WideMask", wideBytes, 128, { High = U64.new(0, 0), All = U64.new(0, 0) }, d.NewWideMask(), "WideMask")
local split = d.NewSplitMask()
split.Lead, split.Mask, split.Tail = 5, U64.new(0, 1), 2
local splitGot, splitBits = write(d, "SplitMask", split)
check(splitGot == "\x05\x00\x00\x00\x28" and splitBits == 38, "SplitMask wire")
checkRead("SplitMask", "\x05\x00\x00\x00\x28", 38, d.NewSplitMask(), split, "SplitMask")

-- The defaultless twin, at the same explicit values, writes the same packet.
local plainBytes, plainBits = write(p, "Sample", sample())
local defaultBytes, defaultBits = write(d, "Sample", d.NewSample())
check(plainBytes == defaultBytes and plainBits == defaultBits, "defaultless twin")

-- The eight C++ pins.
pin("Sample", "sample-defaults", d.NewSample(), zero, sample)

local batch = d.NewBatch()
check(batch.CountedCount == 1 and same(batch.Head, sample()), "Batch construction")
for i = 1, 2 do
	check(same(batch.Items[i], sample()), "Batch items backing element " .. i)
end
for i = 1, 3 do
	check(same(batch.Counted[i], sample()), "Batch counted backing element " .. i)
end
local function reusedBatch(): any
	local b = d.NewBatch()
	b.Counted[2] = dirty()
	b.Counted[3] = short()
	return b
end
pin("Batch", "batch-defaults", batch, reusedBatch, reusedBatch)

local z = d.NewZeroCount()
check(z.ItemsCount == 0 and same(z.Items, { sample(), sample() }), "ZeroCount is born at count zero with built elements")
pin("ZeroCount", "zero-count", z, function(): any
	return { Items = { dirty(), short() }, ItemsCount = 2 }
end, function(): any
	return { Items = { dirty(), short() }, ItemsCount = 0 }
end)

local on = d.NewConditional()
check(on.Enabled == true and same(on.Value, sample()), "Conditional construction")
pin("Conditional", "conditional-on", on, function(): any
	return { Enabled = false, Value = zero() }
end, d.NewConditional)
local off = d.NewConditional()
off.Enabled = false
pin("Conditional", "conditional-off", off, function(): any
	return { Enabled = true, Value = dirty() }
end, function(): any
	return { Enabled = false, Value = zero() }
end)

local function choice(): any
	local c = d.NewChoice()
	c.Type = d.ChoiceType.Sample
	return c
end
pin("Choice", "choice-sample", choice(), function(): any
	local c = d.NewChoice()
	c.Type = d.ChoiceType.Conditional
	return c
end, choice)
pin("Sample", "sample-short", short(), dirty, function(): any
	return overlay(dirty(), short())
end)
pin("Sample", "sample-empty", zero(), dirty, function(): any
	return overlay(dirty(), zero())
end)

-- Repeated selection: every read of the Sample arm takes fresh defaults
-- before its payload, in the same arm table and buffers, twice.
for _, sent in { short(), zero() } do
	local value = choice()
	value.Sample = sent
	local bytes, bits = write(d, "Choice", value)
	local output = d.NewChoice()
	output.Type = d.ChoiceType.Conditional
	local arm = output.Sample
	local armToken = arm.Token
	for attempt = 1, 2 do
		arm.Name = "dddddd"
		arm.EmptyName = "ooo"
		buffer.fill(arm.Token, 0, 0xa1)
		buffer.fill(arm.EmptyToken, 0, 0xb1)
		local want = choice()
		want.Sample = overlay(sample(), sent)
		checkRead("Choice", bytes, bits, output, want, "repeated Sample selection " .. attempt)
		check(rawequal(output.Sample, arm) and rawequal(arm.Token, armToken), "the Sample arm keeps its storage")
	end
end

-- An untaken conditional inside a freshly selected arm reads to zero.
local offChoice = d.NewChoice()
offChoice.Type = d.ChoiceType.Conditional
offChoice.Conditional.Enabled = false
local offBytes, offBits = write(d, "Choice", offChoice)
local output = choice()
for attempt = 1, 2 do
	output.Conditional.Enabled = true
	output.Conditional.Value = dirty()
	local want = d.NewChoice()
	want.Type = d.ChoiceType.Conditional
	want.Conditional.Enabled = false
	want.Conditional.Value = zero()
	checkRead("Choice", offBytes, offBits, output, want, "repeated Conditional selection " .. attempt)
end

if failed > 0 then
	error(string.format("packet defaults moo: %d check(s) failed", failed), 0)
end
print("packet defaults moo: constructors, eight C++ goldens and reused storage OK")
