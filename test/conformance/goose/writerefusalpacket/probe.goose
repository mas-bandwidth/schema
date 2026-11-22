// THE GOOSE LEG OF write-refusal-packet (docs/SPEC.md §5.1;
// test/conformance/write-refusal-packet/rows.txt). For every row it builds a
// fresh, lawful PacketWriteRefusal, applies the row's one op, runs the
// generated packet write into a 256-byte buffer and prints one line:
// `<name> refused` when the write returns a refusal kind the contract names
// (value_out_of_range, invalid_utf8, embedded_nul), `<name> wrote` when it
// returns SERIALIZE_ERROR_NONE. Any other return is printed as
// `<name> error <kind>`, which the Go side never takes as a verdict, so a
// past-end or an unknown kind cannot pass as a refusal.
//
//   main.goose -- <rows.txt path>
import os;
import .PacketWriteRefusal;
import .serialize;

var wbuf: u8[256] = [0; 256];

// apply sets one op on v; false when the op or its argument is not one the
// row list defines (the run then fails rather than answer the row).
fn apply(v: packetrefusal::PacketWriteRefusal&, op: const u8[:], arg: const u8[:]) -> bool {
    if op == "text_bytes" {
        if arg.len % 2 != 0 || arg.len / 2 > 8 { return false; }
        let count = arg.len / 2;
        for i in count {
            let b, ok = parse_int(arg[i * 2..i * 2 + 2], 16);
            if !ok { return false; }
            v.Text[i] = b as! u8;
        }
        v.TextLength = count as! i32;
        return true;
    }
    let n, ok = parse_int(arg);
    if !ok { return false; }
    if op == "scalar" {
        v.Scalar = n as! i32;
    } else if op == "narrow" {
        v.Narrow = n as! u32;
    } else if op == "grade" {
        v.Grade = n as! packetrefusal::Grade;
    } else if op == "shot_tag" {
        v.Shot.Type = n as! packetrefusal::ShotType;
    } else if op == "window_count" {
        v.WindowCount = n as! i32;
    } else if op == "text_length" {
        v.TextLength = n as! i32;
    } else if op == "text_fill" {
        if n < 0 || n > 8 { return false; }
        for i in n { v.Text[i] = 'a'; }
        v.TextLength = n as! i32;
    } else if op == "fixed_constant" {
        v.FixedConstant = n as! i32;
    } else if op == "reserved_zero" {
        v.ReservedZero = n as! i32;
    } else {
        return false;
    }
    return true;
}

fn main() {
    let argv = args();
    if argv.len != 2 {
        write_stderr("usage: main.goose -- <rows.txt>\n");
        exit(2);
    }
    var text: u8[>..] = [];
    if !read_file(argv[1], text) {
        write_stderr("probe: cannot read the rows file\n");
        exit(2);
    }
    var out: u8[>..] = [];
    for raw in split(text, '\n') {
        let line = trim(raw);
        if line.len == 0 || line[0] == '#' { continue; }
        var f: const u8[:][>..] = [];
        for p in split(line, ' ') {
            if p.len > 0 { f.push(p); }
        }
        if f.len != 4 {
            write_stderr(str("probe: malformed row: ", line, "\n"));
            exit(2);
        }
        var value = packetrefusal::NewPacketWriteRefusal();
        if !apply(value, f[2], f[3]) {
            write_stderr(str("probe: unknown op: ", line, "\n"));
            exit(2);
        }
        var s = serialize_write_stream_init(wbuf[0..256]);
        let r = packetrefusal::WritePacketWriteRefusal(s, value);
        out.append(f[0]);
        if r == SERIALIZE_ERROR_NONE {
            out.append(" wrote\n");
        } else if r == SERIALIZE_ERROR_VALUE_OUT_OF_RANGE || r == SERIALIZE_ERROR_INVALID_UTF8 || r == SERIALIZE_ERROR_EMBEDDED_NUL {
            out.append(" refused\n");
        } else {
            out.append(str(" error ", r, "\n"));
        }
    }
    print(out);
}
