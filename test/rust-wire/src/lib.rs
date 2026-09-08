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

#[cfg(test)]
mod arms {
    use rustarms::*;
    fn take<'a>(bytes: &mut &'a [u8]) -> &'a [u8] {
        let n = u32::from_le_bytes(bytes[..4].try_into().unwrap()) as usize;
        let result = &bytes[4..4 + n];
        *bytes = &bytes[4 + n..];
        result
    }
    fn json(value: &Root) -> Vec<u8> {
        let n = root_to_json_measure(value);
        assert!(n >= 0);
        let mut bytes = vec![0; n as usize];
        assert_eq!(root_to_json(value, &mut bytes), n);
        bytes
    }
    #[test]
    fn general_arms_and_optional_arrays_match_cpp() {
        let data = std::fs::read(concat!(
            env!("CARGO_MANIFEST_DIR"),
            "/../../build/rust-arms/cpp.bin"
        ))
        .unwrap();
        let mut data = data.as_slice();
        for text in include_str!("../arms.jsonl").lines() {
            let expected_wire = take(&mut data);
            let expected_json = take(&mut data);
            let counts: Vec<u32> = (0..4)
                .map(|_| u32::from_le_bytes(take(&mut data).try_into().unwrap()))
                .collect();
            let mut value = Root::default();
            let mut report = TableReport::default();
            assert!(
                root_from_json(&mut value, text.as_bytes(), &mut report),
                "{text}"
            );
            assert_eq!(
                [
                    report.unknown as u32,
                    report.kind_mismatch as u32,
                    report.clamped as u32,
                    report.duplicate as u32
                ],
                counts.as_slice(),
                "{text}"
            );
            assert_eq!(json(&value), expected_json, "{text}");
            assert_eq!(root_measure(&value), expected_wire.len() as i64, "{text}");
            let mut wire = vec![0; expected_wire.len()];
            assert_eq!(root_save(&value, &mut wire), wire.len() as i64);
            assert_eq!(wire, expected_wire, "{text}");
            let mut decoded = Root::default();
            let mut report = TableReport::default();
            assert!(root_load(&mut decoded, &wire, &mut report));
            assert!(!report.malformed);
            assert_eq!(json(&decoded), expected_json, "wire {text}");
        }
        assert!(data.is_empty());
    }
    #[test]
    fn malformed_general_arms_match_cpp() {
        let base = std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("../../build/rust-arms");
        let data = std::fs::read(base.join("cpp.bin")).unwrap();
        let mut data = data.as_slice();
        let mut mutations = Vec::new();
        while !data.is_empty() {
            let wire = take(&mut data);
            for _ in 0..5 {
                take(&mut data);
            }
            for length in 0..wire.len() {
                mutations.push(wire[..length].to_vec());
            }
            for i in 0..wire.len() {
                for mask in [1, 0x80, 0xff] {
                    let mut m = wire.to_vec();
                    m[i] ^= mask;
                    mutations.push(m);
                }
            }
        }
        let mut input = Vec::new();
        for m in &mutations {
            input.extend_from_slice(&(m.len() as u32).to_le_bytes());
            input.extend_from_slice(m);
        }
        std::fs::write(base.join("mutations.in"), input).unwrap();
        assert!(
            std::process::Command::new(base.join("reference"))
                .arg("--wire")
                .arg(base.join("mutations.in"))
                .arg(base.join("mutations.out"))
                .status()
                .unwrap()
                .success()
        );
        let output = std::fs::read(base.join("mutations.out")).unwrap();
        let mut output = output.as_slice();
        for (i, m) in mutations.iter().enumerate() {
            let counts: Vec<u32> = (0..7)
                .map(|_| u32::from_le_bytes(take(&mut output).try_into().unwrap()))
                .collect();
            let wire = take(&mut output);
            let mut value = Root::default();
            let mut report = TableReport::default();
            let ok = root_load(&mut value, m, &mut report);
            assert_eq!(
                [
                    report.unknown as u32,
                    report.kind_mismatch as u32,
                    report.widened as u32,
                    report.clamped as u32,
                    report.duplicate as u32,
                    report.malformed as u32,
                    ok as u32
                ],
                counts.as_slice(),
                "mutant {i}: {m:02x?}"
            );
            let mut expected = Root::default();
            root_load(&mut expected, wire, &mut TableReport::default());
            assert_eq!(
                root_measure(&value),
                wire.len() as i64,
                "mutant {i}: input {m:02x?}; actual {value:?}; expected {expected:?}"
            );
            let mut actual = vec![0; wire.len()];
            assert_eq!(root_save(&value, &mut actual), wire.len() as i64);
            assert_eq!(actual, wire, "mutant {i}: {m:02x?}");
        }
        assert!(output.is_empty());
    }

    #[test]
    fn optional_null_resets_storage_and_presence() {
        let mut value = Root::default();
        let mut report = TableReport::default();
        assert!(root_from_json(&mut value,br#"{"entries":[{"n":99}],"entries":null,"weights":[1,2],"weights":null,"grades":["Silver"],"grades":null}"#,&mut report));
        assert!(!value.entries_present && !value.weights_present && !value.grades_present);
        assert_eq!(value.entries_count, 0);
        assert_eq!(value.grades_count, 0);
        assert!(value.entries.iter().all(|v| v.n == 7));
        assert_eq!(value.weights, [0.0; 2]);
        assert_eq!(value.grades, [Grade::NONE; 3]);
    }
}

#[cfg(test)]
mod arena_tests {
    use graphdemo::*;
    fn corpus(name: &str) -> Vec<u8> {
        std::fs::read(format!(
            "{}/../../testdata/wire/tables/{name}.bin",
            env!("CARGO_MANIFEST_DIR")
        ))
        .unwrap()
    }
    #[test]
    fn pointer_corpus_round_trips_with_no_depth_limit() {
        for name in ["graph_empty", "graph_tree", "graph_shared", "graph_deep"] {
            let expected = corpus(name);
            let size = scene_load_measure(&expected).unwrap();
            let mut data =
                vec![core::mem::MaybeUninit::<u128>::zeroed(); size.data_bytes.max(1).div_ceil(16)];
            let mut directory = vec![TableNodeDirEntry::default(); size.directory_entries()];
            let mut report = TableReport::default();
            let region = scene_load(&mut data, &mut directory, &expected, &mut report).unwrap();
            assert_eq!(report, TableReport::default(), "{name}");
            let measured = scene_measure(region.root(), region.context()).unwrap();
            let mut actual = vec![0; measured as usize];
            assert_eq!(
                scene_save(region.root(), region.context(), &mut actual).unwrap(),
                measured
            );
            assert_eq!(actual, expected, "{name}");
            if name == "graph_shared" {
                let a = region.get(&region.root().head).unwrap();
                let b = region.get(&region.root().alias).unwrap();
                assert!(core::ptr::eq(a, b));
            }
            if name != "graph_deep" {
                let mut text =
                    vec![0; scene_to_json_measure(region.root(), region.context()) as usize];
                assert_eq!(
                    scene_to_json(region.root(), region.context(), &mut text),
                    text.len() as i64
                );
                let cpp = std::fs::read(format!(
                    "{}/../../testdata/conformance/tables/json/{name}.json",
                    env!("CARGO_MANIFEST_DIR")
                ))
                .unwrap();
                assert_eq!(text, cpp, "{name} text");
                let mut builder = SceneBuilder::new();
                let mut report = TableReport::default();
                assert!(scene_from_json(&mut builder, &text, &mut report));
                assert_eq!(report, TableReport::default());
                let owned = builder.lock().unwrap();
                let region = owned.region();
                let mut again = vec![0; text.len()];
                assert_eq!(
                    scene_to_json(region.root(), region.context(), &mut again),
                    text.len() as i64
                );
                assert_eq!(again, text);
            }
        }
    }
    #[test]
    fn malformed_node_table_kind_keeps_later_field_reports() {
        let mut bytes = vec![0; 128];
        let mut ids = [0; 16];
        let mut writer = TableWriter::new(Some(&mut bytes), &mut ids);
        writer.put8(1);
        writer.putid(u64::MAX);
        writer.put8(3);
        writer.put16(0);
        writer.putid(123);
        writer.put8(6);
        writer.put8(7);
        writer.putleb(0);
        let size = writer.finish();
        bytes.truncate(size as usize);
        let size = scene_load_measure(&bytes).unwrap();
        let mut data = vec![core::mem::MaybeUninit::<u128>::zeroed(); size.data_bytes.div_ceil(16)];
        let mut directory = vec![TableNodeDirEntry::default(); size.directory_entries()];
        let mut report = TableReport::default();
        scene_load(&mut data, &mut directory, &bytes, &mut report).unwrap();
        assert!(report.malformed);
        assert_eq!(report.unknown, 1);
    }
    #[test]
    fn arena_sharing_cycles_and_short_caller_buffers() {
        let mut builder = SceneBuilder::new();
        let node = builder.arena_mut().alloc::<ListNode>();
        builder.arena_mut().get_mut(node).unwrap().value = 37;
        builder.root_mut().head = node;
        builder.root_mut().alias = node;
        let owned = builder.lock().unwrap();
        assert!(core::ptr::eq(
            owned.get(&owned.root().head).unwrap(),
            owned.get(&owned.root().alias).unwrap()
        ));
        assert_eq!(owned.get(&owned.root().head).unwrap().value, 37);
        let mut cycle = TableBuilder::<ListNode>::new();
        cycle.root_mut().next = cycle.root_ref();
        assert_eq!(
            list_node_measure(cycle.root(), cycle.context()),
            Err(TableRefuseReason::DataCycle)
        );
        assert!(matches!(cycle.lock(), Err(TableRefuseReason::DataCycle)));
        let wire = corpus("graph_tree");
        let mut data = [];
        let mut directory = [];
        assert!(matches!(
            scene_load(
                &mut data,
                &mut directory,
                &wire,
                &mut TableReport::default()
            ),
            Err(TableLoadError::BufferTooSmall)
        ));
    }
}

#[cfg(test)]
mod graph_arms {
    use rustgraph::*;
    fn take<'a>(data: &mut &'a [u8]) -> &'a [u8] {
        let n = u32::from_le_bytes(data[..4].try_into().unwrap()) as usize;
        let out = &data[4..4 + n];
        *data = &data[4 + n..];
        out
    }
    #[test]
    fn pointer_arms_arrays_and_guards_match_cpp() {
        let data = std::fs::read(concat!(
            env!("CARGO_MANIFEST_DIR"),
            "/../../build/rust-graph/cpp.bin"
        ))
        .unwrap();
        let mut data = data.as_slice();
        for text in include_str!("../graph.jsonl").lines() {
            let wire = take(&mut data);
            let json = take(&mut data);
            let counts: Vec<_> = (0..4)
                .map(|_| u32::from_le_bytes(take(&mut data).try_into().unwrap()))
                .collect();
            let mut builder = RootBuilder::new();
            let mut report = TableReport::default();
            assert!(
                root_from_json(&mut builder, text.as_bytes(), &mut report),
                "{text}"
            );
            assert_eq!(
                [
                    report.unknown as u32,
                    report.kind_mismatch as u32,
                    report.clamped as u32,
                    report.duplicate as u32
                ],
                counts.as_slice(),
                "{text}"
            );
            let mut out =
                vec![0; root_measure(builder.root(), builder.context()).unwrap() as usize];
            assert_eq!(
                root_save(builder.root(), builder.context(), &mut out).unwrap(),
                out.len() as i64
            );
            assert_eq!(out, wire, "{text}");
            let owned = builder.lock().unwrap();
            let region = owned.region();
            let mut out = vec![0; root_to_json_measure(region.root(), region.context()) as usize];
            assert_eq!(
                root_to_json(region.root(), region.context(), &mut out),
                out.len() as i64
            );
            assert_eq!(out, json, "{text}");
            let size = root_load_measure(wire).unwrap();
            let mut storage =
                vec![core::mem::MaybeUninit::<u128>::zeroed(); size.data_bytes.div_ceil(16)];
            let mut dir = vec![TableNodeDirEntry::default(); size.directory_entries()];
            let mut report = TableReport::default();
            let region = root_load(&mut storage, &mut dir, wire, &mut report).unwrap();
            assert_eq!(report, TableReport::default(), "{text}");
            out.resize(json.len(), 0);
            assert_eq!(
                root_to_json(region.root(), region.context(), &mut out),
                out.len() as i64
            );
            assert_eq!(out, json);
            let mut again = vec![0; wire.len()];
            assert_eq!(
                root_save(region.root(), region.context(), &mut again).unwrap(),
                wire.len() as i64
            );
            assert_eq!(again, wire);
        }
    }
}

