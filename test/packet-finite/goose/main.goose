// Verify SPEC §4.3's finite float rule on the Goose packet target, against
// serialize.goose v2.1.1: test/packet-finite/go/main.go's vectors. Every
// shape of test/packet-finite/Finite.schema must refuse a NaN or an infinity
// on write and on read as non_finite_float, and carry negative zero, the
// subnormals and the largest finite value bit exact.
//
// Each failing vector prints one line, "FAILED: <vector>: <detail>"; the
// negative controls compare the set of failing names with the .expected
// lists beside this file. The make target generates Finite.schema beside it.
import os;
import .Finite;
import .serialize;

var failed = false;
var vectors = 0;

fn fail(name: const u8[:], detail: const u8[:]) {
    write_stderr(str("FAILED: ", name, ": ", detail, "\n"));
    failed = true;
}

fn wr(w: WriteStream&, v: finite::Flat&) -> i32 { return finite::WriteFlat(w, v); }
fn wr(w: WriteStream&, v: finite::Single32&) -> i32 { return finite::WriteSingle32(w, v); }
fn wr(w: WriteStream&, v: finite::Single64&) -> i32 { return finite::WriteSingle64(w, v); }
fn rd(r: ReadStream&, v: finite::Flat&) -> bool { return finite::ReadFlat(r, v); }
fn rd(r: ReadStream&, v: finite::Single32&) -> bool { return finite::ReadSingle32(r, v); }
fn rd(r: ReadStream&, v: finite::Single64&) -> bool { return finite::ReadSingle64(r, v); }

var wbuf: u8[64] = [0; 64];
var wlen: i64 = 0;
var raw: u8[64] = [0; 64];
var rawLen: i64 = 0;
var rbuf: u8[72] = [0; 72];

// rawWire lays the given patterns out the way the generated code does,
// bypassing every float entry point, so a reader can be handed a pattern no
// conforming writer emits.
fn flatWire(a: u32, b: u64) {
    for i in 64 { raw[i] = 0; }
    var w = serialize_write_stream_init(raw);
    serialize_bits(w, a, 32);
    serialize_bits64(w, b, 64);
    serialize_bool(w, true);
    serialize_flush(w);
    rawLen = serialize_bytes_processed(w);
}
fn single32Wire(x: u32) {
    for i in 64 { raw[i] = 0; }
    var w = serialize_write_stream_init(raw);
    serialize_bits(w, x, 32);
    serialize_flush(w);
    rawLen = serialize_bytes_processed(w);
}
fn single64Wire(x: u64) {
    for i in 64 { raw[i] = 0; }
    var w = serialize_write_stream_init(raw);
    serialize_bits64(w, x, 64);
    serialize_flush(w);
    rawLen = serialize_bytes_processed(w);
}

fn write<T>(v: T&) -> i32 {
    for i in 64 { wbuf[i] = 0; }
    var w = serialize_write_stream_init(wbuf);
    let refusal = wr(w, v);
    serialize_flush(w);
    wlen = serialize_bytes_processed(w);
    return refusal;
}

fn checkWriteRefused<T>(name: const u8[:], v: T&) {
    let refusal = write(v);
    if refusal == SERIALIZE_ERROR_NONE { fail(name, "write accepted a non-finite float"); return; }
    if refusal != SERIALIZE_ERROR_NON_FINITE_FLOAT { fail(name, str("write refused with ", serialize_refusal_name(refusal), ", want non_finite_float")); }
}

fn checkReadRefused<T>(name: const u8[:], v: T&) {
    var r = serialize_read_stream_init_padded(rbuf, raw[0..rawLen]);
    if rd(r, v) { fail(name, "read accepted a non-finite pattern"); return; }
    if serialize_error(r) != SERIALIZE_ERROR_NON_FINITE_FLOAT {
        fail(name, str("read refused with ", serialize_refusal_name(serialize_error(r)), ", want non_finite_float"));
    }
}

fn same(a: const u8[:], b: const u8[:]) -> bool {
    if a.len != b.len { return false; }
    for i in a.len { if a[i] != b[i] { return false; } }
    return true;
}

// checkRoundTrip writes v, requires exactly the raw pattern's wire, and
// reads it back into out.
fn checkRoundTrip<T>(name: const u8[:], v: T&, out: T&) -> bool {
    let refusal = write(v);
    if refusal != SERIALIZE_ERROR_NONE { fail(name, str("write refused a finite value: ", serialize_refusal_name(refusal))); return false; }
    if !same(wbuf[0..wlen], raw[0..rawLen]) { fail(name, "the pattern did not ride bit exact"); return false; }
    var r = serialize_read_stream_init_padded(rbuf, wbuf[0..wlen]);
    if !rd(r, out) { fail(name, str("read refused a finite pattern: ", serialize_refusal_name(serialize_error(r)))); return false; }
    return true;
}

