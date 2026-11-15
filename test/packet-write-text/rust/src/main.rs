// Rust packet Write text contract (SPEC §5.1 row 8): in a debug build the
// writer's debug_assert! refuses each ill-formed case, observed here as a
// caught panic; each well-formed case is written and read back byte-exact.
use packetwritetext::{TextPacket, read_text_packet, write_text_packet};
use serialize::{ReadStream, Stream, WriteStream};
use std::panic;

struct Case {
    name: String,
    bytes: Vec<u8>,
    verdict: String,
}

fn load(path: &str) -> Vec<Case> {
    let text = std::fs::read_to_string(path).unwrap_or_else(|e| panic!("{path}: {e}"));
    let mut cases = Vec::new();
    for line in text.lines() {
        let line = line.trim();
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        let parts: Vec<&str> = line.split('|').map(str::trim).collect();
        assert!(parts.len() == 3, "invalid case row: {line}");
        let bytes = if parts[1] == "-" {
            Vec::new()
        } else {
            parts[1]
                .split_whitespace()
                .map(|b| u8::from_str_radix(b, 16).unwrap_or_else(|_| panic!("invalid bytes for {}", parts[0])))
                .collect()
        };
        cases.push(Case { name: parts[0].into(), bytes, verdict: parts[2].into() });
    }
    cases
}

// Write one value; on success read it back and require the same used bytes
// and the same bit count on both sides.
fn round_trip(bytes: &[u8]) -> Result<(), String> {
    let mut value = TextPacket::default();
    if bytes.len() > value.text.len() {
        return Err("case longer than the field".into());
    }
    value.text[..bytes.len()].copy_from_slice(bytes);
    value.text_length = bytes.len() as i32;
    let mut buffer = [0u8; 64];
    let mut writer = WriteStream::new(&mut buffer);
    write_text_packet(&mut writer, &value).map_err(|e| format!("write: {e:?}"))?;
    let bits = writer.bits_processed();
    writer.flush();
    let wire = writer.data().to_vec();
    let mut padded = vec![0u8; (wire.len() + 3) & !3];
    padded[..wire.len()].copy_from_slice(&wire);
    let mut decoded = TextPacket::default();
    let mut reader = ReadStream::new(&padded, wire.len());
    read_text_packet(&mut reader, &mut decoded).map_err(|e| format!("read back: {e:?}"))?;
    if decoded.text_length as usize != bytes.len() || &decoded.text[..bytes.len()] != bytes {
        return Err("read back different bytes".into());
    }
    if reader.bits_processed() != bits {
        return Err("read back a different bit count".into());
    }
    Ok(())
}

fn panic_message(payload: &(dyn std::any::Any + Send)) -> String {
    if let Some(s) = payload.downcast_ref::<&str>() {
        return (*s).into();
    }
    if let Some(s) = payload.downcast_ref::<String>() {
        return s.clone();
    }
    String::new()
}

fn main() {
    let args: Vec<String> = std::env::args().collect();
    if args.len() != 2 {
        eprintln!("usage: {} cases.txt", args[0]);
        std::process::exit(2);
    }
    if !cfg!(debug_assertions) {
        eprintln!("FAILED: packet-write-text needs a debug build: Rust write checks are debug asserts");
        std::process::exit(2);
    }
    let cases = load(&args[1]);
    panic::set_hook(Box::new(|_| {}));
    let mut failed = 0;
    for case in &cases {
        let outcome = panic::catch_unwind(|| round_trip(&case.bytes));
        let problem = match (case.verdict.as_str(), outcome) {
            ("refused", Err(payload)) => {
                let message = panic_message(payload.as_ref());
                if message.starts_with("text holds") || message.starts_with("text is not") {
                    None
                } else {
                    Some(format!("expected the text debug assert, panicked with {message:?}"))
                }
            }
            ("refused", Ok(_)) => Some("expected debug assert, the writer accepted the value".into()),
            ("written", Ok(Ok(()))) => None,
            ("written", Ok(Err(e))) => Some(format!("expected byte-exact round trip, {e}")),
            ("written", Err(payload)) => {
                Some(format!("expected byte-exact round trip, panicked with {:?}", panic_message(payload.as_ref())))
            }
            (other, _) => Some(format!("invalid verdict {other:?}")),
        };
        if let Some(problem) = problem {
            eprintln!("FAILED: packet-write-text {}: {problem}", case.name);
            failed += 1;
        }
    }
    if failed > 0 {
        eprintln!("FAILED: packet-write-text Rust: {failed} of {} shared cases", cases.len());
        std::process::exit(1);
    }
    println!("packet-write-text Rust: all {} shared cases passed", cases.len());
}
