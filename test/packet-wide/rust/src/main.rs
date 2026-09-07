use serialize::{ReadStream, Stream, WriteStream};
use std::io::{self, BufRead};

fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|b| format!("{b:02x}")).collect()
}

macro_rules! replay {
    ($ty:ty, $read:path, $write:path, $buffer:expr, $size:expr) => {{
        let mut value = <$ty>::default();
        value.text.fill(0x7f7f);
        let mut r = ReadStream::new(&$buffer, $size);
        if $read(&mut r, &mut value).is_err() {
            println!("REFUSE");
        } else {
            let mut encoded = [0u8; 256];
            let mut w = WriteStream::new(&mut encoded);
            $write(&mut w, &value).expect("write");
            w.flush();
            let units = &value.text[..value.text_length as usize];
            let text = if units.is_empty() {
                "-".into()
            } else {
                units.iter().map(|u| format!("{u:04x}")).collect::<String>()
            };
            println!(
                "OK {} {} {} {}",
                r.bits_processed(),
                text,
                w.bits_processed(),
                hex(w.data())
            );
        }
    }};
}

fn main() {
    for line in io::stdin().lock().lines() {
        let line = line.expect("input");
        let (bound, raw) = line.split_once(' ').expect("bound and wire");
        let size = if raw == "-" { 0 } else { raw.len() / 2 };
        let mut buffer = [0u8; 256];
        for (i, b) in buffer[..size].iter_mut().enumerate() {
            *b = u8::from_str_radix(&raw[i * 2..i * 2 + 2], 16).expect("hex");
        }
        match bound {
            "7" => replay!(
                wide::WideSeven,
                wide::read_wide_seven,
                wide::write_wide_seven,
                buffer,
                size
            ),
            "4" => replay!(
                wide::WideFour,
                wide::read_wide_four,
                wide::write_wide_four,
                buffer,
                size
            ),
            _ => panic!("unexpected bound"),
        }
    }
}

#[cfg(test)]
mod contracts {
    use super::*;
    use wideprobe as p;

    fn write<T>(value: &T, encode: impl Fn(&mut WriteStream, &T) -> p::Result) -> (Vec<u8>, u64) {
        let mut bytes = [0u8; 1024];
        let mut w = WriteStream::new(&mut bytes);
        encode(&mut w, value).expect("write");
        w.flush();
        (w.data().to_vec(), w.bits_processed())
    }
    fn read<T>(bytes: &[u8], value: &mut T, decode: impl Fn(&mut ReadStream, &mut T) -> p::Result) {
        let mut padded = vec![0u8; (bytes.len() + 7) & !7];
        padded[..bytes.len()].copy_from_slice(bytes);
        decode(&mut ReadStream::new(&padded, bytes.len()), value).expect("read");
    }
    #[test]
    fn bounds_null_pairing_and_reused_tail() {
        let mut value = wide::WideSeven::default();
        for length in [-1, 8, 1] {
            value.text_length = length;
            assert!(
                wide::write_wide_seven(&mut WriteStream::new(&mut [0u8; 256]), &value).is_err()
            );
        }
        value.text[0] = 0xd800;
        let mut buffer = [0u8; 256];
        let mut w = WriteStream::new(&mut buffer);
        wide::write_wide_seven(&mut w, &value).expect("writer leaves pairing to read");
        w.flush();
        let size = w.data().len();
        assert!(wide::read_wide_seven(&mut ReadStream::new(&buffer, size), &mut value).is_err());
        value.text[0] = 0xffff;
        let mut w = WriteStream::new(&mut buffer);
        wide::write_wide_seven(&mut w, &value).unwrap();
        w.flush();
        let size = w.data().len();
        value.text.fill(0x7f7f);
        wide::read_wide_seven(&mut ReadStream::new(&buffer, size), &mut value).unwrap();
        assert_eq!(
            value.text,
            [0xffff, 0x7f7f, 0x7f7f, 0x7f7f, 0x7f7f, 0x7f7f, 0x7f7f]
        );
    }
    #[test]
    fn branches_arrays_and_repeated_union_reads() {
        let mut sent = p::Conditional {
            enabled: true,
            text: [0xd800, 0xdc00, 0, 0],
            text_length: 2,
        };
        let (bytes, bits) = write(&sent, p::write_conditional);
        assert_eq!(bits, 68);
        assert_eq!(bytes[0] & 15, 5);
        let mut out = p::Conditional::default();
        read(&bytes, &mut out, p::read_conditional);
        assert_eq!(out, sent);
        sent.enabled = false;
        let (bytes, _) = write(&sent, p::write_conditional);
        read(&bytes, &mut out, p::read_conditional);
        assert_eq!(out, p::Conditional::default());
        let text = p::Text {
            value: [0xffff, 0, 0, 0],
            value_length: 1,
        };
        let choice = p::Choice::Text(text);
        let (bytes, _) = write(&choice, p::write_choice);
        let mut out = p::Choice::None;
        for _ in 0..2 {
            read(&bytes, &mut out, p::read_choice);
            assert_eq!(out, choice);
        }
        let mut bx = p::Box::new();
        assert_eq!(bx.counted_count, 1);
        bx.items[0] = text;
        bx.counted[0] = text;
        bx.choice = choice;
        let (bytes, _) = write(&bx, p::write_box);
        let mut out = p::Box::new();
        out.counted[1].value.fill(0x7f7f);
        read(&bytes, &mut out, p::read_box);
        assert_eq!(out.items, bx.items);
        assert_eq!(out.counted_count, 1);
        assert_eq!(out.counted[0], text);
        assert_eq!(out.counted[1].value, [0x7f7f; 4]);
        assert_eq!(out.choice, choice);
    }
}
