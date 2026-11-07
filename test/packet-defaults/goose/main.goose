// Verify constructor storage and the eight C++ packet goldens on the Goose
// packet target: test/packet-defaults/go/main.go block for block. Reads also
// exercise reused storage: field prefixes overlay, absent branches clear to
// the zero form, and every union selection constructs its payload with its
// declared defaults. The make target generates Defaults.schema
// (packetdefaults) and Plain.schema (packetplain) beside this file and runs
// it with `-- <golden-directory>`.
import os;
import .Defaults;
import .Plain;
import .serialize;

var failed = false;

fn check(ok: bool, name: const u8[:]) {
    if !ok {
        write_stderr(str("FAILED: ", name, "\n"));
        failed = true;
    }
}

// fixture values, independent of the generated constructors; schema
// literals have no escapes, so the token is backslash-n, backslash-t
fn expectedSample() -> packetdefaults::Sample {
    var v = packetdefaults::Sample { .. };
    packetdefaults::ZeroSample(v);
    v.Name = [0xc3, 0xa9, 0xf0, 0x90, 0x80, 0x80];
    v.NameLength = 6;
    v.Token = [0x5c, 0x6e, 0x5c, 0x74];
    v.TokenLength = 4;
    v.Caps = 5;
    return v;
}

fn shortSample() -> packetdefaults::Sample {
    var v = packetdefaults::Sample { .. };
    packetdefaults::ZeroSample(v);
    v.Name[0] = 'A';
    v.NameLength = 1;
    v.Token[1] = 0xff;
    v.TokenLength = 2;
    v.Caps = 2;
    return v;
}

fn dirtySample() -> packetdefaults::Sample {
    var v = packetdefaults::Sample { .. };
    v.Name = ['d', 'i', 'r', 't', 'y', '!'];
    v.NameLength = 6;
    v.Token = [0xa1, 0xa2, 0xa3, 0xa4];
    v.TokenLength = 4;
    v.Caps = 7;
    v.EmptyName = ['o', 'l', 'd'];
    v.EmptyNameLength = 3;
    v.EmptyToken = [0xb1, 0xb2];
    v.EmptyTokenLength = 2;
    v.EmptyCaps = 7;
    return v;
}

fn zeroSample() -> packetdefaults::Sample {
    var v = packetdefaults::Sample { .. };
    packetdefaults::ZeroSample(v);
    return v;
}

// one write and one read overload per type, so the checks below are generic
fn wr(w: WriteStream&, v: packetdefaults::Sample&) -> i32 { return packetdefaults::WriteSample(w, v); }
fn rd(r: ReadStream&, v: packetdefaults::Sample&) -> bool { return packetdefaults::ReadSample(r, v); }
fn wr(w: WriteStream&, v: packetdefaults::Batch&) -> i32 { return packetdefaults::WriteBatch(w, v); }
fn rd(r: ReadStream&, v: packetdefaults::Batch&) -> bool { return packetdefaults::ReadBatch(r, v); }
fn wr(w: WriteStream&, v: packetdefaults::ZeroCount&) -> i32 { return packetdefaults::WriteZeroCount(w, v); }
fn rd(r: ReadStream&, v: packetdefaults::ZeroCount&) -> bool { return packetdefaults::ReadZeroCount(r, v); }
fn wr(w: WriteStream&, v: packetdefaults::Conditional&) -> i32 { return packetdefaults::WriteConditional(w, v); }
fn rd(r: ReadStream&, v: packetdefaults::Conditional&) -> bool { return packetdefaults::ReadConditional(r, v); }
fn wr(w: WriteStream&, v: packetdefaults::Choice&) -> i32 { return packetdefaults::WriteChoice(w, v); }
fn rd(r: ReadStream&, v: packetdefaults::Choice&) -> bool { return packetdefaults::ReadChoice(r, v); }
fn wr(w: WriteStream&, v: packetdefaults::WideMask&) -> i32 { return packetdefaults::WriteWideMask(w, v); }
fn rd(r: ReadStream&, v: packetdefaults::WideMask&) -> bool { return packetdefaults::ReadWideMask(r, v); }
fn wr(w: WriteStream&, v: packetplain::WideMask&) -> i32 { return packetplain::WriteWideMask(w, v); }

