use std::fs;

use crate::{from_json, load, parse, render, to_json, MAX_BODY_BYTES, MAX_LIST_ENTRIES};

fn fixture(name: &str) -> String {
    let root = if fs::metadata("tables").is_ok() {
        ""
    } else {
        "../../"
    };
    format!("{root}tables/cards/fuzz/{name}")
}

fn conformance(path: &str) -> String {
    let root = if fs::metadata("tables").is_ok() {
        ""
    } else {
        "../../"
    };
    format!("{root}tables/cards/conformance/{path}")
}

#[test]
fn card_round_trips_the_populated_fixture() {
    let wire = fs::read(fixture("card-populated.wire")).unwrap();
    let json = fs::read_to_string(fixture("card-populated.json")).unwrap();

    let (record, _report) = load(&wire).unwrap();
    let (out, _) = to_json(&record).unwrap();
    assert_eq!(out.as_bytes(), json.as_bytes());

    let expected_body = "body with multibyte →世界 and \"escapes\"\nand tabs\t";
    let body = record.card().body.as_ref().unwrap();
    assert_eq!(body.as_ref(), expected_body);

    let record2 = from_json(&json).unwrap();
    assert_eq!(record2.wire(), wire);

    let (out2, _) = to_json(&record2).unwrap();
    assert_eq!(out2.as_bytes(), json.as_bytes());

    let mut cannon = wire.clone();
    let (record3, _) = load(&cannon).unwrap();
    let w0 = record3.wire();
    cannon[0] ^= 0xff;
    assert_eq!(record3.wire(), w0);
    let mut wres = record3.wire();
    wres[0] ^= 0xff;
    assert_eq!(record3.wire(), w0);
}

#[test]
fn card_bounds_refuse_before_publishing() {
    let ok = format!(
        "{{\"id\":\"b\",\"sha\":\"0123456789ab\",\"tier\":\"pro\",\"body\":\"{}\"}}",
        "a".repeat(MAX_BODY_BYTES)
    );
    from_json(&ok).unwrap();

    let big = format!(
        "{{\"id\":\"b\",\"sha\":\"0123456789ab\",\"tier\":\"pro\",\"body\":\"{}\"}}",
        "a".repeat(MAX_BODY_BYTES + 1)
    );
    let err = from_json(&big).err().unwrap();
    assert!(err.contains("body") && err.contains(&MAX_BODY_BYTES.to_string()));

    for (field, row) in [
        ("paths", "{\"s\":\"p\"}"),
        ("_new", "{\"s\":\"p\"}"),
        ("shared", "{\"s\":\"p\"}"),
        ("needs", "{\"s\":\"p\"}"),
        ("extra", "{\"key\":\"k\",\"value\":\"v\"}"),
    ] {
        let ok_list = format!(
            "{{\"id\":\"b\",\"sha\":\"0123456789ab\",\"tier\":\"pro\",\"{field}\":[{}]}}",
            std::iter::repeat(row).take(MAX_LIST_ENTRIES).collect::<Vec<_>>().join(",")
        );
        from_json(&ok_list).unwrap();
        let big_list = format!(
            "{{\"id\":\"b\",\"sha\":\"0123456789ab\",\"tier\":\"pro\",\"{field}\":[{}]}}",
            std::iter::repeat(row).take(MAX_LIST_ENTRIES + 1).collect::<Vec<_>>().join(",")
        );
        let err = from_json(&big_list).err().unwrap();
        assert!(err.contains(field) && err.contains(&MAX_LIST_ENTRIES.to_string()), "{err}");
    }
}

#[test]
fn parse_render_matches_go() {
    let text = fs::read(conformance("cases/normalized-valid.md")).unwrap();
    let expected_wire = fs::read(conformance("expected/normalized-valid.wire")).unwrap();

    let record = parse(&text).unwrap();
    assert_eq!(record.wire(), expected_wire);

    let rendered = render(&record).unwrap();
    assert_eq!(rendered, text);

    let rejected = fs::read(conformance("cases/rejected-legacy.md")).unwrap();
    let err = parse(&rejected).err().expect("a stranded key must refuse");
    assert!(err.contains("PRIORITY") && err.contains("line 5"), "{err}");
}

