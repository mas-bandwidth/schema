// main.lobster is the probe the Lobster packet Write compressed-float range
// gate runs (test/packet-write-cfloat/cases.txt, SPEC §5.1 row 10): one case
// per run, because a refusal ends the process. It takes the case's float32 bit
// pattern and the type to write (CFloat or RatioEndpoint), and prints the
// bits a read returns, the written bytes in hex, and the number of bits the
// write emitted before the flush.
import .PacketCFloat
import .serialize

def wire_hex(b:string) -> string:
    var s = ""
    for(b) c:
        s += number_to_string(c, 16, 2)
    return s

let args = command_line_arguments()
if length(args) != 2:
    print("FAILED: a float32 bit pattern and a type expected")
else:
    let bits = string_to_int(args[0], 16)
    let stream = serialize_write_stream(ensure_size("", 64, 0))
    if args[1] == "RatioEndpoint":
        let value = cfloat.NewRatioEndpoint()
        value.Ratio = serialize_float32_from_bits(bits)
        if not cfloat.WriteRatioEndpoint(stream, value):
            print("FAILED: the write refused")
        else:
            let bits_written = stream.bits_processed()
            stream.flush()
            let wire = substring(stream.buffer, 0, stream.bytes_processed())
            let reader = serialize_read_stream_padded(wire)
            let back = cfloat.NewRatioEndpoint()
            if not cfloat.ReadRatioEndpoint(reader, back):
                print("FAILED: the read refused " + reader.refusal_name())
            else:
                print("wrote " + number_to_string(serialize_float32_bits(back.Ratio), 16, 8) + " wire " + wire_hex(wire) + " bits " + string(bits_written))
    else:
        let value = cfloat.NewCFloat()
        value.X = serialize_float32_from_bits(bits)
        if not cfloat.WriteCFloat(stream, value):
            print("FAILED: the write refused")
        else:
            let bits_written = stream.bits_processed()
            stream.flush()
            let wire = substring(stream.buffer, 0, stream.bytes_processed())
            let reader = serialize_read_stream_padded(wire)
            let back = cfloat.NewCFloat()
            if not cfloat.ReadCFloat(reader, back):
                print("FAILED: the read refused " + reader.refusal_name())
            else:
                print("wrote " + number_to_string(serialize_float32_bits(back.X), 16, 8) + " wire " + wire_hex(wire) + " bits " + string(bits_written))