#[cfg(test)]
mod blob_tests {
    use blobdemo::*;
    fn save(root: &Catalog, context: TableContext<'_>) -> Vec<u8> {
        let mut bytes = vec![0; catalog_measure(root, context).unwrap() as usize];
        assert_eq!(
            catalog_save(root, context, &mut bytes).unwrap(),
            bytes.len() as i64
        );
        bytes
    }
    #[test]
    fn shared_large_and_empty_blobs_keep_identity_and_owner_lifetimes() {
        let payload: Vec<u8> = (0..70001).map(|i| i as u8).collect();
        let mut builder = CatalogBuilder::new();
        let blob = builder.arena_mut().alloc_bytes(&payload).unwrap();
        let empty = builder.arena_mut().alloc_string("").unwrap();
        builder.root_mut().thumb = blob;
        builder.root_mut().alias = blob;
        builder.root_mut().note = empty;
        assert_eq!(builder.arena().bytes(blob), Some(payload.as_slice()));
        assert_eq!(builder.arena().string(empty), Some(""));
        assert!(builder.arena_mut().alloc_string("x\0y").is_err());
        assert_eq!(
            catalog_to_json_measure(builder.root(), builder.context()),
            -1
        );
        let wire = save(builder.root(), builder.context());
        let owned = builder.lock().unwrap();
        assert_eq!(owned.bytes(&owned.root().thumb), Some(payload.as_slice()));
        assert!(core::ptr::eq(
            owned.bytes(&owned.root().thumb).unwrap().as_ptr(),
            owned.bytes(&owned.root().alias).unwrap().as_ptr()
        ));
        assert_eq!(owned.string(&owned.root().note), Some(""));
        let view = owned.region();
        assert_eq!(save(view.root(), view.context()), wire);
        let size = catalog_load_measure(&wire).unwrap();
        let mut data = vec![core::mem::MaybeUninit::<u128>::zeroed(); size.data_bytes.div_ceil(16)];
        let mut directory = vec![TableNodeDirEntry::default(); size.directory_entries()];
        let mut report = TableReport::default();
        let view = catalog_load(&mut data, &mut directory, &wire, &mut report).unwrap();
        assert_eq!(report, TableReport::default());
        assert_eq!(view.bytes(&view.root().thumb), Some(payload.as_slice()));
        assert_eq!(view.string(&view.root().note), Some(""));
        assert_eq!(save(view.root(), view.context()), wire);
    }
    #[test]
    fn invalid_string_node_nulls_every_reference_without_poisoning_bytes() {
        let mut builder = CatalogBuilder::new();
        let text = builder.arena_mut().alloc_string("unique-marker").unwrap();
        let asset = builder.arena_mut().alloc::<Asset>();
        builder.arena_mut().get_mut(asset).unwrap().caption = text;
        builder.root_mut().note = text;
        builder.root_mut().head = asset;
        let bytes = builder.arena_mut().alloc_bytes(b"raw").unwrap();
        builder.root_mut().thumb = bytes;
        let mut wire = save(builder.root(), builder.context());
        let at = wire
            .windows(13)
            .position(|w| w == b"unique-marker")
            .unwrap();
        wire[at] = 0xff;
        let size = catalog_load_measure(&wire).unwrap();
        let mut data = vec![core::mem::MaybeUninit::<u128>::zeroed(); size.data_bytes.div_ceil(16)];
        let mut directory = vec![TableNodeDirEntry::default(); size.directory_entries()];
        let mut report = TableReport::default();
        let region = catalog_load(&mut data, &mut directory, &wire, &mut report).unwrap();
        assert!(report.malformed);
        assert_eq!(report.kind_mismatch, 0);
        assert_eq!(report.unknown, 0);
        assert!(region.root().note.is_null());
        assert!(region.get(&region.root().head).unwrap().caption.is_null());
        assert_eq!(region.bytes(&region.root().thumb), Some(b"raw".as_slice()));
    }
}