fn main() {
    let names: const u8[:][6] = ["quiet-nan", "signalling-nan", "negative-nan", "payload-nan", "positive-inf", "negative-inf"];
    let nan32: u32[6] = [0x7fc00000, 0x7f800001, 0xffc00000, 0x7fc0dead, 0x7f800000, 0xff800000];
    let nan64: u64[6] = [0x7ff8000000000000, 0x7ff0000000000001, 0xfff8000000000000, 0x7ff800000000dead, 0x7ff0000000000000, 0xfff0000000000000];
    let finNames: const u8[:][6] = ["negative-zero", "min-subnormal", "max-subnormal", "negative-subnormal", "max-finite", "negative-max-finite"];
    let fin32: u32[6] = [0x80000000, 0x00000001, 0x007fffff, 0x80000001, 0x7f7fffff, 0xff7fffff];
    let fin64: u64[6] = [0x8000000000000000, 0x0000000000000001, 0x000fffffffffffff, 0x8000000000000001, 0x7fefffffffffffff, 0xffefffffffffffff];

    for i in 6 {
        let x = serialize_f32_from_bits(nan32[i]);
        var flat = finite::Flat { A: x, .. };
        checkWriteRefused(str("flat-f32/write/", names[i]), flat);
        var single = finite::Single32 { X: x };
        checkWriteRefused(str("single32/write/", names[i]), single);
        flatWire(nan32[i], 0);
        var f2 = finite::Flat { .. };
        checkReadRefused(str("flat-f32/read/", names[i]), f2);
        single32Wire(nan32[i]);
        var s2 = finite::Single32 { .. };
        checkReadRefused(str("single32/read/", names[i]), s2);
        vectors += 4;
    }
    for i in 6 {
        let x = serialize_f64_from_bits(nan64[i]);
        var flat = finite::Flat { B: x, .. };
        checkWriteRefused(str("flat-f64/write/", names[i]), flat);
        var single = finite::Single64 { X: x };
        checkWriteRefused(str("single64/write/", names[i]), single);
        flatWire(0, nan64[i]);
        var f2 = finite::Flat { .. };
        checkReadRefused(str("flat-f64/read/", names[i]), f2);
        single64Wire(nan64[i]);
        var s2 = finite::Single64 { .. };
        checkReadRefused(str("single64/read/", names[i]), s2);
        vectors += 4;
    }
    for i in 6 {
        let x = serialize_f32_from_bits(fin32[i]);
        flatWire(fin32[i], 0);
        var flat = finite::Flat { A: x, C: true, .. };
        var fo = finite::Flat { .. };
        let n1 = str("flat-f32/roundtrip/", finNames[i]);
        if checkRoundTrip(n1, flat, fo) && !(serialize_f32_bits(fo.A) == fin32[i] && serialize_f64_bits(fo.B) == 0 && fo.C) {
            fail(n1, "read back a value other than the pattern written");
        }
        single32Wire(fin32[i]);
        var single = finite::Single32 { X: x };
        var so = finite::Single32 { .. };
        let n2 = str("single32/roundtrip/", finNames[i]);
        if checkRoundTrip(n2, single, so) && serialize_f32_bits(so.X) != fin32[i] {
            fail(n2, "read back a value other than the pattern written");
        }
        vectors += 2;
    }
    for i in 6 {
        let x = serialize_f64_from_bits(fin64[i]);
        flatWire(0, fin64[i]);
        var flat = finite::Flat { B: x, C: true, .. };
        var fo = finite::Flat { .. };
        let n1 = str("flat-f64/roundtrip/", finNames[i]);
        if checkRoundTrip(n1, flat, fo) && !(serialize_f64_bits(fo.B) == fin64[i] && serialize_f32_bits(fo.A) == 0 && fo.C) {
            fail(n1, "read back a value other than the pattern written");
        }
        single64Wire(fin64[i]);
        var single = finite::Single64 { X: x };
        var so = finite::Single64 { .. };
        let n2 = str("single64/roundtrip/", finNames[i]);
        if checkRoundTrip(n2, single, so) && serialize_f64_bits(so.X) != fin64[i] {
            fail(n2, "read back a value other than the pattern written");
        }
        vectors += 2;
    }
    if failed { exit(1); }
    print("packet finite floats Goose: ", vectors, " vectors OK");
}
