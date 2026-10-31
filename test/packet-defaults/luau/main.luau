--!strict
-- The Luau driver of the packet value-defaults fixture
-- (test/packet-defaults/README.md): constructors and Init at the declared
-- string, bytes and flags defaults, the C++-pinned bytes and bit counts of
-- testdata/wire/packet-defaults, and reads into reused storage, including
-- repeated union selections that reset the selected arm in place. pins.luau
-- carries the pins in as source (the interpreter has no io library); plain/
-- is Plain.schema, the defaultless twin.
local serialize = require("./serialize")
local d = require("./Defaults")
local p = require("./plain/Plain")
local pins: { [string]: { hex: string, bits: number } } = require("./pins") :: any

local function bytes(n: number, list: { number }): buffer
	local b = buffer.create(n)
	for i, x in list do
		buffer.writeu8(b, i - 1, x)
	end
	return b
end

local function filled(n: number, x: number): buffer
	local b = buffer.create(n)
	buffer.fill(b, 0, x)
	return b
end

local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end

local function unhex(s: string): buffer
	local n = #s // 2
	local b = buffer.create(n)
	for i = 0, n - 1 do
		buffer.writeu8(b, i, tonumber(string.sub(s, 2 * i + 1, 2 * i + 2), 16) :: number)
	end
	return b
end

-- deepEqual compares two values field by field, buffers by content.
local function deepEqual(a: any, b: any): boolean
	if type(a) ~= type(b) then
		return false
	end
	if type(a) == "buffer" then
		return buffer.len(a) == buffer.len(b) and buffer.tostring(a) == buffer.tostring(b)
	end
	if type(a) ~= "table" then
		return a == b
	end
	for k, x in a do
		if not deepEqual(x, b[k]) then
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

local function check(ok: boolean, what: string)
	if not ok then
		error("FAILED: " .. what, 2)
	end
end

local function zero(): any
	local v: any = d.NewSample()
	v.Name, v.NameLength = buffer.create(6), 0
	v.Token, v.TokenLength = buffer.create(4), 0
	v.Caps = 0i
	v.EmptyName, v.EmptyNameLength = buffer.create(3), 0
	v.EmptyToken, v.EmptyTokenLength = buffer.create(2), 0
	v.EmptyCaps = 0i
	return v
end

local function sample(): any
	local v = zero()
	v.Name, v.NameLength = bytes(6, { 195, 169, 240, 144, 128, 128 }), 6
	v.Token, v.TokenLength = bytes(4, { 92, 110, 92, 116 }), 4
	v.Caps = 5i
	return v
end

local function short(): any
	local v = zero()
	v.Name, v.NameLength = bytes(6, { 65 }), 1
	v.Token, v.TokenLength = bytes(4, { 0, 255 }), 2
	v.Caps = 2i
	return v
end

local function dirty(): any
	local v = zero()
	v.Name, v.NameLength = filled(6, 100), 6
	v.Token, v.TokenLength = filled(4, 161), 4
	v.Caps = 7i
	v.EmptyName, v.EmptyNameLength = filled(3, 111), 3
	v.EmptyToken, v.EmptyTokenLength = filled(2, 177), 2
	v.EmptyCaps = 7i
	return v
end

-- overlay is what a read of sent leaves in initial: the received prefixes and
-- lengths, the flags, and nothing past a used length rewritten.
local function overlay(initial: any, sent: any): any
	buffer.copy(initial.Name, 0, sent.Name, 0, sent.NameLength)
	initial.NameLength = sent.NameLength
	buffer.copy(initial.Token, 0, sent.Token, 0, sent.TokenLength)
	initial.TokenLength = sent.TokenLength
	initial.Caps = sent.Caps
	initial.EmptyNameLength, initial.EmptyTokenLength, initial.EmptyCaps = 0, 0, 0i
	return initial
end

local function write(mod: any, name: string, value: any): (string, number)
	local stream = serialize.WriteStream.new(buffer.create(4096))
	check(mod["Write" .. name](stream, value), "runtime write " .. name)
	local bits = stream:bitsProcessed()
	stream:flush()
	return hex(stream:data()), bits
end

local function checkRead(name: string, wire: string, bits: number, initial: any, want: any)
	local stream = serialize.ReadStream.new(unhex(wire))
	check((d :: any)["Read" .. name](stream, initial), "runtime read " .. name)
	check(stream:bitsProcessed() == bits, "read consumed bits " .. name)
	check(deepEqual(initial, want), name .. ": values and backing storage")
end

local function pin(name: string, file: string, value: any, initial: () -> any, want: () -> any)
	local pinned = pins[file]
	check(pinned ~= nil, "the pin " .. file .. " is carried in")
	local got, bits = write(d, name, value)
	check(got == pinned.hex and bits == pinned.bits, "C++ pin " .. file .. ": " .. got .. " " .. bits)
	checkRead(name, pinned.hex, pinned.bits, initial(), want())
end