#[cfg(test)]
mod list_tests {
    #[test]
    fn deletion_preserves_order_and_segment_addresses() {
        use rustlist::*;
        let mut builder = RowBuilder::new();
        let mut values = builder.root().values;
        let mut handles = Vec::new();
        for i in 0..130 {
            let handle = builder.arena_mut().list_add(&mut values).unwrap();
            *builder.arena_mut().element_mut(handle).unwrap() = i;
            handles.push(handle);
        }
        let before = builder.arena().element(handles[0]).unwrap() as *const i32 as usize;
        assert!(builder.arena_mut().list_erase(&mut values, handles[32]));
        assert!(!builder.arena_mut().list_erase(&mut values, handles[32]));
        assert!(builder.arena_mut().list_erase(&mut values, handles[0]));
        let new = builder.arena_mut().list_add(&mut values).unwrap();
        *builder.arena_mut().element_mut(new).unwrap() = 130;
        assert_ne!(
            before,
            builder.arena().element(new).unwrap() as *const i32 as usize
        );
        assert!(builder.arena().element(handles[32]).is_none());
        builder.root_mut().values = values;
        let expected: Vec<i32> = (1..=130).filter(|&i| i != 32).collect();
        assert_eq!(
            builder
                .context()
                .list(&builder.root().values)
                .unwrap()
                .copied()
                .collect::<Vec<_>>(),
            expected
        );
        let owned = builder.lock().expect("lock builder");
        let region = owned.region();
        let view = region.context().list(&region.root().values).unwrap();
        assert_eq!(view.get(0), Some(&1));
        assert_eq!(view.get(usize::MAX), None);
        assert_eq!(view.copied().collect::<Vec<_>>(), expected);
    }