#[test]
fn upgrade_matches_go() {
    use crate::{upgrade, upgrade_with_capacity, read_strict, format_report, refusal_class, MAX_RETAIN_BYTES};

    for (name, is_refused) in [
        ("initial-populated", false),
        ("consumers-populated", false),
        ("empty-consumers", false),
        ("future-note", false),
        ("truncated", true),
    ] {
        let input_path = conformance(&format!("cases/{name}.wire"));
        let wire = fs::read(&input_path).unwrap();
        let snapshot = wire.clone();
        match upgrade(&wire) {
            Ok((out, report)) => {
                assert!(!is_refused, "{name} was expected to be refused");
                let expected_wire = fs::read(conformance(&format!("expected/{name}.wire"))).unwrap();
                assert_eq!(out, expected_wire, "wire mismatch for {name}");
                let expected_report = fs::read_to_string(conformance(&format!("expected/{name}.report"))).unwrap();
                let actual_report = format_report(&report);
                assert_eq!(actual_report, expected_report, "report mismatch for {name}");

                // Upgrade of an upgraded record is byte-identical (SPEC-TABLES §6.6; tla/CardVersioning.tla ShippedUpgradeSucceeds).
                let (out2, _) = upgrade(&out).unwrap();
                assert_eq!(out, out2, "upgrade is not idempotent for {name}");
            }
            Err(e) => {
                assert!(is_refused, "{name} was refused unexpectedly: {e}");
                assert_eq!(wire, snapshot, "refused upgrade mutated input for {name}");
                let expected_refused = fs::read_to_string(conformance(&format!("expected/{name}.refused"))).unwrap();
                assert_eq!(refusal_class(&e), expected_refused.trim(), "refusal class mismatch for {name}");
            }
        }
    }

    // Retention storage one id short refuses with retain-lost and leaves the input unchanged (docs/SPEC-TABLES.md §6.6).
    let future_wire = fs::read(conformance("cases/future-note.wire")).unwrap();
    let snapshot = future_wire.clone();
    let res = upgrade_with_capacity(&future_wire, MAX_RETAIN_BYTES, 0);
    assert!(res.is_err(), "upgrade with zero id capacity should fail");
    let err = res.err().unwrap();
    assert_eq!(refusal_class(&err), "retain-lost", "error should be retain-lost: {err}");
    assert_eq!(future_wire, snapshot, "input wire was mutated on retain-lost refusal");

    // Strict read accepts compatible card and refuses future unknown fields (SPEC-TABLES §4).
    let empty_wire = fs::read(conformance("cases/empty-consumers.wire")).unwrap();
    assert!(read_strict(&empty_wire).is_ok());
    let strict_err = read_strict(&future_wire).err().expect("strict read should refuse future fields");
    assert!(strict_err.contains("unknown field"), "{strict_err}");
}

#[test]
fn sha256_matches_fips_180_vectors() {
    use crate::sha256::Sha256;

    // Vector 1: empty input
    assert_eq!(
        hex_str(&Sha256::digest(b"")),
        "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
    );

    // Vector 2: "abc"
    assert_eq!(
        hex_str(&Sha256::digest(b"abc")),
        "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
    );

    // Vector 3: 448-bit message (56 bytes)
    assert_eq!(
        hex_str(&Sha256::digest(b"abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq")),
        "248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1"
    );
}

fn hex_str(bytes: &[u8]) -> String {
    let mut s = String::with_capacity(bytes.len() * 2);
    for b in bytes {
        use std::fmt::Write;
        write!(&mut s, "{:02x}", b).unwrap();
    }
    s
}

#[test]
fn task_hash_frames_logical_fields() {
    use crate::{task_hash, task_hash_hex, load};
    use crate::sha256::Sha256;

    // Frame pin with independently written byte vector (tables/cards/edit.go TaskHash; tla/CardHops.tla TaskHashStable).
    fn push_u64_le(buf: &mut Vec<u8>, v: u64) {
        buf.extend_from_slice(&v.to_le_bytes());
    }
    fn push_text_le(buf: &mut Vec<u8>, s: &[u8]) {
        push_u64_le(buf, s.len() as u64);
        buf.extend_from_slice(s);
    }
    fn push_list_le(buf: &mut Vec<u8>, items: &[&str]) {
        push_u64_le(buf, items.len() as u64);
        for item in items {
            push_text_le(buf, item.as_bytes());
        }
    }

    let mut frame = Vec::new();
    push_text_le(&mut frame, b"a"); // body
    push_list_le(&mut frame, &["bc"]); // paths
    push_list_le(&mut frame, &[]); // _new
    push_list_le(&mut frame, &[]); // shared
    push_text_le(&mut frame, b"T"); // test
    push_text_le(&mut frame, b"d"); // done

    let want_hash = Sha256::digest(&frame);

    let card_a = crate::from_json("{\"id\":\"x\",\"sha\":\"0123456789ab\",\"tier\":\"pro\",\"body\":\"a\",\"paths\":[{\"s\":\"bc\"}],\"test\":\"T\",\"done_when\":\"d\"}").unwrap();
    let got_hash = task_hash(&card_a).unwrap();


    assert_eq!(got_hash, want_hash, "frame vector mismatch");

    // Shows body \"a\" with paths [\"bc\"] and body \"ab\" with paths [\"c\"] hash differently.
    let card_b = crate::from_json("{\"id\":\"x\",\"sha\":\"0123456789ab\",\"tier\":\"pro\",\"body\":\"ab\",\"paths\":[{\"s\":\"c\"}],\"test\":\"T\",\"done_when\":\"d\"}").unwrap();
    let hash_b = task_hash(&card_b).unwrap();
    assert_ne!(got_hash, hash_b, "framing boundary collision: body and paths must not collide");

    // Matches every expected .hex in tables/cards/conformance/expected/.
    for case in ["card-populated", "initial-populated", "empty-consumers"] {
        let wire = fs::read(conformance(&format!("cases/{case}.wire"))).unwrap();
        let (rec, _) = load(&wire).unwrap();
        let hex = task_hash_hex(&rec).unwrap();
        let expected_hex = fs::read_to_string(conformance(&format!("expected/{case}.hex"))).unwrap();
        assert_eq!(hex, expected_hex.trim(), "task hash hex mismatch for {case}");
    }
}

