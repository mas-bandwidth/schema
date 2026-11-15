package luau

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
)

// TestLuauUntakenBranchZeroesByteArrays pins SPEC §5 for the byte arrays
// inside a branch: a packet read zeroes the fields of the untaken side, and a
// [N]uint8 or [..N]uint8 field stores as a 0-based buffer, so its zero form
// zeroes that buffer rather than indexing it. The flag-false wire reads over a
// value whose arrays were filled first, the flag-true wire still reads every
// byte and count exactly, and Zero<T> zeroes the buffers of each type.
func TestLuauUntakenBranchZeroesByteArrays(t *testing.T) {
	t.Parallel()
	u := unitFromSchemas(t, map[string]string{"Packet.schema": `package probe

type Bytes
{
    data [..5]uint8
}

type Packet
{
    flag bool
    if flag
    {
        raw  [3]uint8
        some [..4]uint8
        nest Bytes
    }
    else
    {
        y uint16
    }
}
`})
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeAll(t, dir, files)
	writeFile(t, dir, "main.luau", byteArrayZeroProbe)
	if out := string(luaurun.Run(t, dir, "main.luau", nil)); out != "ok\n" {
		t.Fatalf("the byte-array zero probe printed %q, want ok", out)
	}
	luaurun.Analyze(t, dir, analyzable(files, "main.luau")...)
}

const byteArrayZeroProbe = `local serialize = require("./serialize")
local P = require("./Packet")

local function fill(b: buffer, value: number, n: number)
	for i = 0, n - 1 do buffer.writeu8(b, i, value) end
end

local function allzero(b: buffer): boolean
	for i = 0, buffer.len(b) - 1 do
		if buffer.readu8(b, i) ~= 0 then return false end
	end
	return true
end

local function writePacket(v: any): buffer
	local stream = serialize.WriteStream.new(buffer.create(2048))
	assert(P.WritePacket(stream, v), "write Packet")
	stream:flush()
	return stream:data()
end

-- flag false: y rides, the byte arrays do not, and the read target's arrays
-- were filled first so a read that leaves them untouched is visible.
local src = P.NewPacket()
src.Flag = false
src.Y = 0x1234
local wire = writePacket(src)

local got = P.NewPacket()
got.Flag = true
got.Y = 0xBEEF
fill(got.Raw, 0xAA, 3)
fill(got.Some, 0xBB, 4)
got.SomeCount = 4
fill(got.Nest.Data, 0xCC, 5)
got.Nest.DataCount = 5

assert(P.ReadPacket(serialize.ReadStream.new(wire), got), "read flag-false Packet")
assert(got.Flag == false, "flag-false reads flag false")
assert(got.Y == 0x1234, "flag-false reads y exactly")
assert(allzero(got.Raw), "flag-false zeroes raw")
assert(allzero(got.Some) and got.SomeCount == 0, "flag-false zeroes some and its count")
assert(allzero(got.Nest.Data) and got.Nest.DataCount == 0, "flag-false zeroes nest.data and its count")

-- flag true: every byte array and count reads exactly, and the untaken else
-- is zero.
local full = P.NewPacket()
full.Flag = true
fill(full.Raw, 1, 3)
fill(full.Some, 2, 4)
full.SomeCount = 4
fill(full.Nest.Data, 3, 5)
full.Nest.DataCount = 5
local fullWire = writePacket(full)

local back = P.NewPacket()
assert(P.ReadPacket(serialize.ReadStream.new(fullWire), back), "read flag-true Packet")
assert(back.Flag == true, "flag-true reads flag true")
assert(back.Y == 0, "flag-true zeroes the untaken y")
for i = 0, 2 do assert(buffer.readu8(back.Raw, i) == 1, "flag-true reads raw") end
for i = 0, 3 do assert(buffer.readu8(back.Some, i) == 2, "flag-true reads some") end
assert(back.SomeCount == 4, "flag-true reads someCount")
for i = 0, 4 do assert(buffer.readu8(back.Nest.Data, i) == 3, "flag-true reads nest.data") end
assert(back.Nest.DataCount == 5, "flag-true reads nest.dataCount")

-- Zero<T> on a filled value of each type zeroes every buffer and count.
local z = P.NewPacket()
z.Flag = true
z.Y = 0xABCD
fill(z.Raw, 0xAA, 3)
fill(z.Some, 0xBB, 4)
z.SomeCount = 4
fill(z.Nest.Data, 0xCC, 5)
z.Nest.DataCount = 5
P.ZeroPacket(z)
assert(z.Flag == false and z.Y == 0, "ZeroPacket zeroes scalars")
assert(allzero(z.Raw), "ZeroPacket zeroes raw")
assert(allzero(z.Some) and z.SomeCount == 0, "ZeroPacket zeroes some and its count")
assert(allzero(z.Nest.Data) and z.Nest.DataCount == 0, "ZeroPacket zeroes nest.data and its count")

local b = P.NewBytes()
fill(b.Data, 0xDD, 5)
b.DataCount = 5
P.ZeroBytes(b)
assert(allzero(b.Data) and b.DataCount == 0, "ZeroBytes zeroes data and its count")

print("ok")
`