var wbuf: u8[4096] = [0; 4096];
var wlen: i64 = 0;
var wbits: i64 = 0;
var rbuf: u8[4104] = [0; 4104];

fn writeWire<T>(name: const u8[:], value: T&) -> bool {
    for i in 4096 { wbuf[i] = 0; }
    var w = serialize_write_stream_init(wbuf);
    let refusal = wr(w, value);
    if refusal != SERIALIZE_ERROR_NONE {
        check(false, str(name, ": write: ", serialize_refusal_name(refusal)));
        return false;
    }
    wbits = serialize_bits_processed(w);
    serialize_flush(w);
    wlen = serialize_bytes_processed(w);
    return true;
}

fn same(a: const u8[:], b: const u8[:]) -> bool {
    if a.len != b.len { return false; }
    for i in a.len { if a[i] != b[i] { return false; } }
    return true;
}

fn readWire<T>(name: const u8[:], wire: const u8[:], bits: i64, value: T&, want: T&) {
    var r = serialize_read_stream_init_padded(rbuf, wire);
    if !rd(r, value) {
        check(false, str(name, ": read: ", serialize_refusal_name(serialize_error(r))));
        return;
    }
    check(serialize_bits_processed(r) == bits, str(name, ": read consumed bits"));
    check(value == want, str(name, ": read values and backing storage"));
}

fn parseBits(text: const u8[:]) -> i64, bool {
    var n: i64 = 0;
    var any = false;
    for c in text {
        if c >= '0' && c <= '9' { n = n * 10 + (c - '0'); any = true; }
        else if c != '\n' && c != ' ' && c != '\r' { return 0, false; }
    }
    return n, any;
}

fn golden<T>(dir: const u8[:], name: const u8[:], value: T&, initial: T, want: T&) {
    if !writeWire(name, value) { return; }
    var expected: u8[>..] = [];
    if !read_file(str(dir, "/", name, ".bin"), expected) { check(false, str(name, ": read C++ byte golden")); return; }
    var bitText: u8[>..] = [];
    if !read_file(str(dir, "/", name, ".bits"), bitText) { check(false, str(name, ": read C++ bit golden")); return; }
    let expectedBits, parsed = parseBits(bitText);
    if !parsed { check(false, str(name, ": parse C++ bit golden")); return; }
    check(same(wbuf[0..wlen], expected), str(name, ": C++ golden bytes"));
    check(wbits == expectedBits, str(name, ": C++ golden bits"));
    // decode the oracle's bytes, not this writer's output
    var v = initial;
    readWire(name, expected, expectedBits, v, want);
}

fn roundTrip<T>(name: const u8[:], value: T&, reused: T&, want: T&) {
    if !writeWire(name, value) { return; }
    var copy: u8[4096] = [0; 4096];
    for i in wlen { copy[i] = wbuf[i]; }
    readWire(name, copy[0..wlen], wbits, reused, want);
}

