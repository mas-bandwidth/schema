// main.lobster is the probe the Lobster packet Write compressed-float range
// gate runs (test/packet-write-cfloat/cases.txt, SPEC §5.1 row 10): one case
// per run, because a refusal ends the process. It takes the case's float32 bit
// pattern and prints the bits a read returns.
import .CFloat
import .serialize

let args = command_line_arguments()
if length(args) != 1:
    print("FAILED: one float32 bit pattern expected")
else:
    let bits = string_to_int(args[0], 16)
    let value = cfloat.NewCFloat()
    value.X = serialize_float32_from_bits(bits)
    let stream = serialize_write_stream(ensure_size("", 64, 0))
    if not cfloat.WriteCFloat(stream, value):
        print("FAILED: the write refused")
    else:
        stream.flush()
        let reader = serialize_read_stream_padded(substring(stream.buffer, 0, stream.bytes_processed()))
        let back = cfloat.NewCFloat()
        if not cfloat.ReadCFloat(reader, back):
            print("FAILED: the read refused " + reader.refusal_name())
        else:
            print("wrote " + number_to_string(serialize_float32_bits(back.X), 16, 8))
