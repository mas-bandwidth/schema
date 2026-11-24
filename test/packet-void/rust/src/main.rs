// The Rust leg of test/packet-void (SPEC §4.8): answers every case of
// testdata/wire/packet-void/cases.txt, one "case <name> ok|FAILED: <why>"
// line each, for tools/casegate. The pins are the Go oracle's bytes, never
// this writer's. A panic (a debug_assert! in the generated writer, a slice
// out of range) is caught per case, so every case still gets its line.

use packetvoid as v;
use serialize::{ReadStream, Stream, WriteStream};
use std::panic::{AssertUnwindSafe, catch_unwind};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Value {
    Mixed(v::Mixed),
    Tags(v::Tags),
    Single(v::Single),
    Empty(v::Empty),
    Frame(v::Frame),
    Batch(v::Batch),
}

fn payload() -> v::Mixed {
    v::Mixed::Payload(v::DefaultArm::new())
}

// The value of each accept case, by name; None for a name this driver does
// not know, which is FAILED and never written with another case's value.
fn build(name: &str) -> Option<Value> {
    Some(match name {
        "mixed-none" => Value::Mixed(v::Mixed::None),
        "mixed-ping" => Value::Mixed(v::Mixed::Ping),
        "mixed-payload" => Value::Mixed(payload()),
        "tags-none" => Value::Tags(v::Tags::None),
        "tags-ping" => Value::Tags(v::Tags::Ping),
        "tags-pong" => Value::Tags(v::Tags::Pong),
        "single-none" => Value::Single(v::Single::None),
        "single-ack" => Value::Single(v::Single::Ack),
        "empty-none" => Value::Empty(v::Empty::None),
        "frame-ping" => Value::Frame(v::Frame { lead: 5, choice: v::Mixed::Ping, tail: 6 }),
        "batch-empty" => Value::Batch(v::Batch { values: [v::Mixed::None; 2], values_count: 0 }),
        "batch-ping" => Value::Batch(v::Batch { values: [v::Mixed::Ping, v::Mixed::None], values_count: 1 }),
        "batch-ping-payload" => Value::Batch(v::Batch { values: [v::Mixed::Ping, payload()], values_count: 2 }),
        _ => return None,
    })
}

// Every read lands on storage dirtied first: a selected payload must be
// constructed with its declared defaults, whatever the storage held.
fn dirty_mixed() -> v::Mixed {
    let mut arm = v::DefaultArm::new();
    arm.entries_count = 2;
    arm.entries[0].retries = 99;
    arm.entries[1].retries = 99;
    arm.marker = 1;
    v::Mixed::Payload(arm)
}

fn dirty(kind: &str) -> Option<Value> {
    Some(match kind {
        "Mixed" => Value::Mixed(dirty_mixed()),
        "Tags" => Value::Tags(v::Tags::Pong),
        "Single" => Value::Single(v::Single::Ack),
        "Empty" => Value::Empty(v::Empty::None),
        "Frame" => Value::Frame(v::Frame { lead: 1, choice: dirty_mixed(), tail: 1 }),
        "Batch" => Value::Batch(v::Batch { values: [dirty_mixed(); 2], values_count: 2 }),
        _ => return None,
    })
}

fn type_of(value: &Value) -> &'static str {
    match value {
        Value::Mixed(_) => "Mixed",
        Value::Tags(_) => "Tags",
        Value::Single(_) => "Single",
        Value::Empty(_) => "Empty",
        Value::Frame(_) => "Frame",
        Value::Batch(_) => "Batch",
    }
}

fn write_value(stream: &mut WriteStream<'_>, value: &Value) -> v::Result {
    match value {
        Value::Mixed(x) => v::write_mixed(stream, x),
        Value::Tags(x) => v::write_tags(stream, x),
        Value::Single(x) => v::write_single(stream, x),
        Value::Empty(x) => v::write_empty(stream, x),
        Value::Frame(x) => v::write_frame(stream, x),
        Value::Batch(x) => v::write_batch(stream, x),
    }
}

fn read_value(stream: &mut ReadStream<'_>, value: &mut Value) -> v::Result {
    match value {
        Value::Mixed(x) => v::read_mixed(stream, x),
        Value::Tags(x) => v::read_tags(stream, x),
        Value::Single(x) => v::read_single(stream, x),
        Value::Empty(x) => v::read_empty(stream, x),
        Value::Frame(x) => v::read_frame(stream, x),
        Value::Batch(x) => v::read_batch(stream, x),
    }
}

// The used part of a value: a batch's unused tail slots are not its value.
fn used(value: &Value) -> Value {
    match value {
        Value::Batch(b) => {
            let mut b = *b;
            for i in (b.values_count.clamp(0, 2) as usize)..2 {
                b.values[i] = v::Mixed::None;
            }
            Value::Batch(b)
        }
        other => *other,
    }
}

fn load(dir: &str, name: &str) -> Result<Vec<u8>, String> {
    std::fs::read(format!("{dir}/{name}.bin")).map_err(|e| format!("cannot open {dir}/{name}.bin: {e}"))
}