fn run(dir: const u8[:]) {
    let sample = packetdefaults::NewSample();
    let expected = expectedSample();
    // keep this stable: the named negative control sabotages constructor bytes
    check(sample == expected, "packet-default constructor bytes");
    check(sample.NameLength == 6 && sample.TokenLength == 4, "constructor byte lengths");
    check(sample.Caps == 5 && sample.EmptyCaps == 0, "constructor nonzero and empty flags masks");
    let e3: u8[3] = [0, 0, 0];
    let e2: u8[2] = [0, 0];
    check(sample.EmptyNameLength == 0 && sample.EmptyName == e3 && sample.EmptyTokenLength == 0 && sample.EmptyToken == e2,
          "constructor explicit empty byte storage");
    let zero = zeroSample();
    check(!(zero == sample), "the zero form remains distinct from specified defaults");
    let plain = packetplain::NewSample();
    let e6: u8[6] = [0, 0, 0, 0, 0, 0];
    let e4: u8[4] = [0, 0, 0, 0];
    check(plain.Name == e6 && plain.NameLength == 0 && plain.Token == e4 && plain.TokenLength == 0 && plain.Caps == 0 &&
          plain.EmptyName == e3 && plain.EmptyNameLength == 0 && plain.EmptyToken == e2 && plain.EmptyTokenLength == 0 && plain.EmptyCaps == 0,
          "defaultless Sample has zero storage");
    var emptyZero = packetdefaults::NewEmptyOnly();
    packetdefaults::ZeroEmptyOnly(emptyZero);
    check(packetdefaults::NewEmptyOnly() == emptyZero, "explicit-empty-only constructor exists and returns zero");
    let prefix = packetdefaults::NewPrefix();
    let p5n: u8[5] = [0xc3, 0xa9, 0, 0, 0];
    let p5t: u8[5] = [0x5c, 0x6e, 0, 0, 0];
    check(prefix.Name == p5n && prefix.NameLength == 2 && prefix.Token == p5t && prefix.TokenLength == 2,
          "constructor short literals have exact bytes, lengths and zero backing tails");
    var wide = packetdefaults::NewWideMask();
    check(wide.High == 0x8000000000000000 && wide.All == 0xffffffffffffffff, "constructor bit63 and all64 flags masks");
    if writeWire("wide flags defaults", wide) {
        var wideWire: u8[16] = [0; 16];
        for i in wlen { wideWire[i] = wbuf[i]; }
        let wideBits = wbits;
        let wideLen = wlen;
        var plainWide = packetplain::NewWideMask();
        plainWide.High = 0x8000000000000000;
        plainWide.All = 0xffffffffffffffff;
        if writeWire("wide flags explicit plain values", plainWide) {
            check(wideBits == 128 && wbits == wideBits && same(wideWire[0..wideLen], wbuf[0..wlen]),
                  "wide flags defaults retain the defaultless 128-bit packet wire");
            var back = packetdefaults::NewWideMask();
            packetdefaults::ZeroWideMask(back);
            readWire("wide flags defaults", wideWire[0..wideLen], wideBits, back, wide);
        }
    }

    var s = sample;
    golden(dir, "sample-defaults", s, zero, expected);

    var zeroCount = packetdefaults::NewZeroCount();
    check(zeroCount.ItemsCount == 0 && zeroCount.Items[0] == expected && zeroCount.Items[1] == expected, "zero-count backing defaults");
    var zeroCountInitial = packetdefaults::NewZeroCount();
    zeroCountInitial.Items[0] = dirtySample();
    zeroCountInitial.Items[1] = shortSample();
    zeroCountInitial.ItemsCount = 2;
    var zeroCountWant = zeroCountInitial;
    zeroCountWant.ItemsCount = 0;
    golden(dir, "zero-count", zeroCount, zeroCountInitial, zeroCountWant);

    var batch = packetdefaults::NewBatch();
    check(batch.Head == expected, "nested constructor defaults");
    for item in batch.Items { check(item == expected, "fixed array constructor defaults"); }
    for item in batch.Counted { check(item == expected, "counted backing constructor defaults"); }
    check(batch.CountedCount == 1, "counted constructor BornCount 1");
    let plainBatch = packetplain::NewBatch();
    let plainZero = packetplain::NewSample();
    check(plainBatch.CountedCount == 1 && plainBatch.Head == plainZero && plainBatch.Items[0] == plainZero &&
          plainBatch.Items[1] == plainZero && plainBatch.Counted[2] == plainZero, "defaultless constructor retains BornCount and zero elements");
    var batchInitial = packetdefaults::NewBatch();
    batchInitial.Counted[1] = dirtySample();
    batchInitial.Counted[2] = shortSample();
    var batchWant = batchInitial;
    batchWant.Counted[0] = expected;
    golden(dir, "batch-defaults", batch, batchInitial, batchWant);

    var conditional = packetdefaults::NewConditional();
    check(conditional.Enabled && conditional.Value == expected, "conditional constructor defaults");
    var condZero = packetdefaults::NewConditional();
    packetdefaults::ZeroConditional(condZero);
    var condOnWant = condZero;
    condOnWant.Enabled = true;
    condOnWant.Value = expected;
    golden(dir, "conditional-on", conditional, condZero, condOnWant);
    conditional.Enabled = false;
    var condDirty = condZero;
    condDirty.Enabled = true;
    condDirty.Value = dirtySample();
    golden(dir, "conditional-off", conditional, condDirty, condZero);

    var choice = packetdefaults::NewChoice();
    let plainChoice = packetplain::NewChoice();
    check(choice.Type == packetdefaults::ChoiceTypeNone && plainChoice.Type == packetplain::ChoiceTypeNone, "union zero tag is None");
    choice.Type = packetdefaults::ChoiceTypeSample;
    choice.Sample = sample;
    var choiceInitial = packetdefaults::NewChoice();
    choiceInitial.Type = packetdefaults::ChoiceTypeConditional;
    choiceInitial.Sample = dirtySample();
    choiceInitial.Conditional.Enabled = true;
    choiceInitial.Conditional.Value = dirtySample();
    var choiceWant = choiceInitial;
    choiceWant.Type = packetdefaults::ChoiceTypeSample;
    choiceWant.Sample = expected;
    golden(dir, "choice-sample", choice, choiceInitial, choiceWant);

    // keep defaults in the full name/token buffers; dirty the explicitly
    // empty fields, so their zero-length wire values must keep those tails
    var overlay = packetdefaults::NewSample();
    overlay.EmptyName = ['o', 'l', 'd'];
    overlay.EmptyNameLength = 3;
    overlay.EmptyToken = [0xb1, 0xb2];
    overlay.EmptyTokenLength = 2;
    overlay.EmptyCaps = 7;
    var shortWant = overlay;
    shortWant.Name[0] = 'A';
    shortWant.NameLength = 1;
    shortWant.Token[0] = 0;
    shortWant.Token[1] = 0xff;
    shortWant.TokenLength = 2;
    shortWant.Caps = 2;
    shortWant.EmptyNameLength = 0;
    shortWant.EmptyTokenLength = 0;
    shortWant.EmptyCaps = 0;
    var short = shortSample();
    golden(dir, "sample-short", short, overlay, shortWant);
    var emptyWant = overlay;
    emptyWant.NameLength = 0;
    emptyWant.TokenLength = 0;
    emptyWant.Caps = 0;
    emptyWant.EmptyNameLength = 0;
    emptyWant.EmptyTokenLength = 0;
    emptyWant.EmptyCaps = 0;
    var empty = zeroSample();
    golden(dir, "sample-empty", empty, overlay, emptyWant);

    // every selection starts from the declared defaults, even with the same
    // tag; received prefixes overlay those fresh buffers, no poison
    // survives, and the unselected arm keeps its storage
    var selectedShort = expectedSample();
    selectedShort.Name[0] = 'A';
    selectedShort.NameLength = 1;
    selectedShort.Token[0] = 0;
    selectedShort.Token[1] = 0xff;
    selectedShort.TokenLength = 2;
    selectedShort.Caps = 2;
    var selectedEmpty = expectedSample();
    selectedEmpty.NameLength = 0;
    selectedEmpty.TokenLength = 0;
    selectedEmpty.Caps = 0;
    for k in 2 {
        choice.Sample = if k == 0 { shortSample() } else { zeroSample() };
        choiceWant.Sample = if k == 0 { selectedShort } else { selectedEmpty };
        var reused = choiceInitial;
        for attempt in 2 {
            reused.Sample = dirtySample();
            roundTrip(str("choice fresh declared-default tails, case ", k, " selection ", attempt), choice, reused, choiceWant);
        }
    }

    // selection first constructs Conditional (Enabled = true and Sample's
    // defaults); the received false gate then applies the zero rule
    choice.Type = packetdefaults::ChoiceTypeConditional;
    choice.Conditional = condZero;
    choice.Conditional.Value = expected;
    var reused = choiceInitial;
    reused.Type = packetdefaults::ChoiceTypeSample;
    choiceWant = reused;
    choiceWant.Type = packetdefaults::ChoiceTypeConditional;
    choiceWant.Conditional = condZero;
    for attempt in 2 {
        reused.Conditional = condZero;
        reused.Conditional.Enabled = true;
        reused.Conditional.Value = dirtySample();
        roundTrip(str("choice-conditional-off zero overrides construction defaults, selection ", attempt), choice, reused, choiceWant);
    }
}

fn main() {
    let argv = args();
    if argv.len != 2 {
        write_stderr("usage: main.goose -- <golden-directory>\n");
        exit(2);
    }
    run(argv[1]);
    if failed { exit(1); }
    print("packet defaults Goose: constructors, eight C++ goldens and reused storage OK");
}