#[test]
fn field_edit_leaves_the_task_hash_alone() {
    use crate::{load, task_hash, set_needs, set_paths, set_tier, set_priority, set_column, set_holder, Tier, Priority, Column, refusal_class};

    let wire = fs::read(conformance("cases/card-populated.wire")).unwrap();
    let (mut rec, _) = load(&wire).unwrap();
    let orig_hash = task_hash(&rec).unwrap();
    let orig_wire = rec.wire();
    let orig_body = rec.card().body.clone();

    // 1. needs edit changes wire and not hash
    set_needs(&mut rec, &["need-a".to_string(), "need-b".to_string()]).unwrap();
    assert_ne!(rec.wire(), orig_wire);
    assert_eq!(task_hash(&rec).unwrap(), orig_hash);
    assert_eq!(rec.card().body, orig_body);

    // 2. tier edit changes wire and not hash
    set_tier(&mut rec, Tier::Heavy).unwrap();
    assert_eq!(task_hash(&rec).unwrap(), orig_hash);
    assert_eq!(rec.card().body, orig_body);

    // 3. priority edit changes wire and not hash
    set_priority(&mut rec, Priority::Blocker).unwrap();
    assert_eq!(task_hash(&rec).unwrap(), orig_hash);
    assert_eq!(rec.card().body, orig_body);

    // 4. column edit changes wire and not hash
    set_column(&mut rec, Column::Landed).unwrap();
    assert_eq!(task_hash(&rec).unwrap(), orig_hash);
    assert_eq!(rec.card().body, orig_body);

    // 5. holder edit changes wire and not hash
    set_holder(&mut rec, "new-holder").unwrap();
    assert_eq!(task_hash(&rec).unwrap(), orig_hash);
    assert_eq!(rec.card().body, orig_body);

    // 6. paths edit changes both wire and hash
    set_paths(&mut rec, &["p1".to_string(), "p2".to_string()]).unwrap();
    assert_ne!(task_hash(&rec).unwrap(), orig_hash);
    assert_eq!(rec.card().body, orig_body);

    // 7. Every refusal leaves Wire byte-equal to before
    let cur_wire = rec.wire();
    assert!(set_needs(&mut rec, &["x".repeat(65)]).is_err());
    assert_eq!(rec.wire(), cur_wire);

    assert!(set_needs(&mut rec, &["bad\0nul".to_string()]).is_err());
    assert_eq!(rec.wire(), cur_wire);

    assert!(set_holder(&mut rec, &"h".repeat(33)).is_err());
    assert_eq!(rec.wire(), cur_wire);

    let over_list: Vec<String> = (0..513).map(|_| "p".to_string()).collect();
    assert!(set_paths(&mut rec, &over_list).is_err());
    assert_eq!(rec.wire(), cur_wire);
    assert!(set_needs(&mut rec, &over_list).is_err());
    assert_eq!(rec.wire(), cur_wire);

    // 8. Every edit case in the corpus matches its expected wire or refusal class
    for (name, edit_cmd, is_refused) in [
        ("needs", "needs=a,b", false),
        ("tier", "tier=heavy", false),
        ("priority", "priority=blocker", false),
        ("paths", "paths=x,y", false),
        ("column", "column=landed", false),
        ("holder", "holder=h", false),
        ("empty-to-growing", "paths=x", false),
        ("unknown", "needs=a", true),
    ] {
        let input_wire_path = if name == "empty-to-growing" {
            conformance("cases/empty-consumers.wire")
        } else if name == "unknown" {
            conformance("cases/future-note.wire")
        } else {
            conformance("cases/card-populated.wire")
        };
        let input_wire = fs::read(&input_wire_path).unwrap();
        let (mut edit_rec, _) = load(&input_wire).unwrap();
        let edit_res = crate::apply_edit(&mut edit_rec, edit_cmd);
        if is_refused {
            assert!(edit_res.is_err(), "{name} should be refused");
            let err = edit_res.err().unwrap();
            let expected_refused = fs::read_to_string(conformance(&format!("expected/{name}.refused"))).unwrap();
            assert_eq!(refusal_class(&err), expected_refused.trim(), "refusal class mismatch for {name}");
        } else {
            edit_res.unwrap();
            let expected_wire = fs::read(conformance(&format!("expected/{name}.wire"))).unwrap();
            assert_eq!(edit_rec.wire(), expected_wire, "wire mismatch for edit case {name}");
        }
    }
}
