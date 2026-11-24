// Rust packet Write refusal surface (SPEC §5.1, write-refusal-packet). The one
// row list is test/conformance/write-refusal-packet/rows.txt
// (<name> <verdict> <op> <arg>), keyed here by name. In a
// debug build the writer's debug_assert! refuses each value its declaration
// does not admit, observed here as a caught panic whose message names the
// field; each lawful twin at the same boundary is written and read back.
// Rust cannot build a union tag outside its arm set (Shot is a native enum),
// so that row is named not-representable, by case; a row neither run nor so
// named fails, and so does a setter the file does not name.
use packetwriterefusal::{
    Grade, Laser, Missile, PERK_BOOST, PERK_CLOAK, PERK_SHIELD, PacketWriteRefusal, Shot, read_packet_write_refusal,
    write_packet_write_refusal,
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
        let parts: Vec<&str> = line.split_whitespace().collect();
        assert!(parts.len() == 4, "a row is not <name> <verdict> <op> <arg>: {line}");
        cases.push(Case { name: parts[0].into(), verdict: parts[1].into() });
    }
    cases
}

type Setter = fn(&mut PacketWriteRefusal);

// Each row's one changed field, and the fragment of the assert message that
// must name it when refused.
const SETTERS: &[(&str, &str, Setter)] = &[
    // write-refusal-packet setters: begin
    ("scalar_high", "scalar", |v| v.scalar = 11),
    ("scalar_low", "scalar", |v| v.scalar = -1),
    ("scalar_at_max", "scalar", |v| v.scalar = 10),
    ("scalar_at_min", "scalar", |v| v.scalar = 0),
    ("bits_wide", "narrow", |v| v.narrow = 200),
    ("bits_just_wide", "narrow", |v| v.narrow = 128),
    ("bits_at_max", "narrow", |v| v.narrow = 127),
    ("enum_high", "grade", |v| v.grade = Grade(4)),
    ("enum_at_max", "grade", |v| v.grade = Grade::GOLD),
    ("union_tag_at_max", "shot", |v| v.shot = Shot::Missile(Missile { fuel: 7 })),
    ("count_high", "window_count", |v| v.window_count = 9),
    ("count_low", "window_count", |v| v.window_count = 1),
    ("count_at_max", "window_count", |v| v.window_count = 8),
    ("count_at_min", "window_count", |v| v.window_count = 2),
    ("text_long", "text_length", |v| v.text_length = 9),
    ("text_illformed", "text", |v| {
        v.text[0] = 0xff;
        v.text_length = 1;
    }),
    ("text_zero", "text", |v| {
        v.text[0] = 0;
        v.text_length = 1;
    }),
    ("text_at_max", "text", |v| {
        v.text = [b'a'; 8];
        v.text_length = 8;
    }),
    ("const_wrong", "fixed_constant", |v| v.fixed_constant = 6),
    ("const_at", "fixed_constant", |v| v.fixed_constant = 5),
    ("reserved_wrong", "reserved_zero", |v| v.reserved_zero = 1),
    ("reserved_at", "reserved_zero", |v| v.reserved_zero = 0),
    ("flags_wide", "perks", |v| v.perks = 1 << 3),
    ("flags_wide_high", "perks", |v| v.perks = 1 << 40),
    ("flags_at_max", "perks", |v| v.perks = PERK_SHIELD | PERK_BOOST | PERK_CLOAK),
    // write-refusal-packet setters: end
];

fn build(name: &str) -> Option<(PacketWriteRefusal, &'static str)> {
    let (_, field, set) = SETTERS.iter().find(|(n, _, _)| *n == name)?;
    let mut v = PacketWriteRefusal::new();
    set(&mut v);
    let _ = Laser::default();
    Some((v, *field))
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
        || decoded.shot != value.shot || decoded.perks != value.perks
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
        eprintln!("usage: {} rows.txt", args[0]);
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
    let mut seen = std::collections::HashSet::new();
    for case in &cases {
        if !seen.insert(case.name.as_str()) {
            eprintln!("FAILED: packet-write-refusal {}: named twice in rows.txt", case.name);
            failed += 1;
            continue;
        }
        if NOT_REPRESENTABLE.contains(&case.name.as_str()) {
            if case.verdict != "refused" {
                eprintln!("FAILED: packet-write-refusal {}: named not representable but its verdict is {}", case.name, case.verdict);
                failed += 1;
            }
            named += 1;
            continue;
        }
        let Some((value, field)) = build(&case.name) else {
            eprintln!("FAILED: packet-write-refusal {}: a row in rows.txt neither run nor named not representable in Rust", case.name);
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
    for name in SETTERS.iter().map(|(n, _, _)| *n).chain(NOT_REPRESENTABLE.iter().copied()) {
        if !seen.contains(name) {
            eprintln!("FAILED: packet-write-refusal {name}: a setter or not-representable name rows.txt does not name");
            failed += 1;
        }
    }
    if failed > 0 {
        eprintln!("FAILED: packet-write-refusal Rust: {failed} of {} shared rows", cases.len());
        std::process::exit(1);
    }
    println!("packet-write-refusal Rust: {} rows, {} named not representable, all passed", cases.len(), named);
}