    use rustlist::*;
    fn take<'a>(data: &mut &'a [u8]) -> &'a [u8] {
        let n = u32::from_le_bytes(data[..4].try_into().unwrap()) as usize;
        let out = &data[4..4 + n];
        *data = &data[4 + n..];
        out
    }
    #[test]
    fn pointer_arms_arrays_and_guards_match_cpp() {
        let data = std::fs::read(concat!(
            env!("CARGO_MANIFEST_DIR"),
            "/../../build/rust-list/cpp.bin"
        ))
        .unwrap();
        let mut data = data.as_slice();
        for text in include_str!("../list.jsonl").lines() {
            let wire = take(&mut data);
            let json = take(&mut data);
            let counts: Vec<_> = (0..4)
                .map(|_| u32::from_le_bytes(take(&mut data).try_into().unwrap()))
                .collect();
            let mut builder = RootBuilder::new();
            let mut report = TableReport::default();
            assert!(
                root_from_json(&mut builder, text.as_bytes(), &mut report),
                "{text}"
            );
            assert_eq!(
                [
                    report.unknown as u32,
                    report.kind_mismatch as u32,
                    report.clamped as u32,
                    report.duplicate as u32
                ],
                counts.as_slice(),
                "{text}"
            );
            let mut out =
                vec![0; root_measure(builder.root(), builder.context()).unwrap() as usize];
            assert_eq!(
                root_save(builder.root(), builder.context(), &mut out).unwrap(),
                out.len() as i64
            );
            assert_eq!(out, wire, "{text}");
            let owned = builder.lock().unwrap();
            let region = owned.region();
            let mut out = vec![0; root_to_json_measure(region.root(), region.context()) as usize];
            assert_eq!(
                root_to_json(region.root(), region.context(), &mut out),
                out.len() as i64
            );
            assert_eq!(out, json, "{text}");
            let size = root_load_measure(wire).unwrap();
            let mut storage =
                vec![core::mem::MaybeUninit::<u128>::zeroed(); size.data_bytes.div_ceil(16)];
            let mut dir = vec![TableNodeDirEntry::default(); size.directory_entries()];
            let mut report = TableReport::default();
            let region = root_load(&mut storage, &mut dir, wire, &mut report).unwrap();
            assert_eq!(report, TableReport::default(), "{text}");
            out.resize(json.len(), 0);
            assert_eq!(
                root_to_json(region.root(), region.context(), &mut out),
                out.len() as i64
            );
            assert_eq!(out, json);
            let mut again = vec![0; wire.len()];
            assert_eq!(
                root_save(region.root(), region.context(), &mut again).unwrap(),
                wire.len() as i64
            );
            assert_eq!(again, wire);
        }
    }
}

