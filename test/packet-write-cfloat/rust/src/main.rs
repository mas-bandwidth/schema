use packetcfloat::{self as c, CFloat, RatioEndpoint, RatioPair};
use serialize::{ReadStream, Stream, WriteStream};
use std::panic::{AssertUnwindSafe, catch_unwind};

fn value(text: &str) -> f32 {
    match text {
        "NaN" => f32::NAN,
        "+Inf" => f32::INFINITY,
        "-Inf" => f32::NEG_INFINITY,
        other => other.parse().expect("float case"),
    }
}

fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|b| format!("{b:02x}")).collect()
}

// A failed case: its FAILED line on stderr, its answer on stdout (the line
// tools/casegate reads), and the run ends; casegate names every case after
// it as unanswered.
fn fail(name: &str, why: String) -> ! {
    eprintln!("FAILED: {name} {why}");
    println!("case {name} FAILED: {why}");
    std::process::exit(1);
}

fn main() {
    let cases = std::env::args().nth(1).expect("shared cases path");
    std::panic::set_hook(Box::new(|_| {}));
    let mut ran = 0u32;
    for line in std::fs::read_to_string(&cases).expect("read cases").lines() {
        let fields: Vec<_> = line.split_whitespace().collect();
        if fields.is_empty() || fields[0].starts_with('#') {
            continue;
        }
        if fields.len() < 3 || fields.len() % 2 != 1 {
            eprintln!("FAILED: malformed case line {line:?}");
            std::process::exit(1);
        }
        ran += 1;
        let name = fields[0];
        let input = value(fields[1]);
        let wanted = fields[2];
        // [reads-back <v>] [type RatioEndpoint|RatioPair] [wire <hex>] [bits <n>], in pairs
        let mut expected = input;
        let mut ratio = false;
        // a RatioPair writes the value to both fields
        let mut is_pair = false;
        let mut wire: Option<String> = None;
        let mut want_bits: Option<usize> = None;
        for pair in fields[3..].chunks(2) {
            match (pair[0], pair[1]) {
                ("reads-back", v) => expected = value(v),
                ("type", "RatioEndpoint") => ratio = true,
                ("type", "RatioPair") => is_pair = true,
                ("wire", h) => wire = Some(h.to_ascii_lowercase()),
                ("bits", n) => match n.parse() {
                    Ok(n) => want_bits = Some(n),
                    Err(_) => fail(name, format!("malformed bits in {line:?}")),
                },
                _ => fail(name, format!("malformed modifier in {line:?}")),
            }
        }
        let old_hook = std::panic::take_hook();
        std::panic::set_hook(Box::new(|_| {}));
        let attempt = catch_unwind(AssertUnwindSafe(|| {
            let mut bytes = [0u8; 64];
            let mut ws = WriteStream::new(&mut bytes);
            if is_pair {
                c::write_ratio_pair(
                    &mut ws,
                    &RatioPair {
                        first: input,
                        second: input,
                    },
                )?;
            } else if ratio {
                c::write_ratio_endpoint(&mut ws, &RatioEndpoint { ratio: input })?;
            } else {
                c::write_c_float(&mut ws, &CFloat { x: input })?;
            }
            let bits = ws.bits_processed();
            ws.flush();
            Ok::<_, c::Error>((ws.data().to_vec(), bits))
        }));
        std::panic::set_hook(old_hook);
        let (data, bits) = match attempt {
            Ok(Ok(pair)) => pair,
            Err(_) if wanted == "refused" => {
                println!("case {name} ok");
                continue;
            }
            Ok(Err(error)) => fail(name, format!("unexpectedly refused: {error:?}")),
            Err(_) => fail(name, "unexpectedly panicked".to_string()),
        };
        if wanted != "written" {
            fail(name, "was written but must be refused".to_string());
        }
        if let Some(want) = want_bits {
            if bits as usize != want {
                fail(
                    name,
                    format!("wrote {bits} bits before the flush, want {want}"),
                );
            }
        }
        if let Some(want) = &wire {
            if hex(&data) != *want {
                fail(name, format!("wrote {}, want {want}", hex(&data)));
            }
        }
        let mut padded = vec![0; (data.len() + 3) & !3];
        padded[..data.len()].copy_from_slice(&data);
        let mut rs = ReadStream::new(&padded, data.len());
        let got = if is_pair {
            let mut output = RatioPair::default();
            c::read_ratio_pair(&mut rs, &mut output).expect("read back");
            if output.first.to_bits() != output.second.to_bits() {
                fail(
                    name,
                    format!(
                        "the second field read back {}, the first {}",
                        output.second, output.first
                    ),
                );
            }
            output.first
        } else if ratio {
            let mut output = RatioEndpoint::default();
            c::read_ratio_endpoint(&mut rs, &mut output).expect("read back");
            output.ratio
        } else {
            let mut output = CFloat::default();
            c::read_c_float(&mut rs, &mut output).expect("read back");
            output.x
        };
        if got.to_bits() != expected.to_bits() {
            fail(name, format!("read back {got}, expected {expected}"));
        }
        // The public read ends the packet (SPEC §5): it consumes the final
        // byte's unused bits, so it stops at the written bits rounded up to a
        // whole byte.
        let bits = bits.div_ceil(8) * 8;
        if rs.bits_processed() != bits {
            fail(
                name,
                format!("consumed {} bits, wrote {bits}", rs.bits_processed()),
            );
        }
        println!("case {name} ok");
    }
    // a summary; tools/casegate holds every case of the file to an answer
    // (every failure above exits)
    if ran == 0 {
        eprintln!("FAILED: no cases in {cases}");
    }
    println!(
        "packet write compressed float Rust: ran {ran}, failed {}",
        u32::from(ran == 0)
    );
    if ran == 0 {
        std::process::exit(1);
    }
}
