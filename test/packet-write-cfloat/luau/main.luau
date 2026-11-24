--!strict
local serialize = require("./serialize")
local c = require("./PacketCFloat")
local cases: { { name: string, value: number, written: boolean, expected: number, ratio: boolean, pair: boolean, wire: string, bits: number } } = require("./cases") :: any

local function hex(b: buffer): string
    local parts = {}
    for i = 0, buffer.len(b) - 1 do
        table.insert(parts, string.format("%02x", buffer.readu8(b, i)))
    end
    return table.concat(parts)
end

-- A failed case prints its answer (the line tools/casegate reads) and ends
-- the run; casegate names every case after it as unanswered.
local function fail(name: string, why: string): never
    print("case " .. name .. " FAILED: " .. why)
    error("FAILED: packet-write-cfloat " .. why .. " on " .. name)
end

local ran = 0
for _, case in cases do
    ran += 1
    local stream = serialize.WriteStream.new(buffer.create(8))
    local called, ok
    if case.pair then
        local value = c.NewRatioPair()
        value.First = case.value
        value.Second = case.value
        called, ok = pcall(c.WriteRatioPair, stream, value)
    elseif case.ratio then
        local value = c.NewRatioEndpoint()
        value.Ratio = case.value
        called, ok = pcall(c.WriteRatioEndpoint, stream, value)
    else
        local value = c.NewCFloat()
        value.X = case.value
        called, ok = pcall(c.WriteCFloat, stream, value)
    end
    local written = called and ok == true
    if written ~= case.written then fail(case.name, "verdict") end
    if written then
        -- the bits the write emitted, before the flush pads the last byte
        local bits = stream:bitsProcessed()
        if case.bits >= 0 and bits ~= case.bits then
            fail(case.name, "wrote " .. tostring(bits) .. " bits before the flush, want " .. tostring(case.bits))
        end
        stream:flush()
        local wire = stream:data()
        if case.wire ~= "" and hex(wire) ~= case.wire then
            fail(case.name, "wire: wrote " .. hex(wire) .. ", want " .. case.wire)
        end
        local read = serialize.ReadStream.new(wire)
        local got: number
        local success: boolean
        if case.pair then
            -- a RatioPair reads back only when its two fields agree
            local output = c.NewRatioPair()
            success = c.ReadRatioPair(read, output)
            got = output.First
            if output.Second ~= output.First then
                fail(case.name, "read back " .. tostring(output.First) .. " and " .. tostring(output.Second))
            end
        elseif case.ratio then
            local output = c.NewRatioEndpoint()
            success = c.ReadRatioEndpoint(read, output)
            got = output.Ratio
        else
            local output = c.NewCFloat()
            success = c.ReadCFloat(read, output)
            got = output.X
        end
        if not success or math.abs(got - case.expected) > 0.00001 then
            fail(case.name, "roundtrip")
        end
    end
    print("case " .. case.name .. " ok")
end
-- A summary; tools/casegate holds every case of the file to an answer. The
-- first failed case raises above; an empty case list is a failure.
if ran == 0 then error("FAILED: packet-write-cfloat ran no case") end
print(string.format("packet-write-cfloat Luau: ran %d, failed 0", ran))
