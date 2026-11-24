--!strict
local serialize = require("./serialize")
local c = require("./PacketCFloat")
local cases: { { name: string, value: number, written: boolean, expected: number, ratio: boolean, wire: string } } = require("./cases") :: any

local function hex(b: buffer): string
    local parts = {}
    for i = 0, buffer.len(b) - 1 do
        table.insert(parts, string.format("%02x", buffer.readu8(b, i)))
    end
    return table.concat(parts)
end

local ran = 0
for _, case in cases do
    ran += 1
    local stream = serialize.WriteStream.new(buffer.create(8))
    local called, ok
    if case.ratio then
        local value = c.NewRatioEndpoint()
        value.Ratio = case.value
        called, ok = pcall(c.WriteRatioEndpoint, stream, value)
    else
        local value = c.NewCFloat()
        value.X = case.value
        called, ok = pcall(c.WriteCFloat, stream, value)
    end
    local written = called and ok == true
    if written ~= case.written then error("FAILED: packet-write-cfloat verdict on " .. case.name) end
    if written then
        stream:flush()
        local wire = stream:data()
        if case.wire ~= "" and hex(wire) ~= case.wire then
            error("FAILED: packet-write-cfloat wire on " .. case.name .. ": wrote " .. hex(wire) .. ", want " .. case.wire)
        end
        local read = serialize.ReadStream.new(wire)
        local got: number
        local success: boolean
        if case.ratio then
            local output = c.NewRatioEndpoint()
            success = c.ReadRatioEndpoint(read, output)
            got = output.Ratio
        else
            local output = c.NewCFloat()
            success = c.ReadCFloat(read, output)
            got = output.X
        end
        if not success or math.abs(got - case.expected) > 0.00001 then
            error("FAILED: packet-write-cfloat roundtrip on " .. case.name)
        end
    end
end
-- The last stdout line is the count tools/mustrun reads. The first failed case
-- raises above; an empty case list is a failure (it used to print "all cases
-- passed" and exit 0).
if ran == 0 then error("FAILED: packet-write-cfloat ran no case") end
print(string.format("packet-write-cfloat Luau: ran %d, failed 0", ran))