check(deepEqual(d.NewSample(), sample()), "packet-default constructor bytes")
local reused = dirty()
local nameBuffer, tokenBuffer = reused.Name, reused.Token
d.InitSample(reused)
check(deepEqual(reused, sample()), "InitSample resets to the declared defaults")
check(reused.Name == nameBuffer and reused.Token == tokenBuffer, "InitSample reuses the buffers")
d.ZeroSample(reused)
check(deepEqual(reused, zero()), "ZeroSample is the zero form")
local empty = d.NewEmptyOnly()
check(empty.NameLength == 0 and empty.TokenLength == 0 and empty.Caps == 0i, "explicit empty defaults")
check(buffer.tostring(empty.Name) == "\0\0" and buffer.tostring(empty.Token) == "\0", "explicit empty defaults leave zero buffers")
local prefix = d.NewPrefix()
check(deepEqual(prefix.Name, bytes(5, { 195, 169 })) and prefix.NameLength == 2, "a short string default, zero tail")
check(deepEqual(prefix.Token, bytes(5, { 92, 110 })) and prefix.TokenLength == 2, "a short bytes default, zero tail")
buffer.fill(prefix.Name, 0, 255)
buffer.fill(prefix.Token, 0, 255)
d.InitPrefix(prefix)
check(deepEqual(prefix, d.NewPrefix()), "InitPrefix clears the tail")
local wide = d.NewWideMask()
check(wide.High == 0x8000000000000000i and wide.All == 0xffffffffffffffffi, "the bit-63 and full 64-bit masks")
local wideWire = "0000000000000080ffffffffffffffff"
local got, bits = write(d, "WideMask", wide)
check(got == wideWire and bits == 128, "WideMask bytes " .. got)
local split = d.NewSplitMask()
split.Lead, split.Mask, split.Tail = 5, 0x100000000i, 2
got, bits = write(d, "SplitMask", split)
check(got == "0500000028" and bits == 38, "SplitMask bytes " .. got)
local wideInitial = d.NewWideMask()
wideInitial.High, wideInitial.All = 0i, 0i
checkRead("WideMask", wideWire, 128, wideInitial, wide)
checkRead("SplitMask", "0500000028", 38, d.NewSplitMask(), split)
local plain: any = p.NewSample()
for k, x in sample() do
	plain[k] = x
end
local plainWire, plainBits = write(p, "Sample", plain)
local defaultWire, defaultBits = write(d, "Sample", d.NewSample())
check(plainWire == defaultWire and plainBits == defaultBits, "the defaultless twin writes the same bytes")
pin("Sample", "sample-defaults", d.NewSample(), zero, sample)

local batch = d.NewBatch()
check(batch.CountedCount == 1 and deepEqual(batch.Head, sample()), "Batch is born at its defaults")
for _, s in batch.Items do
	check(deepEqual(s, sample()), "every fixed element at its defaults")
end
for _, s in batch.Counted do
	check(deepEqual(s, sample()), "every backing element at its defaults")
end
local function reusedBatch(): any
	local b = d.NewBatch()
	b.Counted[2] = dirty()
	b.Counted[3] = short()
	return b
end
pin("Batch", "batch-defaults", batch, reusedBatch, reusedBatch)
local z = d.NewZeroCount()
check(z.ItemsCount == 0 and deepEqual(z.Items, { sample(), sample() }), "ZeroCount is born empty over defaulted elements")
pin("ZeroCount", "zero-count", z, function()
	local v = d.NewZeroCount()
	v.Items, v.ItemsCount = { dirty(), short() }, 2
	return v
end, function()
	local v = d.NewZeroCount()
	v.Items, v.ItemsCount = { dirty(), short() }, 0
	return v
end)
check(d.NewConditional().Enabled == true and deepEqual(d.NewConditional().Value, sample()), "Conditional is born enabled at its defaults")
pin("Conditional", "conditional-on", d.NewConditional(), function()
	local v = d.NewConditional()
	v.Enabled, v.Value = false, zero()
	return v
end, d.NewConditional)
local off = d.NewConditional()
off.Enabled = false
pin("Conditional", "conditional-off", off, function()
	local v = d.NewConditional()
	v.Value = dirty()
	return v
end, function()
	local v = d.NewConditional()
	v.Enabled, v.Value = false, zero()
	return v
end)
local function choice(): any
	local v = d.NewChoice()
	v.Type = d.ChoiceType.Sample
	return v
end
pin("Choice", "choice-sample", choice(), function()
	local v = d.NewChoice()
	v.Type = d.ChoiceType.Conditional
	return v
end, choice)
pin("Sample", "sample-short", short(), dirty, function()
	return overlay(dirty(), short())
end)
pin("Sample", "sample-empty", zero(), dirty, function()
	return overlay(dirty(), zero())
end)
for _, sent in { short(), zero() } do
	local value = choice()
	value.Sample = sent
	local wire, wireBits = write(d, "Choice", value)
	local output = d.NewChoice()
	output.Type = d.ChoiceType.Conditional
	local arm = output.Sample
	local name, token = arm.Name, arm.Token
	for _ = 1, 2 do
		buffer.fill(arm.Name, 0, 100)
		buffer.fill(arm.Token, 0, 161)
		buffer.fill(arm.EmptyName, 0, 111)
		buffer.fill(arm.EmptyToken, 0, 177)
		local want = choice()
		want.Sample = overlay(sample(), sent)
		checkRead("Choice", wire, wireBits, output, want)
		check(output.Sample == arm and arm.Name == name and arm.Token == token, "a repeated selection resets the arm in place")
	end
end
local offChoice = d.NewChoice()
offChoice.Type = d.ChoiceType.Conditional
offChoice.Conditional.Enabled = false
local offWire, offBits = write(d, "Choice", offChoice)
local output = choice()
for _ = 1, 2 do
	output.Conditional.Enabled = true
	output.Conditional.Value = dirty()
	local want = d.NewChoice()
	want.Type = d.ChoiceType.Conditional
	want.Conditional.Enabled = false
	want.Conditional.Value = zero()
	checkRead("Choice", offWire, offBits, output, want)
end
print("packet defaults Luau: constructors, Init, eight C++ goldens and reused storage OK")
