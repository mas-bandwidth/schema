use packetcfloat::{self as c, CFloat};
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

fn main() {
    let cases = std::env::args().nth(1).expect("shared cases path");
    std::panic::set_hook(Box::new(|_| {}));
    for line in std::fs::read_to_string(cases).expect("read cases").lines() {
        let fields: Vec<_> = line.split_whitespace().collect();
        if fields.len() < 3 || fields[0].starts_with('#') {
            continue;
        }
        let name = fields[0];
        let input = value(fields[1]);
        let wanted = fields[2];
        let old_hook = std::panic::take_hook();
        std::panic::set_hook(Box::new(|_| {}));
        let attempt = catch_unwind(AssertUnwindSafe(|| {
            let mut bytes = [0u8; 64];
            let mut ws = WriteStream::new(&mut bytes);
            c::write_c_float(&mut ws, &CFloat { x: input })?;
            let bits = ws.bits_processed();
            ws.flush();
            Ok::<_, c::Error>((ws.data().to_vec(), bits))
        }));
        std::panic::set_hook(old_hook);
        let (data, bits) = match attempt {
            Ok(Ok(pair)) => pair,
            Ok(Err(_)) | Err(_) if wanted == "refused" => continue,
            Ok(Err(error)) => {
                eprintln!("FAILED: {name} unexpectedly refused: {error:?}");
                std::process::exit(1);
            }
            Err(_) => {
                eprintln!("FAILED: {name} unexpectedly panicked");
                std::process::exit(1);
            }
        };
        if wanted != "written" {
            eprintln!("FAILED: {name} was written but must be refused");
            std::process::exit(1);
        }
        let mut padded = vec![0; (data.len() + 3) & !3];
        padded[..data.len()].copy_from_slice(&data);
        let mut rs = ReadStream::new(&padded, data.len());
        let mut output = CFloat::default();
        c::read_c_float(&mut rs, &mut output).expect("read back");
        let expected = if fields.len() >= 5 && fields[3] == "reads-back" {
            value(fields[4])
        } else {
            input
        };
        if output.x != expected {
            eprintln!("FAILED: {name} read back {}, expected {expected}", output.x);
            std::process::exit(1);
        }
        // The public read ends the packet (SPEC §5): it consumes the final
        // byte's unused bits, so it stops at the written bits rounded up to a
        // whole byte.
        let bits = bits.div_ceil(8) * 8;
        if rs.bits_processed() != bits {
            eprintln!("FAILED: {name} consumed {} bits, wrote {bits}", rs.bits_processed());
            std::process::exit(1);
        }
    }
    println!("packet write compressed float Rust: OK");
}
