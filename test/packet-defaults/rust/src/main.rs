// Independent values and C++ byte/bit pins, including reused-storage tails.
use packetdefaults as d;
use packetplain as p;
use serialize::{ReadStream, Stream, WriteStream};
use std::fmt::Debug;

fn sample() -> d::Sample {
    d::Sample {
        name: [0xc3, 0xa9, 0xf0, 0x90, 0x80, 0x80],
        name_length: 6,
        token: [0x5c, 0x6e, 0x5c, 0x74],
        token_length: 4,
        caps: 5,
        ..d::Sample::default()
    }
}
fn short() -> d::Sample {
    d::Sample {
        name: [b'A', 0, 0, 0, 0, 0],
        name_length: 1,
        token: [0, 255, 0, 0],
        token_length: 2,
        caps: 2,
        ..d::Sample::default()
    }
}
fn dirty() -> d::Sample {
    d::Sample {
        name: *b"dirty!",
        name_length: 6,
        token: [161, 162, 163, 164],
        token_length: 4,
        caps: 7,
        empty_name: *b"old",
        empty_name_length: 3,
        empty_token: [177, 178],
        empty_token_length: 2,
        empty_caps: 7,
    }
}
fn write<T, E: Debug>(
    value: &T,
    encode: impl Fn(&mut WriteStream, &T) -> Result<(), E>,
) -> (Vec<u8>, u64) {
    let mut buffer = [0u8; 4096];
    let mut stream = WriteStream::new(&mut buffer);
    encode(&mut stream, value).expect("write");
    let bits = stream.bits_processed();
    stream.flush();
    (stream.data().to_vec(), bits)
}
fn read<T: PartialEq + Debug>(
    bytes: &[u8],
    bits: u64,
    mut initial: T,
    want: T,
    decode: impl Fn(&mut ReadStream, &mut T) -> d::Result,
) {
    // The runtime reads words; physical padding is distinct from logical size.
    let mut buffer = vec![0u8; (bytes.len() + 3) & !3];
    buffer[..bytes.len()].copy_from_slice(bytes);
    let mut stream = ReadStream::new(&buffer, bytes.len());
    decode(&mut stream, &mut initial).expect("read");
    assert_eq!(stream.bits_processed(), bits, "read consumed bits");
    assert_eq!(initial, want, "read values and backing storage");
}
fn golden<T: PartialEq + Debug>(
    dir: &str,
    name: &str,
    value: &T,
    initial: T,
    want: T,
    encode: impl Fn(&mut WriteStream, &T) -> d::Result,
    decode: impl Fn(&mut ReadStream, &mut T) -> d::Result,
) {
    let bytes = std::fs::read(format!("{dir}/{name}.bin")).unwrap();
    let bits: u64 = std::fs::read_to_string(format!("{dir}/{name}.bits"))
        .unwrap()
        .trim()
        .parse()
        .unwrap();
    assert_eq!(
        write(value, encode),
        (bytes.clone(), bits),
        "C++ pin {name}"
    );
    read(&bytes, bits, initial, want, decode);
}