#[cfg(test)]
mod container_pins {
    macro_rules! check {
        ($test:ident,$krate:ident,$root:ident,$name:literal,$load_measure:path,$load:path,$measure:path,$save:path,$from_json:path,$json_measure:path,$json:path,$load_builder:path) => {
            #[test]
            fn $test() {
                use $krate::*;
                let wire = include_bytes!(concat!("../../../testdata/wire/tables/", $name, ".bin"));
                let size = $load_measure(wire).unwrap();
                let mut data =
                    vec![core::mem::MaybeUninit::<u128>::zeroed(); size.data_bytes.div_ceil(16)];
                let mut dir = vec![TableNodeDirEntry::default(); size.directory_entries()];
                let mut report = TableReport::default();
                let region = $load(&mut data, &mut dir, wire, &mut report).unwrap();
                assert_eq!(report, TableReport::default());
                let mut out = vec![
                    0;
                    $measure(region.root(), region.context()).expect("measure loaded")
                        as usize
                ];
                assert_eq!(
                    $save(region.root(), region.context(), &mut out).expect("save region"),
                    wire.len() as i64
                );
                assert_eq!(out, wire);
                let n = $json_measure(region.root(), region.context());
                assert!(n >= 0);
                let mut text = vec![0; n as usize];
                assert_eq!($json(region.root(), region.context(), &mut text), n);
                let mut builder = TableBuilder::<$root>::new();
                assert!(
                    $from_json(&mut builder, &text, &mut report),
                    "{}",
                    String::from_utf8_lossy(&text)
                );
                assert_eq!(report, TableReport::default());
                assert_eq!(
                    $save(builder.root(), builder.context(), &mut out).expect("save builder"),
                    wire.len() as i64
                );
                assert_eq!(out, wire);
                let owned = builder.lock().expect("lock builder");
                let region = owned.region();
                assert_eq!(
                    $save(region.root(), region.context(), &mut out).expect("save locked"),
                    wire.len() as i64
                );
                assert_eq!(out, wire);
                let mut editable = TableBuilder::<$root>::new();
                $load_builder(&mut editable, wire, &mut report).expect("load builder");
                assert_eq!(report, TableReport::default());
                assert_eq!(
                    $save(editable.root(), editable.context(), &mut out)
                        .expect("save loaded builder"),
                    wire.len() as i64
                );
                assert_eq!(out, wire);
                let owned = editable.lock().unwrap();
                let region = owned.region();
                assert_eq!(
                    $save(region.root(), region.context(), &mut out).expect("save loaded locked"),
                    wire.len() as i64
                );
                assert_eq!(out, wire);
            }
        };
    }
    check!(
        map_full,
        mapdemo,
        Fleet,
        "map_full",
        mapdemo::fleet_load_measure,
        mapdemo::fleet_load,
        mapdemo::fleet_measure,
        mapdemo::fleet_save,
        mapdemo::fleet_from_json,
        mapdemo::fleet_to_json_measure,
        mapdemo::fleet_to_json,
        mapdemo::fleet_load_builder
    );
    check!(
        map_empty,
        mapdemo,
        Fleet,
        "map_empty",
        mapdemo::fleet_load_measure,
        mapdemo::fleet_load,
        mapdemo::fleet_measure,
        mapdemo::fleet_save,
        mapdemo::fleet_from_json,
        mapdemo::fleet_to_json_measure,
        mapdemo::fleet_to_json,
        mapdemo::fleet_load_builder
    );
    check!(
        map_depth,
        mapdemo,
        Depth,
        "map_depth",
        mapdemo::depth_load_measure,
        mapdemo::depth_load,
        mapdemo::depth_measure,
        mapdemo::depth_save,
        mapdemo::depth_from_json,
        mapdemo::depth_to_json_measure,
        mapdemo::depth_to_json,
        mapdemo::depth_load_builder
    );
    check!(
        map_text,
        mapdemo,
        Text,
        "map_text",
        mapdemo::text_load_measure,
        mapdemo::text_load,
        mapdemo::text_measure,
        mapdemo::text_save,
        mapdemo::text_from_json,
        mapdemo::text_to_json_measure,
        mapdemo::text_to_json,
        mapdemo::text_load_builder
    );
    check!(
        map_cells,
        mapdemo,
        Cells,
        "map_cells",
        mapdemo::cells_load_measure,
        mapdemo::cells_load,
        mapdemo::cells_measure,
        mapdemo::cells_save,
        mapdemo::cells_from_json,
        mapdemo::cells_to_json_measure,
        mapdemo::cells_to_json,
        mapdemo::cells_load_builder
    );
    check!(
        map_runs,
        mapdemo,
        Runs,
        "map_runs",
        mapdemo::runs_load_measure,
        mapdemo::runs_load,
        mapdemo::runs_measure,
        mapdemo::runs_save,
        mapdemo::runs_from_json,
        mapdemo::runs_to_json_measure,
        mapdemo::runs_to_json,
        mapdemo::runs_load_builder
    );
    check!(
        map_chunks,
        mapdemo,
        Chunks,
        "map_chunks",
        mapdemo::chunks_load_measure,
        mapdemo::chunks_load,
        mapdemo::chunks_measure,
        mapdemo::chunks_save,
        mapdemo::chunks_from_json,
        mapdemo::chunks_to_json_measure,
        mapdemo::chunks_to_json,
        mapdemo::chunks_load_builder
    );
    check!(
        map_docs,
        mapdemo,
        Docs,
        "map_docs",
        mapdemo::docs_load_measure,
        mapdemo::docs_load,
        mapdemo::docs_measure,
        mapdemo::docs_save,
        mapdemo::docs_from_json,
        mapdemo::docs_to_json_measure,
        mapdemo::docs_to_json,
        mapdemo::docs_load_builder
    );
    check!(
        map_spans,
        mapdemo,
        Spans,
        "map_spans",
        mapdemo::spans_load_measure,
        mapdemo::spans_load,
        mapdemo::spans_measure,
        mapdemo::spans_save,
        mapdemo::spans_from_json,
        mapdemo::spans_to_json_measure,
        mapdemo::spans_to_json,
        mapdemo::spans_load_builder
    );
    check!(
        map_slots,
        mapdemo,
        Slots,
        "map_slots",
        mapdemo::slots_load_measure,
        mapdemo::slots_load,
        mapdemo::slots_measure,
        mapdemo::slots_save,
        mapdemo::slots_from_json,
        mapdemo::slots_to_json_measure,
        mapdemo::slots_to_json,
        mapdemo::slots_load_builder
    );
    check!(
        map_pairs,
        mapdemo,
        Pairs,
        "map_pairs",
        mapdemo::pairs_load_measure,
        mapdemo::pairs_load,
        mapdemo::pairs_measure,
        mapdemo::pairs_save,
        mapdemo::pairs_from_json,
        mapdemo::pairs_to_json_measure,
        mapdemo::pairs_to_json,
        mapdemo::pairs_load_builder
    );
    check!(
        map_crews,
        mapdemo,
        Crews,
        "map_crews",
        mapdemo::crews_load_measure,
        mapdemo::crews_load,
        mapdemo::crews_measure,
        mapdemo::crews_save,
        mapdemo::crews_from_json,
        mapdemo::crews_to_json_measure,
        mapdemo::crews_to_json,
        mapdemo::crews_load_builder
    );
    check!(
        map_trails,
        mapdemo,
        Trails,
        "map_trails",
        mapdemo::trails_load_measure,
        mapdemo::trails_load,
        mapdemo::trails_measure,
        mapdemo::trails_save,
        mapdemo::trails_from_json,
        mapdemo::trails_to_json_measure,
        mapdemo::trails_to_json,
        mapdemo::trails_load_builder
    );
    check!(
        list_tables,
        listdemo,
        Save,
        "list_tables",
        listdemo::save_load_measure,
        listdemo::save_load,
        listdemo::save_measure,
        listdemo::save_save,
        listdemo::save_from_json,
        listdemo::save_to_json_measure,
        listdemo::save_to_json,
        listdemo::save_load_builder
    );
    check!(
        list_scalars,
        listdemo,
        Save,
        "list_scalars",
        listdemo::save_load_measure,
        listdemo::save_load,
        listdemo::save_measure,
        listdemo::save_save,
        listdemo::save_from_json,
        listdemo::save_to_json_measure,
        listdemo::save_to_json,
        listdemo::save_load_builder
    );
    check!(
        list_empty,
        listdemo,
        Save,
        "list_empty",
        listdemo::save_load_measure,
        listdemo::save_load,
        listdemo::save_measure,
        listdemo::save_save,
        listdemo::save_from_json,
        listdemo::save_to_json_measure,
        listdemo::save_to_json,
        listdemo::save_load_builder
    );
    check!(
        list_erased,
        listdemo,
        Save,
        "list_erased",
        listdemo::save_load_measure,
        listdemo::save_load,
        listdemo::save_measure,
        listdemo::save_save,
        listdemo::save_from_json,
        listdemo::save_to_json_measure,
        listdemo::save_to_json,
        listdemo::save_load_builder
    );
    check!(
        list_mixed,
        listdemo,
        Mixed,
        "list_mixed",
        listdemo::mixed_load_measure,
        listdemo::mixed_load,
        listdemo::mixed_measure,
        listdemo::mixed_save,
        listdemo::mixed_from_json,
        listdemo::mixed_to_json_measure,
        listdemo::mixed_to_json,
        listdemo::mixed_load_builder
    );
    check!(
        list_shared,
        listdemo,
        Album,
        "list_shared",
        listdemo::album_load_measure,
        listdemo::album_load,
        listdemo::album_measure,
        listdemo::album_save,
        listdemo::album_from_json,
        listdemo::album_to_json_measure,
        listdemo::album_to_json,
        listdemo::album_load_builder
    );
    check!(
        list_before_pointer,
        listdemo,
        Album,
        "list_before_pointer",
        listdemo::album_load_measure,
        listdemo::album_load,
        listdemo::album_measure,
        listdemo::album_save,
        listdemo::album_from_json,
        listdemo::album_to_json_measure,
        listdemo::album_to_json,
        listdemo::album_load_builder
    );
    check!(
        list_nested,
        listdemo,
        Sheet,
        "list_nested",
        listdemo::sheet_load_measure,
        listdemo::sheet_load,
        listdemo::sheet_measure,
        listdemo::sheet_save,
        listdemo::sheet_from_json,
        listdemo::sheet_to_json_measure,
        listdemo::sheet_to_json,
        listdemo::sheet_load_builder
    );
    check!(
        list_of_maps,
        listdemo,
        Army,
        "list_of_maps",
        listdemo::army_load_measure,
        listdemo::army_load,
        listdemo::army_measure,
        listdemo::army_save,
        listdemo::army_from_json,
        listdemo::army_to_json_measure,
        listdemo::army_to_json,
        listdemo::army_load_builder
    );
    check!(
        list_migrates,
        listdemo,
        Unbounded,
        "list_migrates",
        listdemo::unbounded_load_measure,
        listdemo::unbounded_load,
        listdemo::unbounded_measure,
        listdemo::unbounded_save,
        listdemo::unbounded_from_json,
        listdemo::unbounded_to_json_measure,
        listdemo::unbounded_to_json,
        listdemo::unbounded_load_builder
    );
}

