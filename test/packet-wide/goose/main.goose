// The Goose packet-wide driver (test/packet-wide/README.md): one
// "<bound> <hex wire>" per stdin line, bound 7 or 4; for each, REFUSE, or OK
// with the consumed bits, the UTF-16 payload, and the canonical rewrite's
// bits and bytes. `-- --contracts` instead runs the contracts of
// test/packet-wide/go/contracts_test.go: writer refusals, pairing, the unused
// tail, and composition under a branch, a union and arrays (Shapes.schema).
// The make target generates examples-wide/WideText.schema and
// test/packet-wide/Shapes.schema beside this file.
import os;
import .WideText;
import .Shapes;
import .driverlib;
import .serialize;

var rbuf: u8[1032] = [0; 1032];
var wbuf: u8[1024] = [0; 1024];

fn reply(r: ReadStream&, units: const u16[:], w: WriteStream&) {
    var out: u8[>..] = [];
    out.append("OK ");
    out.append(str(serialize_bits_processed(r)));
    out.push(' ');
    let digits = "0123456789abcdef";
    if units.len == 0 { out.push('-'); }
    for u in units {
        out.push(digits[(u >> 12) & 15]); out.push(digits[(u >> 8) & 15]);
        out.push(digits[(u >> 4) & 15]); out.push(digits[u & 15]);
    }
    out.push(' ');
    out.append(str(serialize_bits_processed(w)));
    out.push(' ');
    hex_append(out, wbuf[0..serialize_bytes_processed(w)]);
    print(out);
}

fn drive() {
    var line: u8[>..] = [];
    while read_line(line) {
        guard line.len >= 2 && line[1] == ' ' else { abort("driver: bound and wire required"); }
        let bound = line[0];
        var wire: u8[>..] = [];
        hex_decode(line[2..line.len], wire);
        line.clear();
        for i in 1024 { wbuf[i] = 0; }
        var r = serialize_read_stream_init_padded(rbuf, wire);
        var w = serialize_write_stream_init(wbuf);
        if bound == '7' {
            var v = wide::WideSeven { .. };
            for &u in v.Text { u = 0x7f7f; }
            if !wide::ReadWideSeven(r, v) { print("REFUSE"); continue; }
            if wide::WriteWideSeven(w, v) != SERIALIZE_ERROR_NONE { abort("driver: rewrite refused"); }
            serialize_flush(w);
            reply(r, v.Text[0..v.TextLength], w);
        } else if bound == '4' {
            var v = wide::WideFour { .. };
            for &u in v.Text { u = 0x7f7f; }
            if !wide::ReadWideFour(r, v) { print("REFUSE"); continue; }
            if wide::WriteWideFour(w, v) != SERIALIZE_ERROR_NONE { abort("driver: rewrite refused"); }
            serialize_flush(w);
            reply(r, v.Text[0..v.TextLength], w);
        } else {
            abort("driver: unexpected bound");
        }
    }
}

var failed = 0;
fn check(ok: bool, what: const u8[:]) {
    if !ok { print("FAILED: ", what); failed += 1; }
}

var wlen: i64 = 0;
fn fresh() -> WriteStream {
    for i in 1024 { wbuf[i] = 0; }
    return serialize_write_stream_init(wbuf);
}
fn done(w: WriteStream&) -> i64 {
    let bits = serialize_bits_processed(w);
    serialize_flush(w);
    wlen = serialize_bytes_processed(w);
    return bits;
}
fn reread() -> ReadStream { return serialize_read_stream_init_padded(rbuf, wbuf[0..wlen]); }

