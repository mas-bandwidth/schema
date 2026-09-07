#[cfg(test)]
mod tests {
    use rustwire::*;

    fn hash(name: &str) -> u64 {
        name.bytes().fold(0xcbf29ce484222325, |h, b| {
            (h ^ u64::from(b)).wrapping_mul(0x100000001b3)
        })
    }

    fn wire(body: impl FnOnce(&mut TableWriter)) -> Vec<u8> {
        let mut bytes = vec![0; 4096];
        let mut ids = [0; 512];
        let mut writer = TableWriter::new(Some(&mut bytes), &mut ids);
        writer.put8(1);
        body(&mut writer);
        let size = writer.finish();
        assert!(size >= 0);
        bytes.truncate(size as usize);
        bytes
    }

    fn sample() -> Boundary {
        let mut value = Boundary::default();
        value.choices_count = 125;
        for i in 0..125 {
            value.choices[i] = Choice((i + 1) as u8);
        }
        value.frames_count = 2;
        value.frames[0].value = Choice::V126;
        value.frames[0].tail = 128;
        value.frames[1].value = Choice::V130;
        value.nested.value = Choice::V127;
        value.maybe_present = true;
        value.mask = 63;
        value.bounded = 0.5;
        value
    }

    #[test]
    fn nested_lengths_and_first_use_cross_127_against_cpp() {
        let expected = std::fs::read(concat!(
            env!("CARGO_MANIFEST_DIR"),
            "/../../build/rust-wire/cpp.bin"
        ))
        .unwrap();
        let count = u64::from_le_bytes(expected[expected.len() - 8..].try_into().unwrap());
        assert!(count > 128, "the fixture must cross the reference boundary");
        let value = sample();
        assert_eq!(boundary_measure(&value), expected.len() as i64);
        let mut actual = vec![0; expected.len()];
        assert_eq!(boundary_save(&value, &mut actual), expected.len() as i64);
        assert_eq!(actual, expected, "C++ and Rust disagree on the file bytes");
        let mut decoded = Boundary::default();
        let mut report = TableReport::default();
        assert_eq!(
            boundary_load_verdict(&mut decoded, &expected, &mut report),
            TableOpenVerdict::Ok
        );
        assert_eq!(decoded, value);
        assert!(!report.malformed);
        assert_eq!(
            (
                report.unknown,
                report.kind_mismatch,
                report.widened,
                report.clamped
            ),
            (0, 0, 0, 0)
        );
        for size in 0..expected.len() {
            assert_eq!(
                boundary_save(&value, &mut actual[..size]),
                -1,
                "short buffer {size}"
            );
        }
    }

    #[test]
    fn canonical_leb_boundaries_and_rollback() {
        for value in [0, 127, 128, 16383, 16384, u32::MAX as u64, u64::MAX] {
            let mut bytes = [0; 10];
            let mut ids = [];
            let mut writer = TableWriter::new(Some(&mut bytes), &mut ids);
            writer.putleb(value);
            let count = writer.offset;
            let mut reader = TableReader::new(&bytes[..count]);
            assert_eq!(reader.getleb(), Some(value));
            assert_eq!(reader.offset, count);
        }
        for bad in [
            &[0x80][..],
            &[0x80, 0],
            &[0x81, 0],
            &[0xff; 10],
            &[0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 2],
        ] {
            let mut reader = TableReader::new(bad);
            assert_eq!(reader.getleb(), None);
            assert_eq!(reader.offset, 0);
        }
    }

    #[test]
    fn zero_hash_is_an_identity_and_not_enum_none() {
        let bytes = wire(|w| {
            w.putid(0);
            w.put8(6);
            w.put8(42);
            w.putleb(0);
        });
        let mut reader = TableReader::open(&bytes).unwrap();
        assert_eq!(reader.getid(), Some(Some(0)));
        assert_eq!(reader.get8(), 6);
        assert_eq!(reader.get8(), 42);
        assert_eq!(reader.getid(), Some(None));
        let bytes = wire(|w| {
            w.putid(hash("maybe"));
            w.put8(30);
            w.putid(0);
            w.putleb(0);
        });
        let mut value = Boundary::default();
        let mut report = TableReport::default();
        assert!(boundary_load(&mut value, &bytes, &mut report));
        assert!(value.maybe_present);
        assert_eq!(value.maybe, Choice::NONE);
        assert_eq!(report.unknown, 1);
        assert_eq!(Choice::table_value(0), None);
        assert_eq!(
            boundary_table_type()
                .fields
                .iter()
                .find(|f| f.name == "maybe")
                .unwrap()
                .kind,
            30
        );
    }