#[cfg(test)]
mod map_tests {
    use mapdemo::*;
    fn take<'a>(data: &mut &'a [u8]) -> &'a [u8] {
        let n = u32::from_le_bytes(data[..4].try_into().unwrap()) as usize;
        let out = &data[4..4 + n];
        *data = &data[4 + n..];
        out
    }
    #[test]
    fn keys_keep_stable_identity_across_replace_erase_and_growth() {
        let mut builder = RowBuilder::new();
        let mut map = builder.root().entries;
        let first = builder
            .arena_mut()
            .map_emplace(&mut map, "first".into())
            .unwrap();
        builder.arena_mut().map_value_mut(first).unwrap().count = 12;
        let address = builder.arena().map_entry(first).unwrap() as *const _ as usize;
        for i in 0..100 {
            builder
                .arena_mut()
                .map_emplace(&mut map, format!("k{i}").as_str().into())
                .unwrap();
        }
        let replaced = builder
            .arena_mut()
            .map_emplace(&mut map, "first".into())
            .unwrap();
        assert_eq!(first, replaced);
        assert_eq!(builder.arena().map_entry(first).unwrap().value.count, 0);
        assert_eq!(
            builder.arena().map_entry(first).unwrap() as *const _ as usize,
            address
        );
        assert!(builder.arena_mut().map_erase(&mut map, "first".into()));
        assert!(!builder.arena_mut().map_erase(&mut map, "first".into()));
        assert!(builder.arena().map_entry(first).is_none());
        let again = builder
            .arena_mut()
            .map_emplace(&mut map, "first".into())
            .unwrap();
        assert_ne!(first, again);
        builder.arena_mut().map_value_mut(again).unwrap().count = 27;
        builder.root_mut().entries = map;
        let owner = builder.lock().unwrap();
        let region = owner.region();
        let view = region.context().map(&region.root().entries).unwrap();
        assert_eq!(view.len(), 101);
        assert_eq!(view.get("first".into()).unwrap().value.count, 27);
        assert!(view.get("absent".into()).is_none());
    }
    #[test]
    fn map_identity_and_recovery_match_cpp() {
        let data = std::fs::read(concat!(
            env!("CARGO_MANIFEST_DIR"),
            "/../../build/rust-maps/cpp.bin"
        ))
        .unwrap();
        let mut data = data.as_slice();
        macro_rules! run {
            ($root:ty,$from:path,$measure:path,$save:path,$lm:path,$load:path,$jm:path,$json:path,$text:expr) => {{
                let counts: Vec<_> = (0..7)
                    .map(|_| u32::from_le_bytes(take(&mut data).try_into().unwrap()))
                    .collect();
                let wire = take(&mut data);
                let json = take(&mut data);
                let mut report = TableReport::default();
                let (mut encoded, mut printed) = (Vec::new(), Vec::new());
                let ok;
                if let Some(hex) = $text.strip_prefix("wire:") {
                    let bytes: Vec<_> = hex
                        .as_bytes()
                        .chunks(2)
                        .map(|v| u8::from_str_radix(core::str::from_utf8(v).unwrap(), 16).unwrap())
                        .collect();
                    if let Ok(size) = $lm(&bytes) {
                        let mut storage = vec![
                            core::mem::MaybeUninit::<u128>::zeroed();
                            size.data_bytes.div_ceil(16)
                        ];
                        let mut dir = vec![TableNodeDirEntry::default(); size.directory_entries()];
                        let region = $load(&mut storage, &mut dir, &bytes, &mut report).unwrap();
                        ok = true;
                        encoded.resize(
                            $measure(region.root(), region.context()).unwrap() as usize,
                            0,
                        );
                        $save(region.root(), region.context(), &mut encoded).unwrap();
                        printed.resize($jm(region.root(), region.context()) as usize, 0);
                        $json(region.root(), region.context(), &mut printed);
                    } else {
                        ok = false;
                    }
                } else {
                    let mut builder = TableBuilder::<$root>::new();
                    ok = $from(&mut builder, $text.as_bytes(), &mut report);
                    if ok {
                        encoded.resize(
                            $measure(builder.root(), builder.context()).unwrap() as usize,
                            0,
                        );
                        $save(builder.root(), builder.context(), &mut encoded).unwrap();
                        printed.resize($jm(builder.root(), builder.context()) as usize, 0);
                        $json(builder.root(), builder.context(), &mut printed);
                    }
                }
                assert_eq!(
                    [
                        ok as u32,
                        report.unknown as u32,
                        report.kind_mismatch as u32,
                        report.widened as u32,
                        report.clamped as u32,
                        report.duplicate as u32,
                        report.malformed as u32
                    ],
                    counts.as_slice(),
                    "{}",
                    $text
                );
                assert_eq!(encoded, wire, "wire {}", $text);
                assert_eq!(printed, json, "json {}", $text);
            }};
        }
        for line in include_str!("../maps.cases").lines() {
            let (root, text) = line.split_once('\t').unwrap();
            match root {
                "Row" => run!(
                    Row,
                    row_from_json,
                    row_measure,
                    row_save,
                    row_load_measure,
                    row_load,
                    row_to_json_measure,
                    row_to_json,
                    text
                ),
                "WideRow" => run!(
                    WideRow,
                    wide_row_from_json,
                    wide_row_measure,
                    wide_row_save,
                    wide_row_load_measure,
                    wide_row_load,
                    wide_row_to_json_measure,
                    wide_row_to_json,
                    text
                ),
                "EdgeRow" => run!(
                    EdgeRow,
                    edge_row_from_json,
                    edge_row_measure,
                    edge_row_save,
                    edge_row_load_measure,
                    edge_row_load,
                    edge_row_to_json_measure,
                    edge_row_to_json,
                    text
                ),
                _ => panic!(),
            }
        }
        assert!(data.is_empty());
    }
}

