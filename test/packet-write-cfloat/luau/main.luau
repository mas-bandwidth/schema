--!strict
local serialize = require("./serialize")
local c = require("./CFloat")
local cases: { { name: string, value: number, written: boolean, expected: number } } = require("./cases") :: any
for _, case in cases do
    local value = c.NewCFloat()
    value.X = case.value
    local stream = serialize.WriteStream.new(buffer.create(8))
    local called, ok = pcall(c.WriteCFloat, stream, value)
    if not called or ok ~= case.written then error("FAILED: packet-write-cfloat verdict on " .. case.name) end
    if ok then
        stream:flush()
        local read = serialize.ReadStream.new(stream:data())
        local output = c.NewCFloat()
        if not c.ReadCFloat(read, output) or math.abs(output.X - case.expected) > 0.00001 then
            error("FAILED: packet-write-cfloat roundtrip on " .. case.name)
        end
    end
end
print("packet-write-cfloat Luau: all cases passed")