fn main() {
    let dir = std::env::args().nth(1).expect("golden directory");
    let expected = sample();
    // The negative control must compile, then fail this exact marker.
    assert_eq!(
        d::Sample::new(),
        expected,
        "packet-default constructor bytes"
    );
    assert_ne!(d::Sample::default(), expected);
    assert_eq!(d::EmptyOnly::new(), d::EmptyOnly::default());
    assert_eq!(
        d::Prefix::new(),
        d::Prefix {
            name: [0xc3, 0xa9, 0, 0, 0],
            name_length: 2,
            token: [0x5c, 0x6e, 0, 0, 0],
            token_length: 2
        }
    );
    let wide = d::WideMask::new();
    assert_eq!((wide.high, wide.all), (1u64 << 63, u64::MAX));
    let mut wide_bytes = vec![0; 16];
    wide_bytes[7] = 128;
    wide_bytes[8..].fill(255);
    assert_eq!(write(&wide, d::write_wide_mask), (wide_bytes.clone(), 128));
    assert_eq!(
        write(
            &p::WideMask {
                high: 1u64 << 63,
                all: u64::MAX
            },
            p::write_wide_mask
        ),
        (wide_bytes.clone(), 128)
    );
    read(
        &wide_bytes,
        128,
        d::WideMask::default(),
        wide,
        d::read_wide_mask,
    );
    let split = d::SplitMask {
        lead: 5,
        mask: 1u64 << 32,
        tail: 2,
    };
    assert_eq!(
        write(&split, d::write_split_mask),
        (vec![5, 0, 0, 0, 40], 38)
    );
    read(
        &[5, 0, 0, 0, 40],
        38,
        d::SplitMask::default(),
        split,
        d::read_split_mask,
    );
    let plain = p::Sample {
        name: expected.name,
        name_length: 6,
        token: expected.token,
        token_length: 4,
        caps: 5,
        ..p::Sample::default()
    };
    assert_eq!(
        write(&plain, p::write_sample),
        write(&d::Sample::new(), d::write_sample),
        "no default-based elision"
    );
    golden(
        &dir,
        "sample-defaults",
        &d::Sample::new(),
        d::Sample::default(),
        expected,
        d::write_sample,
        d::read_sample,
    );

    let batch = d::Batch::new();
    assert_eq!(
        batch,
        d::Batch {
            head: expected,
            items: [expected; 2],
            counted: [expected; 3],
            counted_count: 1
        }
    );
    let plain_batch = p::Batch::new();
    assert_eq!(plain_batch.counted_count, 1);
    assert_eq!(plain_batch.counted, [p::Sample::default(); 3]);
    let mut initial = d::Batch::new();
    initial.counted[1] = dirty();
    initial.counted[2] = short();
    golden(
        &dir,
        "batch-defaults",
        &batch,
        initial,
        initial,
        d::write_batch,
        d::read_batch,
    );
    let zero = d::ZeroCount::new();
    assert_eq!(
        zero,
        d::ZeroCount {
            items: [expected; 2],
            items_count: 0
        }
    );
    golden(
        &dir,
        "zero-count",
        &zero,
        d::ZeroCount {
            items: [dirty(), short()],
            items_count: 2,
        },
        d::ZeroCount {
            items: [dirty(), short()],
            items_count: 0,
        },
        d::write_zero_count,
        d::read_zero_count,
    );

    let conditional = d::Conditional::new();
    assert_eq!(
        conditional,
        d::Conditional {
            enabled: true,
            value: expected
        }
    );
    golden(
        &dir,
        "conditional-on",
        &conditional,
        d::Conditional::default(),
        conditional,
        d::write_conditional,
        d::read_conditional,
    );
    golden(
        &dir,
        "conditional-off",
        &d::Conditional {
            enabled: false,
            value: expected,
        },
        d::Conditional {
            enabled: true,
            value: dirty(),
        },
        d::Conditional::default(),
        d::write_conditional,
        d::read_conditional,
    );
    assert_eq!(d::Choice::default(), d::Choice::None);
    golden(
        &dir,
        "choice-sample",
        &d::Choice::Sample(expected),
        d::Choice::Conditional(conditional),
        d::Choice::Sample(expected),
        d::write_choice,
        d::read_choice,
    );

    let overlay = d::Sample {
        empty_name: *b"old",
        empty_name_length: 3,
        empty_token: [177, 178],
        empty_token_length: 2,
        empty_caps: 7,
        ..expected
    };
    let mut short_want = overlay;
    short_want.name[0] = b'A';
    short_want.name_length = 1;
    short_want.token[..2].copy_from_slice(&[0, 255]);
    short_want.token_length = 2;
    short_want.caps = 2;
    short_want.empty_name_length = 0;
    short_want.empty_token_length = 0;
    short_want.empty_caps = 0;
    golden(
        &dir,
        "sample-short",
        &short(),
        overlay,
        short_want,
        d::write_sample,
        d::read_sample,
    );
    let empty_want = d::Sample {
        name_length: 0,
        token_length: 0,
        caps: 0,
        empty_name_length: 0,
        empty_token_length: 0,
        empty_caps: 0,
        ..overlay
    };
    golden(
        &dir,
        "sample-empty",
        &d::Sample::default(),
        overlay,
        empty_want,
        d::write_sample,
        d::read_sample,
    );
    for sent in [short(), d::Sample::default()] {
        let mut want = expected;
        want.name[..sent.name_length as usize]
            .copy_from_slice(&sent.name[..sent.name_length as usize]);
        want.name_length = sent.name_length;
        want.token[..sent.token_length as usize]
            .copy_from_slice(&sent.token[..sent.token_length as usize]);
        want.token_length = sent.token_length;
        want.caps = sent.caps;
        let (bytes, bits) = write(&d::Choice::Sample(sent), d::write_choice);
        for initial in [
            d::Choice::Conditional(conditional),
            d::Choice::Sample(dirty()),
        ] {
            read(
                &bytes,
                bits,
                initial,
                d::Choice::Sample(want),
                d::read_choice,
            );
        }
    }
    let (bytes, bits) = write(
        &d::Choice::Conditional(d::Conditional {
            enabled: false,
            value: expected,
        }),
        d::write_choice,
    );
    for initial in [
        d::Choice::Sample(dirty()),
        d::Choice::Conditional(conditional),
    ] {
        read(
            &bytes,
            bits,
            initial,
            d::Choice::Conditional(d::Conditional::default()),
            d::read_choice,
        );
    }
    println!("packet defaults Rust: constructors, eight C++ goldens and reused storage OK");
}
