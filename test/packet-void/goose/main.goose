// The Goose leg of test/packet-void (SPEC §4.8): payload-free union arms cost
// exactly their tag bits. Answers every case of
// testdata/wire/packet-void/cases.txt, one "case <name> ok|FAILED: <why>"
// line each, for tools/casegate. The pins are the Go oracle's bytes, never
// this writer's. The make target generates Void.schema beside this file and
// runs it with `-- <testdata/wire/packet-void>`.
//
// An accept case is built by name, written (bytes and bit count against the
// pin and the bits column), read from the pin with the public one-packet read
// into dirtied storage (so a payload not constructed fresh at its declared
// defaults shows), compared field by field, and written again. A refuse
// case's pin must be refused by the public one-packet read.
import os;
import .Void;
import .serialize;
import .driverlib;

// one overload per type: write, the public one-packet read, dirtied storage
// and field equality (an unselected arm is not compared; a selected payload
// is compared with its whole backing storage)
fn wr(w: WriteStream&, v: packetvoid::Mixed&) -> i32 { return packetvoid::WriteMixed(w, v); }
fn wr(w: WriteStream&, v: packetvoid::Tags&) -> i32 { return packetvoid::WriteTags(w, v); }
fn wr(w: WriteStream&, v: packetvoid::Single&) -> i32 { return packetvoid::WriteSingle(w, v); }
fn wr(w: WriteStream&, v: packetvoid::Empty&) -> i32 { return packetvoid::WriteEmpty(w, v); }
fn wr(w: WriteStream&, v: packetvoid::Frame&) -> i32 { return packetvoid::WriteFrame(w, v); }
fn wr(w: WriteStream&, v: packetvoid::Batch&) -> i32 { return packetvoid::WriteBatch(w, v); }

fn rd(r: ReadStream&, v: packetvoid::Mixed&) -> bool { return packetvoid::ReadMixed(r, v); }
fn rd(r: ReadStream&, v: packetvoid::Tags&) -> bool { return packetvoid::ReadTags(r, v); }
fn rd(r: ReadStream&, v: packetvoid::Single&) -> bool { return packetvoid::ReadSingle(r, v); }
fn rd(r: ReadStream&, v: packetvoid::Empty&) -> bool { return packetvoid::ReadEmpty(r, v); }
fn rd(r: ReadStream&, v: packetvoid::Frame&) -> bool { return packetvoid::ReadFrame(r, v); }
fn rd(r: ReadStream&, v: packetvoid::Batch&) -> bool { return packetvoid::ReadBatch(r, v); }

fn eq(a: packetvoid::DefaultArm&, b: packetvoid::DefaultArm&) -> bool {
    return a.EntriesCount == b.EntriesCount && a.Marker == b.Marker && a.Entries == b.Entries;
}
fn eq(a: packetvoid::Mixed&, b: packetvoid::Mixed&) -> bool {
    if a.Type != b.Type { return false; }
    if a.Type == packetvoid::MixedTypePayload { return eq(a.Payload, b.Payload); }
    return true;
}
fn eq(a: packetvoid::Tags&, b: packetvoid::Tags&) -> bool { return a.Type == b.Type; }
fn eq(a: packetvoid::Single&, b: packetvoid::Single&) -> bool { return a.Type == b.Type; }
fn eq(a: packetvoid::Empty&, b: packetvoid::Empty&) -> bool { return a.Type == b.Type; }
fn eq(a: packetvoid::Frame&, b: packetvoid::Frame&) -> bool {
    return a.Lead == b.Lead && a.Tail == b.Tail && eq(a.Choice, b.Choice);
}
fn eq(a: packetvoid::Batch&, b: packetvoid::Batch&) -> bool {
    if a.ValuesCount != b.ValuesCount { return false; }
    for i in a.ValuesCount {
        if !eq(a.Values[i], b.Values[i]) { return false; }
    }
    return true;
}

fn mixed(t: packetvoid::MixedType) -> packetvoid::Mixed {
    var m = packetvoid::NewMixed();
    m.Type = t;
    return m;
}

fn dirtyMixed() -> packetvoid::Mixed {
    var m = packetvoid::NewMixed();
    m.Type = packetvoid::MixedTypePayload;
    m.Payload.EntriesCount = 2;
    m.Payload.Entries[0].Retries = 9;
    m.Payload.Entries[1].Retries = 9;
    m.Payload.Marker = 1;
    return m;
}
fn dirtyTags() -> packetvoid::Tags {
    var t = packetvoid::NewTags();
    t.Type = packetvoid::TagsTypePong;
    return t;
}
fn dirtySingle() -> packetvoid::Single {
    var s = packetvoid::NewSingle();
    s.Type = packetvoid::SingleTypeAck;
    return s;
}
fn dirtyFrame() -> packetvoid::Frame {
    var f = packetvoid::NewFrame();
    f.Lead = 7;
    f.Choice = dirtyMixed();
    f.Tail = 1;
    return f;
}
fn dirtyBatch() -> packetvoid::Batch {
    var b = packetvoid::NewBatch();
    b.ValuesCount = 2;
    b.Values[0] = dirtyMixed();
    b.Values[1] = dirtyMixed();
    return b;
}

