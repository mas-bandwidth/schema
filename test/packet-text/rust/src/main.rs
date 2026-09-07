use packettext::{Narrow, read_narrow, write_narrow};
use serialize::{ReadStream, Stream, WriteStream};
use std::io::{self, BufRead};

fn hex(bytes: &[u8]) -> String {
    if bytes.is_empty() {
        return "-".into();
    }
    bytes.iter().map(|b| format!("{b:02x}")).collect()
}

fn main() {
    for line in io::stdin().lock().lines() {
        let line = line.expect("input");
        let size = if line == "-" { 0 } else { line.len() / 2 };
        let mut buffer = [0u8; 256];
        for (i, b) in buffer[..size].iter_mut().enumerate() {
            *b = u8::from_str_radix(&line[i * 2..i * 2 + 2], 16).expect("hex");
        }
        let mut value = Narrow::default();
        let mut r = ReadStream::new(&buffer, size);
        if read_narrow(&mut r, &mut value).is_err() {
            println!("REFUSE");
            continue;
        }
        let mut encoded = [0u8; 256];
        let mut w = WriteStream::new(&mut encoded);
        write_narrow(&mut w, &value).expect("write");
        w.flush();
        println!(
            "OK {} {} {} {}",
            r.bits_processed(),
            hex(&value.text[..value.text_length as usize]),
            w.bits_processed(),
            hex(w.data())
        );
    }
}