#[cfg(test)]
mod builder_tests {
    fn leb(mut n: u64) -> Vec<u8> {
        let mut out = Vec::new();
        while n >= 128 {
            out.push((n as u8 & 127) | 128);
            n >>= 7;
        }
        out.push(n as u8);
        out
    }
    fn wire(body: &[u8], ids: &[u64]) -> Vec<u8> {
        let mut out = vec![1];
        out.extend_from_slice(body);
        out.push(0);
        for id in ids {
            out.extend(id.to_le_bytes());
        }
        out.extend((ids.len() as u64).to_le_bytes());
        out
    }
    #[test]
    fn unbounded_partial_body_keeps_prefix_and_later_fields() {
        use listdemo::*;
        let mut payload = vec![4, 3];
        payload.extend(11i32.to_le_bytes());
        payload.extend(22i32.to_le_bytes());
        let mut body = vec![1, 14];
        body.extend(leb(payload.len() as u64));
        body.extend(payload);
        body.extend([2, 4, 42, 0, 0, 0]);
        let file = wire(
            &body,
            &ints_table_type()
                .fields
                .iter()
                .map(|f| f.id)
                .collect::<Vec<_>>(),
        );
        assert_eq!(
            ints_load_measure(&file),
            Err(TableLoadError::Refused(TableRefuseReason::CountOverLength))
        );
        let mut builder = IntsBuilder::new();
        let mut report = TableReport::default();
        ints_load_builder(&mut builder, &file, &mut report).unwrap();
        assert!(report.malformed);
        assert_eq!(report.clamped, 0);
        assert_eq!(builder.root().after, 42);
        assert_eq!(
            builder
                .context()
                .list(&builder.root().values)
                .unwrap()
                .copied()
                .collect::<Vec<_>>(),
            [11, 22]
        );
    }
    #[test]
    fn nested_count_cap_discards_builder_without_new_counter() {
        use listdemo::*;
        // Sheet.rows[0].items carries the cap refusal. An earlier unknown
        // field proves the existing report survives; a later one stays unread.
        let mut list = vec![13];
        list.extend(leb(1 << 31));
        let mut row = vec![2, 14];
        row.extend(leb(list.len() as u64));
        row.extend(list);
        row.push(0);
        let mut rows = vec![13, 1];
        rows.extend(leb(row.len() as u64));
        rows.extend(row);
        let mut body = vec![4, 4, 7, 0, 0, 0, 1, 14];
        body.extend(leb(rows.len() as u64));
        body.extend(rows);
        body.extend([4, 4, 8, 0, 0, 0]);
        let file = wire(
            &body,
            &[
                sheet_table_type().fields[0].id,
                row_table_type().fields[0].id,
                0,
                0xf00d,
            ],
        );
        let mut builder = SheetBuilder::new();
        let mut report = TableReport::default();
        assert_eq!(
            sheet_load_builder(&mut builder, &file, &mut report),
            Err(TableLoadError::Refused(
                TableRefuseReason::CountOverExtentCap
            ))
        );
        assert_eq!(report.unknown, 1);
        assert!(!report.malformed);
        assert!(builder.root().rows.is_empty());
    }
    #[test]
    fn invalid_text_restores_declared_default_and_reads_sibling() {
        use rustarms::*;
        let ids = [
            root_table_type()
                .fields
                .iter()
                .find(|f| f.name == "label")
                .unwrap()
                .id,
            root_table_type()
                .fields
                .iter()
                .find(|f| f.name == "tail")
                .unwrap()
                .id,
        ];
        let file = wire(&[1, 12, 2, b'x', 0, 2, 8, 42, 0, 0, 0], &ids);
        let mut root = Root::default();
        let mut report = TableReport::default();
        assert!(root_load(&mut root, &file, &mut report));
        assert!(report.malformed);
        assert_eq!(root.label_length, 5);
        assert_eq!(&root.label[..5], b"hello");
        assert_eq!(root.tail, 42);
    }
}