var wbuf: u8[4096] = [0; 4096];
var wlen: i64 = 0;
var wbits: i64 = 0;
var rbuf: u8[4104] = [0; 4104];

fn same(a: const u8[:], b: const u8[:]) -> bool {
    if a.len != b.len { return false; }
    for i in a.len { if a[i] != b[i] { return false; } }
    return true;
}

fn hexs(b: const u8[:]) -> u8[>..] {
    var out: u8[>..] = [];
    hex_append(out, b);
    return out;
}

// writeWire writes value into wbuf: its refusal, with wlen and wbits set
fn writeWire<T>(value: T&) -> i32 {
    for i in 4096 { wbuf[i] = 0; }
    var w = serialize_write_stream_init(wbuf);
    let refusal = wr(w, value);
    wbits = serialize_bits_processed(w);
    serialize_flush(w);
    wlen = serialize_bytes_processed(w);
    return refusal;
}

fn accept<T>(pin: const u8[:], bits: i64, value: T&, dirty: T) -> u8[>..] {
    let refusal = writeWire(value);
    if refusal != SERIALIZE_ERROR_NONE { return str("write refused: ", serialize_refusal_name(refusal)); }
    if !same(wbuf[0..wlen], pin) { return str("write ", hexs(wbuf[0..wlen]), ", pin ", hexs(pin)); }
    if wbits != bits { return str("write has ", wbits, " bits, want ", bits); }
    var back = dirty;
    var r = serialize_read_stream_init_padded(rbuf, pin);
    if !rd(r, back) { return str("read refused: ", serialize_refusal_name(serialize_error(r))); }
    // the public read ends the packet, so it consumes the final byte's padding
    if serialize_bits_processed(r) != pin.len * 8 {
        return str("read consumed ", serialize_bits_processed(r), " bits of ", pin.len, " bytes");
    }
    if !eq(back, value) { return str("read value differs"); }
    let again = writeWire(back);
    if again != SERIALIZE_ERROR_NONE { return str("write what was read refused: ", serialize_refusal_name(again)); }
    if !same(wbuf[0..wlen], pin) { return str("write what was read: ", hexs(wbuf[0..wlen]), ", pin ", hexs(pin)); }
    return str("");
}

// acceptCase builds each accept case's value by name (its value column); an
// accept name it does not know is FAILED, never some other case's value
fn acceptCase(name: const u8[:], typ: const u8[:], pin: const u8[:], bits: i64) -> u8[>..] {
    if typ == "Mixed" {
        if name == "mixed-none" { var v = mixed(packetvoid::MixedTypeNone); return accept(pin, bits, v, dirtyMixed()); }
        if name == "mixed-ping" { var v = mixed(packetvoid::MixedTypePing); return accept(pin, bits, v, dirtyMixed()); }
        // the payload arm is the type's declared defaults: entries empty, marker 5
        if name == "mixed-payload" { var v = mixed(packetvoid::MixedTypePayload); return accept(pin, bits, v, dirtyMixed()); }
        return str("unknown accept case");
    }
    if typ == "Tags" {
        var v = packetvoid::NewTags();
        if name == "tags-none" { return accept(pin, bits, v, dirtyTags()); }
        if name == "tags-ping" { v.Type = packetvoid::TagsTypePing; return accept(pin, bits, v, dirtyTags()); }
        if name == "tags-pong" { v.Type = packetvoid::TagsTypePong; return accept(pin, bits, v, dirtyTags()); }
        return str("unknown accept case");
    }
    if typ == "Single" {
        var v = packetvoid::NewSingle();
        if name == "single-none" { return accept(pin, bits, v, dirtySingle()); }
        if name == "single-ack" { v.Type = packetvoid::SingleTypeAck; return accept(pin, bits, v, dirtySingle()); }
        return str("unknown accept case");
    }
    if typ == "Empty" {
        var v = packetvoid::NewEmpty();
        if name == "empty-none" { return accept(pin, bits, v, packetvoid::NewEmpty()); }
        return str("unknown accept case");
    }
    if typ == "Frame" {
        if name == "frame-ping" {
            var v = packetvoid::NewFrame();
            v.Lead = 5;
            v.Choice = mixed(packetvoid::MixedTypePing);
            v.Tail = 6;
            return accept(pin, bits, v, dirtyFrame());
        }
        return str("unknown accept case");
    }
    if typ == "Batch" {
        var v = packetvoid::NewBatch();
        if name == "batch-empty" { return accept(pin, bits, v, dirtyBatch()); }
        if name == "batch-ping" {
            v.ValuesCount = 1;
            v.Values[0] = mixed(packetvoid::MixedTypePing);
            return accept(pin, bits, v, dirtyBatch());
        }
        if name == "batch-ping-payload" {
            v.ValuesCount = 2;
            v.Values[0] = mixed(packetvoid::MixedTypePing);
            v.Values[1] = mixed(packetvoid::MixedTypePayload);
            return accept(pin, bits, v, dirtyBatch());
        }
        return str("unknown accept case");
    }
    return str("unknown type ", typ);
}