fn contracts() {
    // the writer refuses a length outside [0, N] and an interior null
    var v = wide::WideSeven { .. };
    let lengths: i32[3] = [-1, 8, 1];
    for n in lengths {
        v.TextLength = n;
        var w = fresh();
        check(wide::WriteWideSeven(w, v) == SERIALIZE_ERROR_VALUE_OUT_OF_RANGE, "writer refuses length/null misuse");
    }
    // an unpaired high surrogate is the reader's refusal
    v.Text[0] = 0xd800;
    {
        var w = fresh();
        check(wide::WriteWideSeven(w, v) == SERIALIZE_ERROR_NONE, "write an unpaired high");
        done(w);
        var r = reread();
        check(!wide::ReadWideSeven(r, v), "unpaired high accepted");
        check(serialize_error(r) == SERIALIZE_ERROR_VALUE_OUT_OF_RANGE, "unpaired high latches value_out_of_range");
    }
    // the unused tail is left as it was
    v.Text[0] = 0xffff;
    {
        var w = fresh();
        check(wide::WriteWideSeven(w, v) == SERIALIZE_ERROR_NONE, "write 0xffff");
        done(w);
        for &u in v.Text { u = 0x7f7f; }
        var r = reread();
        check(wide::ReadWideSeven(r, v), "read 0xffff");
        let want: u16[7] = [0xffff, 0x7f7f, 0x7f7f, 0x7f7f, 0x7f7f, 0x7f7f, 0x7f7f];
        check(v.Text == want, "unused tail changed");
    }
    // composition: a surrogate pair under a branch costs 1 + 3 + 2 * 32 bits
    var c = wideprobe::Conditional { .. };
    c.Enabled = true;
    c.Text[0] = 0xd800;
    c.Text[1] = 0xdc00;
    c.TextLength = 2;
    {
        var w = fresh();
        check(wideprobe::WriteConditional(w, c) == SERIALIZE_ERROR_NONE, "write conditional");
        let bits = done(w);
        check(bits == 68 && (wbuf[0] & 15) == 5, "aligned wide groups");
        var out = wideprobe::Conditional { .. };
        var r = reread();
        check(wideprobe::ReadConditional(r, out) && out == c, "conditional");
        c.Enabled = false;
        var w2 = fresh();
        check(wideprobe::WriteConditional(w2, c) == SERIALIZE_ERROR_NONE, "write conditional off");
        done(w2);
        var r2 = reread();
        var zero = wideprobe::Conditional { .. };
        wideprobe::ZeroConditional(zero);
        check(wideprobe::ReadConditional(r2, out) && out == zero, "branch not zeroed");
    }
    var text = wideprobe::Text { .. };
    text.Value[0] = 0xffff;
    text.ValueLength = 1;
    var choice = wideprobe::Choice { .. };
    choice.Type = wideprobe::ChoiceTypeText;
    choice.Text = text;
    {
        var w = fresh();
        check(wideprobe::WriteChoice(w, choice) == SERIALIZE_ERROR_NONE, "write choice");
        done(w);
        var co = wideprobe::Choice { .. };
        for attempt in 2 {
            co.Text.Value[3] = 0x7f7f;
            var r = reread();
            check(wideprobe::ReadChoice(r, co) && co.Text == text, "union not fresh");
        }
    }
    var bx = wideprobe::NewBox();
    check(bx.CountedCount == 1, "born count");
    bx.Items[0] = text;
    bx.Counted[0] = text;
    bx.Choice = choice;
    {
        var w = fresh();
        check(wideprobe::WriteBox(w, bx) == SERIALIZE_ERROR_NONE, "write box");
        done(w);
        var bo = wideprobe::NewBox();
        bo.Counted[1].Value[0] = 0x7f7f;
        var r = reread();
        check(wideprobe::ReadBox(r, bo), "read box");
        check(bo.Items == bx.Items && bo.Counted[0] == text && bo.CountedCount == 1 && bo.Counted[1].Value[0] == 0x7f7f && bo.Choice == choice,
              "array/union composition");
    }
    if failed != 0 { exit(1); }
    print("packet wide Goose contracts OK");
}

fn main() {
    let argv = args();
    if argv.len == 2 && argv[1] == "--contracts" {
        contracts();
        return;
    }
    drive();
}
