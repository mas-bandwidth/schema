// The packet end-check driver (docs/SPEC.md §5 "Read termination";
// docs/PACKET-WIRE-ALGORITHM.md §3 READ_PACKET): each shared vector under
// testdata/wire/packet-end is read with the generated public one-packet read.
// The exact packet is accepted as value 3; a whole byte left after it and a
// final byte with non-zero unused bits are both refused.

use packetend as p;
use serialize::ReadStream;
use std::process::exit;

fn read_packet(bytes: &[u8]) -> (bool, u8) {
    // the read allocation extends past the packet data (STANDARD.md)
    let mut buffer = vec![0u8; bytes.len() + 8];
    buffer[..bytes.len()].copy_from_slice(bytes);
    let mut stream = ReadStream::new(&buffer, bytes.len());
    let mut value = p::End::default();
    match p::read_end(&mut stream, &mut value) {
        Ok(()) => (true, value.value),
        Err(_) => (false, value.value),
    }
}

// The vectors this driver reads. The directory is held to this list: a .bin
// the driver does not read is red, so a vector added to the shared corpus
// cannot ride unread. A named vector that is gone panics at its read.
const KNOWN: [&str; 3] = ["exact.bin", "trailing.bin", "padding.bin"];

fn hold_directory(dir: &str) -> bool {
    let mut held = true;
    for entry in std::fs::read_dir(dir).expect("list the vector directory") {
        let name = entry.expect("directory entry").file_name();
        let name = name.to_string_lossy();
        if name.ends_with(".bin") && !KNOWN.contains(&name.as_ref()) {
            println!("FAILED: packet end: vector {name} is in {dir} and this driver does not read it");
            held = false;
        }
    }
    held
}

fn main() {
    let dir = std::env::args().nth(1).expect("vector directory");
    let mut failed = !hold_directory(&dir);
    // one counted unit per vector: the last line is "ran N, failed M" and
    // build/mustrun holds N to the count the recipe registers
    let mut ran = 0u32;
    let mut failures = u32::from(failed);

    ran += 1;
    let exact = std::fs::read(format!("{dir}/exact.bin")).expect("exact.bin");
    let (ok, value) = read_packet(&exact);
    if !ok {
        println!("FAILED: packet end exact: the exact packet was refused");
        failed = true;
        failures += 1;
    } else if value != 3 {
        println!("FAILED: packet end exact: value {value}, want 3");
        failed = true;
        failures += 1;
    }

    for name in ["trailing", "padding"] {
        ran += 1;
        let bytes = std::fs::read(format!("{dir}/{name}.bin")).expect(name);
        if read_packet(&bytes).0 {
            println!("FAILED: packet end {name}: the packet was accepted");
            failed = true;
            failures += 1;
        }
    }

    if !failed {
        println!("packet end: exact accepted as 3, trailing and padding refused");
    }
    println!("packet-end-rust: ran {ran}, failed {}", failures.min(ran));
    if failed || ran == 0 {
        exit(1);
    }
}