#[cfg(test)]
mod read_view_tests {
    #[test]
    fn directory_can_be_released_before_reading_shared_map_nodes() {
        use mapdemo::*;
        let wire = include_bytes!("../../../testdata/wire/tables/map_full.bin");
        let size = fleet_load_measure(wire).unwrap();
        let mut data = vec![TableStorageWord::zeroed(); size.data_bytes.div_ceil(16)];
        let mut directory = vec![TableNodeDirEntry::default(); size.directory_entries()];
        let mut report = TableReport::default();
        let view = fleet_load(&mut data, &mut directory, wire, &mut report)
            .unwrap()
            .release_attribution();
        drop(directory);
        assert_eq!(report, TableReport::default());
        assert!(view.get(&view.root().flagship).is_some());
        let map = view.context().map(&view.root().by_id).unwrap();
        assert!(!map.is_empty());
        let mut out = vec![0; fleet_measure(view.root(), view.context()).unwrap() as usize];
        fleet_save(view.root(), view.context(), &mut out).unwrap();
        assert_eq!(out, wire);
    }
}

#[cfg(test)]
mod worker_tests {
    use rustlist::*;

    #[test]
    fn workers_keep_nodes_and_element_handles_through_join_and_lock() {
        let mut builder = RootBuilder::new();
        let threads: Vec<_> = (0..4)
            .map(|worker_index| {
                let mut worker = builder.arena().worker();
                std::thread::spawn(move || {
                    let first = worker.alloc::<Row>();
                    let address = worker.get(first).unwrap() as *const Row as usize;
                    let mut values = TableList::default();
                    let element = worker.list_add(&mut values).unwrap();
                    *worker.element_mut(element).unwrap() = worker_index;
                    for n in 0..2300 {
                        let node = worker.alloc::<Row>();
                        worker.get_mut(node).unwrap().next = first;
                        let e = worker.list_add(&mut values).unwrap();
                        *worker.element_mut(e).unwrap() = n;
                    }
                    worker.get_mut(first).unwrap().values = values;
                    (worker, first, element, address)
                })
            })
            .collect();
        let mut refs = TableList::default();
        for (worker_index, thread) in threads.into_iter().enumerate() {
            let (worker, first, element, address) = thread.join().unwrap();
            assert!(builder.arena().get(first).is_none());
            builder.arena_mut().join(worker);
            assert_eq!(
                builder.arena().get(first).unwrap() as *const Row as usize,
                address
            );
            assert_eq!(
                *builder.arena().element(element).unwrap(),
                worker_index as i32
            );
            *builder.arena_mut().element_mut(element).unwrap() += 10;
            let slot = builder.arena_mut().list_add(&mut refs).unwrap();
            *builder.arena_mut().element_mut(slot).unwrap() = first;
        }
        builder.root_mut().refs = refs;
        let region = builder.lock().unwrap();
        for (worker_index, reference) in region
            .context()
            .list(&region.root().refs)
            .unwrap()
            .enumerate()
        {
            let row = region.get(reference).unwrap();
            let values = region.context().list(&row.values).unwrap();
            assert_eq!(values.len(), 2301);
            assert_eq!(*values.get(0).unwrap(), worker_index as i32 + 10);
            assert_eq!(*values.get(2300).unwrap(), 2299);
        }
    }
}