// refuseCase: the public one-packet read of the pin must refuse
fn refuseCase(typ: const u8[:], pin: const u8[:]) -> u8[>..] {
    var r = serialize_read_stream_init_padded(rbuf, pin);
    var accepted = false;
    if typ == "Mixed" { var v = dirtyMixed(); accepted = packetvoid::ReadMixed(r, v); }
    else if typ == "Tags" { var v = packetvoid::NewTags(); accepted = packetvoid::ReadTags(r, v); }
    else if typ == "Single" { var v = packetvoid::NewSingle(); accepted = packetvoid::ReadSingle(r, v); }
    else if typ == "Empty" { var v = packetvoid::NewEmpty(); accepted = packetvoid::ReadEmpty(r, v); }
    else if typ == "Frame" { var v = packetvoid::NewFrame(); accepted = packetvoid::ReadFrame(r, v); }
    else if typ == "Batch" { var v = packetvoid::NewBatch(); accepted = packetvoid::ReadBatch(r, v); }
    else { return str("unknown type ", typ); }
    if accepted { return str("read accepted"); }
    return str("");
}

fn parseBits(text: const u8[:]) -> i64, bool {
    var n: i64 = 0;
    if text.len == 0 { return 0, false; }
    for c in text {
        if c >= '0' && c <= '9' { n = n * 10 + (c - '0'); }
        else { return 0, false; }
    }
    return n, true;
}

fn answer(dir: const u8[:], name: const u8[:], typ: const u8[:], verdict: const u8[:], bitsText: const u8[:]) -> u8[>..] {
    var pin: u8[>..] = [];
    if !read_file(str(dir, "/", name, ".bin"), pin) { return str("read the pin ", name, ".bin"); }
    if verdict == "refuse" { return refuseCase(typ, pin[0..pin.len]); }
    if verdict != "accept" { return str("verdict ", verdict); }
    let bits, ok = parseBits(bitsText);
    if !ok { return str("bits column ", bitsText); }
    return acceptCase(name, typ, pin[0..pin.len], bits);
}

fn isSpace(c: u8) -> bool { return c == ' ' || c == '\t' || c == '\r'; }

// field is the k-th whitespace-separated field of line, empty past the last
fn field(line: const u8[:], k: i64) -> const u8[:] {
    var i: i64 = 0;
    var n: i64 = 0;
    while i < line.len {
        while i < line.len && isSpace(line[i]) { i += 1; }
        if i == line.len { break; }
        let start = i;
        while i < line.len && !isSpace(line[i]) { i += 1; }
        if n == k { return line[start..i]; }
        n += 1;
    }
    return line[0..0];
}

fn caseLine(dir: const u8[:], line: const u8[:]) {
    let name = field(line, 0);
    if name.len == 0 || name[0] == '#' { return; }
    let bitsText = field(line, 3);
    if bitsText.len == 0 {
        print("case ", name, " FAILED: a case line needs <name> <type> <verdict> <bits>");
        return;
    }
    let why = answer(dir, name, field(line, 1), field(line, 2), bitsText);
    if why.len == 0 { print("case ", name, " ok"); }
    else { print("case ", name, " FAILED: ", why); }
}

fn main() {
    let argv = args();
    if argv.len != 2 {
        write_stderr("usage: main.goose -- <testdata/wire/packet-void>\n");
        exit(2);
    }
    let dir = argv[1];
    var text: u8[>..] = [];
    if !read_file(str(dir, "/cases.txt"), text) {
        write_stderr(str("read ", dir, "/cases.txt\n"));
        exit(2);
    }
    var start: i64 = 0;
    var i: i64 = 0;
    while i <= text.len {
        if i == text.len || text[i] == '\n' {
            caseLine(dir, text[start..i]);
            start = i + 1;
        }
        i += 1;
    }
}