// The read allocation extends past the packet data (STANDARD.md); the
// logical length stays exact, a zero-length pin included.
fn read_pin(pin: &[u8], value: &mut Value) -> (v::Result, u64) {
    let mut buffer = vec![0u8; pin.len() + 8];
    buffer[..pin.len()].copy_from_slice(pin);
    let mut stream = ReadStream::new(&buffer, pin.len());
    let result = read_value(&mut stream, value);
    (result, stream.bits_processed())
}

fn write_bytes(value: &Value) -> Result<(Vec<u8>, u64), String> {
    let mut out = [0u8; 64];
    let mut stream = WriteStream::new(&mut out);
    write_value(&mut stream, value).map_err(|e| format!("write refused: {e:?}"))?;
    let bits = stream.bits_processed();
    stream.flush();
    Ok((stream.data().to_vec(), bits))
}

fn accept_case(dir: &str, name: &str, kind: &str, bits: &str) -> Result<(), String> {
    let want = build(name).ok_or("unknown accept case")?;
    if type_of(&want) != kind {
        return Err(format!("case type {kind}, the driver builds another"));
    }
    let bits: u64 = bits.parse().map_err(|_| format!("bits column {bits}"))?;
    let pin = load(dir, name)?;
    let (out, wrote) = write_bytes(&want)?;
    if wrote != bits {
        return Err(format!("write has {wrote} bits, want {bits}"));
    }
    if out != pin {
        return Err("write bytes differ from the pin".into());
    }
    let mut got = dirty(kind).ok_or_else(|| format!("unknown type {kind}"))?;
    let (result, read) = read_pin(&pin, &mut got);
    result.map_err(|e| format!("read refused: {e:?}"))?;
    // The public read ends the packet: the stream stands on the byte boundary.
    if read != bits.div_ceil(8) * 8 {
        return Err(format!("read has {read} bits, want {bits} rounded up to a byte"));
    }
    if used(&got) != used(&want) {
        return Err(format!("read value differs: {got:?}, want {want:?}"));
    }
    let (again, _) = write_bytes(&got)?;
    if again != pin {
        return Err("the read value writes other bytes than the pin".into());
    }
    Ok(())
}

fn refuse_case(dir: &str, name: &str, kind: &str) -> Result<(), String> {
    let mut got = dirty(kind).ok_or_else(|| format!("unknown type {kind}"))?;
    let pin = load(dir, name)?;
    match read_pin(&pin, &mut got).0 {
        Ok(()) => Err("read accepted".into()),
        Err(_) => Ok(()),
    }
}

fn answer(dir: &str, line: &str) -> Option<(String, Result<(), String>)> {
    let fields: Vec<&str> = line.split_whitespace().collect();
    let name = *fields.first()?;
    if name.starts_with('#') {
        return None;
    }
    if fields.len() < 4 {
        return Some((name.into(), Err("a case line needs <name> <type> <verdict> <bits>".into())));
    }
    let (kind, verdict, bits) = (fields[1], fields[2], fields[3]);
    let run = || match verdict {
        "accept" => accept_case(dir, name, kind, bits),
        "refuse" => refuse_case(dir, name, kind),
        other => Err(format!("verdict {other}")),
    };
    let result = match catch_unwind(AssertUnwindSafe(run)) {
        Ok(r) => r,
        Err(panic) => {
            let what = panic
                .downcast_ref::<String>()
                .cloned()
                .or_else(|| panic.downcast_ref::<&str>().map(|s| s.to_string()))
                .unwrap_or_else(|| "a panic".into());
            Err(format!("panicked: {what}"))
        }
    };
    Some((name.into(), result))
}

fn main() {
    let Some(dir) = std::env::args().nth(1) else {
        eprintln!("usage: packet-void-rust <testdata/wire/packet-void>");
        std::process::exit(2);
    };
    // The generated surface beside the wire: tag extents, debug names and
    // maximum sizes. A miss exits non-zero, which tools/casegate holds red.
    if v::MixedType::MAX.0 != 2
        || v::TagsType::MAX.0 != 2
        || v::SingleType::MAX.0 != 1
        || v::EmptyType::MAX.0 != 0
        || v::MixedType::PING.as_str() != "Ping"
        || v::TagsType::PONG.as_str() != "Pong"
        || v::MIXED_MAX_BITS != 23
        || v::TAGS_MAX_BITS != 2
        || v::SINGLE_MAX_BITS != 1
        || v::EMPTY_MAX_BITS != 0
    {
        println!("FAILED: packet-void tag extents, debug names or maximum bits");
        std::process::exit(1);
    }
    let cases = match std::fs::read_to_string(format!("{dir}/cases.txt")) {
        Ok(text) => text,
        Err(e) => {
            eprintln!("cannot open {dir}/cases.txt: {e}");
            std::process::exit(2);
        }
    };
    // Panics are answers here, not noise on stderr.
    std::panic::set_hook(Box::new(|_| {}));
    for line in cases.lines() {
        if let Some((name, result)) = answer(&dir, line) {
            match result {
                Ok(()) => println!("case {name} ok"),
                Err(why) => println!("case {name} FAILED: {why}"),
            }
        }
    }
}
