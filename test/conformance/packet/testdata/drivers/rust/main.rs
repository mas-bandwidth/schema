// The packet carrier's Rust driver (test/conformance/packet/carrier.go states
// the contract): write|read <codec> <in> <out>. The generated crate is the
// `seed` dependency; one codec per seed root.

use serialize::{ReadStream, WriteStream};
use std::fmt::Write as _;
use std::process::exit;

fn write_seed_t(f: &[i64]) -> Option<Vec<u8>> {
    let mut v = seed::T::default();
    let mut at = 0;
    let tag = *f.get(at)?;
    at += 1;
    if tag == 1 {
        let a = seed::Small { v: *f.get(at)? as u8 };
        at += 1;
        v.u = seed::U::A(a);
    } else if tag != 0 {
        return None;
    }
    for i in 0..4 {
        v.data[i] = *f.get(at)? as u8;
        at += 1;
    }
    if at != f.len() {
        return None;
    }
    let mut buffer = vec![0u8; seed::T_MAX_BYTES + 8];
    let mut stream = WriteStream::new(&mut buffer);
    seed::write_t(&mut stream, &v).ok()?;
    stream.flush();
    Some(stream.data().to_vec())
}

fn read_seed_t(p: &[u8]) -> Option<Vec<i64>> {
    let mut buffer = p.to_vec();
    buffer.resize(p.len() + 8, 0);
    let mut stream = ReadStream::new(&buffer, p.len());
    let mut v = seed::T::default();
    seed::read_t(&mut stream, &mut v).ok()?;
    let mut out = Vec::new();
    match v.u {
        seed::U::None => out.push(0),
        seed::U::A(a) => {
            out.push(1);
            out.push(i64::from(a.v));
        }
    }
    for b in v.data {
        out.push(i64::from(b));
    }
    Some(out)
}

type Writer = fn(&[i64]) -> Option<Vec<u8>>;
type Reader = fn(&[u8]) -> Option<Vec<i64>>;

fn fail(msg: &str) -> ! {
    eprintln!("driver: {msg}");
    exit(1)
}

fn main() {
    let args: Vec<String> = std::env::args().collect();
    if args.len() != 5 {
        fail("usage: driver write|read <codec> <in> <out>");
    }
    let (writer, reader): (Writer, Reader) = match args[2].as_str() {
        "union-before-bytes" => (write_seed_t, read_seed_t),
        other => fail(&format!("no codec {other}")),
    };
    let input = std::fs::read_to_string(&args[3]).unwrap_or_else(|e| fail(&e.to_string()));
    let mut out = String::new();
    for line in input.lines() {
        match args[1].as_str() {
            "write" => {
                let fields: Vec<i64> = line
                    .split_whitespace()
                    .map(|t| t.parse().unwrap_or_else(|e| fail(&format!("{t}: {e}"))))
                    .collect();
                match writer(&fields) {
                    Some(p) => {
                        for b in p {
                            write!(out, "{b:02x}").unwrap();
                        }
                        out.push('\n');
                    }
                    None => out.push_str("refused\n"),
                }
            }
            "read" => {
                if line.len() % 2 != 0 {
                    fail("bad hex line");
                }
                let p: Vec<u8> = (0..line.len())
                    .step_by(2)
                    .map(|i| u8::from_str_radix(&line[i..i + 2], 16).unwrap_or_else(|e| fail(&e.to_string())))
                    .collect();
                match reader(&p) {
                    Some(fields) => {
                        out.push_str("ok");
                        for n in fields {
                            write!(out, " {n}").unwrap();
                        }
                        out.push('\n');
                    }
                    None => out.push_str("refused\n"),
                }
            }
            other => fail(&format!("no verb {other}")),
        }
    }
    std::fs::write(&args[4], out).unwrap_or_else(|e| fail(&e.to_string()));
}
