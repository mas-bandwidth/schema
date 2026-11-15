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

fn main() {
    let dir = std::env::args().nth(1).expect("vector directory");
    let mut failed = false;

    let exact = std::fs::read(format!("{dir}/exact.bin")).expect("exact.bin");
    let (ok, value) = read_packet(&exact);
    if !ok {
        println!("FAILED: packet end exact: the exact packet was refused");
        failed = true;
    } else if value != 3 {
        println!("FAILED: packet end exact: value {value}, want 3");
        failed = true;
    }

    for name in ["trailing", "padding"] {
        let bytes = std::fs::read(format!("{dir}/{name}.bin")).expect(name);
        if read_packet(&bytes).0 {
            println!("FAILED: packet end {name}: the packet was accepted");
            failed = true;
        }
    }

    if failed {
        exit(1);
    }
    println!("packet end: exact accepted as 3, trailing and padding refused");
}
