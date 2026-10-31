// The Goose packet-text driver (test/packet-text/README.md): one hex wire per
// stdin line; for each, REFUSE, or OK with the consumed bits, the payload,
// and the canonical rewrite's bits and bytes. The make target generates
// test/packet-text/Narrow.schema beside this file.
import os;
import .Narrow;
import .driverlib;
import .serialize;

fn main() {
    var line: u8[>..] = [];
    while read_line(line) {
        var wire: u8[>..] = [];
        hex_decode(line, wire);
        line.clear();
        var rbuf: u8[72] = [0; 72];
        var r = serialize_read_stream_init_padded(rbuf, wire);
        var v = packettext::Narrow { .. };
        for &c in v.Text { c = 0x7f; }
        var out: u8[>..] = [];
        if !packettext::ReadNarrow(r, v) {
            print("REFUSE");
            continue;
        }
        var wbuf: u8[256] = [0; 256];
        var w = serialize_write_stream_init(wbuf);
        if packettext::WriteNarrow(w, v) != SERIALIZE_ERROR_NONE { abort("driver: rewrite refused"); }
        serialize_flush(w);
        out.append("OK ");
        out.append(str(serialize_bits_processed(r)));
        out.push(' ');
        hex_append(out, v.Text[0..v.TextLength]);
        out.push(' ');
        out.append(str(serialize_bits_processed(w)));
        out.push(' ');
        hex_append(out, wbuf[0..serialize_bytes_processed(w)]);
        print(out);
    }
}