    #[test]
    fn form_refusal_precedes_any_trailer_validation() {
        let mut value = sample();
        for form in [0, 2, 255] {
            let mut report = TableReport::default();
            assert_eq!(
                boundary_load_verdict(&mut value, &[form], &mut report),
                TableOpenVerdict::Refused
            );
            assert_eq!(report.verdict, TableOpenVerdict::Refused);
            assert!(!report.malformed);
            assert_eq!(value, Boundary::default());
        }
        let mut duplicate = vec![1, 0];
        duplicate.extend_from_slice(&0u64.to_le_bytes());
        duplicate.extend_from_slice(&0u64.to_le_bytes());
        duplicate.extend_from_slice(&2u64.to_le_bytes());
        assert!(matches!(
            TableReader::open(&duplicate),
            Err(TableOpenVerdict::Damaged)
        ));
        let mut gap = vec![1, 0, 42];
        gap.extend_from_slice(&0u64.to_le_bytes());
        assert!(matches!(
            TableReader::open(&gap),
            Err(TableOpenVerdict::Damaged)
        ));
    }

    #[test]
    fn truncated_repeat_keeps_the_previous_scalar_union() {
        let bytes = wire(|w| {
            w.putid(hash("selected"));
            w.put8(15);
            w.putid(hash("inner"));
            w.put8(13);
            w.putleb(1);
            w.putleb(0);
            w.putid(hash("selected"));
            w.put8(15);
            w.putid(hash("inner")); // the repeated arm has no kind or length
        });
        let mut value = Boundary::default();
        let mut report = TableReport::default();
        assert_eq!(
            boundary_load_verdict(&mut value, &bytes, &mut report),
            TableOpenVerdict::BodyStopped
        );
        assert!(report.malformed);
        assert_eq!(value.selected, Pick::Inner(Inner::default()));
    }

    #[test]
    fn array_header_and_elements_use_the_reference_bounds() {
        for kind in [30, 31] {
            let bytes = wire(|w| {
                w.putid(hash("choices"));
                w.put8(14);
                w.putleb(2);
                w.put8(kind);
                w.put8(0x80);
                // The reference parses count against the enclosing body;
                // this field reference terminates it, outside array L.
                w.putid(hash("mask"));
                w.put8(6);
                w.put8(42);
                w.putleb(0);
            });
            let mut value = Boundary::default();
            let mut report = TableReport::default();
            assert!(boundary_load(&mut value, &bytes, &mut report));
            assert_eq!(value.mask, 42);
            assert_eq!(value.choices_count, 0);
            if kind == 30 {
                assert!(report.malformed);
                assert_eq!(report.clamped, 1);
            } else {
                assert!(!report.malformed);
                assert_eq!(report.kind_mismatch, 1);
            }
        }
    }

    #[test]
    fn new_kinds_skip_their_lengths_and_clamps_match_the_reference() {
        for kind in [31, 32] {
            let bytes = wire(|w| {
                w.putid(hash("foreign"));
                w.put8(kind);
                w.putleb(3);
                w.raw(&[0x80, 0, 0xff]);
                w.putid(hash("mask"));
                w.put8(6);
                w.put8(0xff);
                w.putid(hash("bounded"));
                w.put8(10);
                w.put32(2f32.to_bits());
                w.putleb(0);
            });
            let mut value = Boundary::default();
            let mut report = TableReport::default();
            assert!(boundary_load(&mut value, &bytes, &mut report));
            assert!(!report.malformed);
            assert_eq!(report.unknown, 1);
            assert_eq!(report.clamped, 2);
            assert_eq!(value.mask, 63);
            assert_eq!(value.bounded, 1.0);
        }
    }

    #[test]
    fn widening_preserves_nan_payload_and_signalling_bit() {
        for bits in [0x7f800001u32, 0x7fc00123, 0xff800001, 0xffc12345] {
            let bytes = bits.to_le_bytes();
            let expected = ((u64::from(bits) & 0x80000000) << 32)
                | 0x7ff0000000000000
                | ((u64::from(bits) & 0x7fffff) << 29);
            assert_eq!(TableReader::new(&bytes).widened(10), Some(expected));
        }
    }
}
