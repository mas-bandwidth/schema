// Rust packet Write refusal surface (SPEC §5.1, write-refusal-packet): in a
// debug build the writer's debug_assert! refuses each value its declaration
// does not admit, observed here as a caught panic whose message names the
// field; each lawful twin at the same boundary is written and read back.
// Rust cannot build a union tag outside its arm set (Shot is a native enum),
// so that row is named not-representable, by case; a case neither run nor so
// named fails.
use packetwriterefusal::{
    Grade, Laser, Missile, PacketWriteRefusal, Shot, read_packet_write_refusal, write_packet_write_refusal,
};
use serialize::{ReadStream, WriteStream};
use std::panic;

const NOT_REPRESENTABLE: &[&str] = &["union_tag_high"];

struct Case {
    name: String,
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
        assert!(parts.len() == 2, "invalid case row: {line}");
        cases.push(Case { name: parts[0].into(), verdict: parts[1].into() });
    }
    cases
}

// The case's one changed field, and the fragment of the assert message that
// must name it when refused.
fn build(name: &str) -> Option<(PacketWriteRefusal, &'static str)> {
    let mut v = PacketWriteRefusal::new();
    let field = match name {
        "scalar_high" => { v.scalar = 11; "scalar" }
        "scalar_low" => { v.scalar = -1; "scalar" }
        "scalar_at_max" => { v.scalar = 10; "scalar" }
        "scalar_at_min" => { v.scalar = 0; "scalar" }
        "bits_wide" => { v.narrow = 200; "narrow" }
        "bits_at_max" => { v.narrow = 127; "narrow" }
        "enum_high" => { v.grade = Grade(4); "grade" }
        "enum_at_max" => { v.grade = Grade::GOLD; "grade" }
        "union_tag_at_max" => { v.shot = Shot::Missile(Missile { fuel: 7 }); "shot" }
        "count_high" => { v.window_count = 9; "window_count" }
        "count_low" => { v.window_count = 1; "window_count" }
        "count_at_max" => { v.window_count = 8; "window_count" }
        "count_at_min" => { v.window_count = 2; "window_count" }
        "text_long" => { v.text_length = 9; "text_length" }
        "text_illformed" => { v.text[0] = 0xff; v.text_length = 1; "text" }
        "text_zero" => { v.text[0] = 0; v.text_length = 1; "text" }
        "text_at_max" => { v.text = [b'a'; 8]; v.text_length = 8; "text" }
        "const_wrong" => { v.fixed_constant = 6; "fixed_constant" }
        "const_at" => { v.fixed_constant = 5; "fixed_constant" }
        "reserved_wrong" => { v.reserved_zero = 1; "reserved_zero" }
        "reserved_at" => { v.reserved_zero = 0; "reserved_zero" }
        _ => return None,
    };
    let _ = Laser::default();
    Some((v, field))
}

fn write_read(value: &PacketWriteRefusal) -> Result<(), String> {
    let mut buffer = [0u8; 128];
    let mut writer = WriteStream::new(&mut buffer);
    write_packet_write_refusal(&mut writer, value).map_err(|e| format!("write: {e:?}"))?;
    writer.flush();
    let wire = writer.data().to_vec();
    let mut padded = vec![0u8; (wire.len() + 3) & !3];
    padded[..wire.len()].copy_from_slice(&wire);
    let mut decoded = PacketWriteRefusal::default();
    let mut reader = ReadStream::new(&padded, wire.len());
    read_packet_write_refusal(&mut reader, &mut decoded).map_err(|e| format!("read back: {e:?}"))?;
    if decoded.scalar != value.scalar || decoded.narrow != value.narrow || decoded.grade != value.grade
        || decoded.window_count != value.window_count || decoded.text_length != value.text_length
        || decoded.shot != value.shot
    {
        return Err("read back a different value".into());
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
        eprintln!("FAILED: packet-write-refusal needs a debug build: Rust write checks are debug asserts");
        std::process::exit(2);
    }
    let cases = load(&args[1]);
    panic::set_hook(Box::new(|_| {}));
    let mut failed = 0;
    let mut named = 0;
    for case in &cases {
        if NOT_REPRESENTABLE.contains(&case.name.as_str()) {
            if case.verdict != "refused" {
                eprintln!("FAILED: packet-write-refusal {}: named not representable but its verdict is {}", case.name, case.verdict);
                failed += 1;
            }
            named += 1;
            continue;
        }
        let Some((value, field)) = build(&case.name) else {
            eprintln!("FAILED: packet-write-refusal {}: neither run nor named not representable in Rust", case.name);
            failed += 1;
            continue;
        };
        let outcome = panic::catch_unwind(|| write_read(&value));
        let problem = match (case.verdict.as_str(), outcome) {
            ("refused", Err(payload)) => {
                let message = panic_message(payload.as_ref());
                if message.starts_with(field) {
                    None
                } else {
                    Some(format!("expected the {field} debug assert, panicked with {message:?}"))
                }
            }
            ("refused", Ok(_)) => Some("expected debug assert, the writer accepted the value".into()),
            ("wrote", Ok(Ok(()))) => None,
            ("wrote", Ok(Err(e))) => Some(format!("expected a written round trip, {e}")),
            ("wrote", Err(payload)) => {
                Some(format!("expected a written round trip, panicked with {:?}", panic_message(payload.as_ref())))
            }
            (other, _) => Some(format!("invalid verdict {other:?}")),
        };
        if let Some(problem) = problem {
            eprintln!("FAILED: packet-write-refusal {}: {problem}", case.name);
            failed += 1;
        }
    }
    if failed > 0 {
        eprintln!("FAILED: packet-write-refusal Rust: {failed} of {} shared cases", cases.len());
        std::process::exit(1);
    }
    println!("packet-write-refusal Rust: {} shared cases, {} named not representable, all passed", cases.len(), named);
}
