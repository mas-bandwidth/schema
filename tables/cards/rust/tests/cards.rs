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

fn hex_bytes(s: &str) -> [u8; 32] {
    let mut out = [0u8; 32];
    for i in 0..32 {
        out[i] = u8::from_str_radix(&s[i * 2..i * 2 + 2], 16).unwrap();
    }
    out
}

#[test]
fn sha256_matches_fips_180_vectors() {
    use crate::sha256::Sha256;
    let mut h = Sha256::new();
    h.update(b"");
    assert_eq!(h.finalize(), hex_bytes("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"));

    let mut h = Sha256::new();
    h.update(b"abc");
    assert_eq!(h.finalize(), hex_bytes("ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"));

    let mut h = Sha256::new();
    h.update(b"abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq");
    assert_eq!(h.finalize(), hex_bytes("248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1"));
}

fn report_lines(r: &crate::TableReport) -> String {
    format!(
        "Retained={}\nRetainLost={}\nWidened={}\nUnknown={}\nKindMismatch={}\nClamped={}\nDuplicate={}\nMalformed={}\n",
        r.retained, r.retain_lost, r.widened, r.unknown, r.kind_mismatch, r.clamped, r.duplicate, r.malformed
    )
}

#[test]
fn upgrade_matches_go() {
    for (name, expect_wire, expect_report) in [
        ("initial-populated", true, true),
        ("consumers-populated", true, true),
        ("empty-consumers", true, true),
        ("future-note", true, true),
        ("truncated", false, false),
    ] {
        let wire = fs::read(conformance(&format!("cases/{name}.wire"))).unwrap();
        match crate::upgrade(&wire) {
            Ok((out, report)) => {
                assert!(expect_wire, "{name}: expected a refusal, got a successful upgrade");
                assert_eq!(out, fs::read(conformance(&format!("expected/{name}.wire"))).unwrap(), "{name}: upgraded wire");
                assert_eq!(report_lines(&report), fs::read_to_string(conformance(&format!("expected/{name}.report"))).unwrap(), "{name}: report");
                let again = crate::upgrade(&out).unwrap().0;
                assert_eq!(again, out, "{name}: upgrading an upgraded record is byte-identical");
            }
            Err(e) => {
                assert!(!expect_wire, "{name}: expected success, got {e}");
                assert_eq!(crate::refusal_class(&e), "malformed", "{name}");
            }
        }
    }

    let future = fs::read(conformance("cases/future-note.wire")).unwrap();
    let before = future.clone();
    let err = crate::upgrade_seam(&future, crate::MAX_RETAIN_BYTES, 0).err().expect("one id short must refuse");
    assert!(err.contains("retained field(s) lost"), "{err}");
    assert_eq!(future, before, "the input wire is unchanged");
}

fn push_text(out: &mut Vec<u8>, s: &str) {
    out.extend_from_slice(&(s.len() as u64).to_le_bytes());
    out.extend_from_slice(s.as_bytes());
}

fn push_path_list(out: &mut Vec<u8>, l: &[&str]) {
    out.extend_from_slice(&(l.len() as u64).to_le_bytes());
    for s in l {
        push_text(out, s);
    }
}

#[test]
fn task_hash_frames_logical_fields() {
    use crate::sha256::Sha256;
    let json = "{\"id\":\"x\",\"sha\":\"0123456789ab\",\"tier\":\"pro\",\"body\":\"a\",\"paths\":[{\"s\":\"bc\"}]}";
    let record = from_json(json).unwrap();
    let got = crate::task_hash(&record).unwrap();

    let mut framed = Vec::new();
    push_text(&mut framed, "a");
    push_path_list(&mut framed, &["bc"]);
    push_path_list(&mut framed, &[]);
    push_path_list(&mut framed, &[]);
    push_text(&mut framed, "");
    push_text(&mut framed, "");
    let mut h = Sha256::new();
    h.update(&framed);
    assert_eq!(got, h.finalize(), "the frame is pinned by an independent byte vector");

    let json2 = "{\"id\":\"x\",\"sha\":\"0123456789ab\",\"tier\":\"pro\",\"body\":\"ab\",\"paths\":[{\"s\":\"c\"}]}";
    let record2 = from_json(json2).unwrap();
    let got2 = crate::task_hash(&record2).unwrap();
    assert_ne!(got, got2, "body \"a\" paths [bc] and body \"ab\" paths [c] hash differently");

    for name in ["card-populated", "initial-populated", "empty-consumers"] {
        let wire = fs::read(conformance(&format!("cases/{name}.wire"))).unwrap();
        let (rec, _) = load(&wire).unwrap();
        let h = crate::task_hash(&rec).unwrap();
        let expected = fs::read_to_string(conformance(&format!("expected/{name}.hex"))).unwrap();
        let expected = expected.trim();
        let actual = h.iter().map(|b| format!("{b:02x}")).collect::<String>();
        assert_eq!(actual, expected, "{name}: .hex");
    }
}

#[test]
fn field_edit_leaves_the_task_hash_alone() {
    let wire = fs::read(conformance("cases/card-populated.wire")).unwrap();
    let (mut record, _) = load(&wire).unwrap();
    let h0 = crate::task_hash(&record).unwrap();
    let body0 = record.card().body.as_ref().map(|s| s.as_bytes().to_vec());
    let wire0 = record.wire();
    let body = |r: &crate::Record| r.card().body.as_ref().map(|s| s.as_bytes().to_vec());

    crate::set_needs(&mut record, &["a".to_string(), "b".to_string()]).unwrap();
    assert_ne!(record.wire(), wire0);
    assert_eq!(crate::task_hash(&record).unwrap(), h0, "needs edit keeps the hash");
    assert_eq!(body(&record), body0, "body is byte-equal");

    crate::set_tier(&mut record, crate::Tier::Heavy).unwrap();
    assert_ne!(record.wire(), wire0);
    assert_eq!(crate::task_hash(&record).unwrap(), h0, "tier edit keeps the hash");

    crate::set_priority(&mut record, crate::Priority::Blocker).unwrap();
    assert_ne!(record.wire(), wire0);
    assert_eq!(crate::task_hash(&record).unwrap(), h0, "priority edit keeps the hash");

    crate::set_column(&mut record, crate::Column::Landed).unwrap();
    assert_ne!(record.wire(), wire0);
    assert_eq!(crate::task_hash(&record).unwrap(), h0, "column edit keeps the hash");

    crate::set_holder(&mut record, "h").unwrap();
    assert_ne!(record.wire(), wire0);
    assert_eq!(crate::task_hash(&record).unwrap(), h0, "holder edit keeps the hash");

    crate::set_paths(&mut record, &["x".to_string(), "y".to_string()]).unwrap();
    assert_ne!(record.wire(), wire0);
    assert_ne!(crate::task_hash(&record).unwrap(), h0, "paths edit changes the hash");

    let (mut rec2, _) = load(&wire).unwrap();
    let before = rec2.wire();
    let overlong = vec!["a".repeat(65)];
    let err = crate::set_needs(&mut rec2, &overlong).err().expect("overlong needs must refuse");
    assert!(err.contains("bytes"), "{err}");
    assert_eq!(rec2.wire(), before, "a refusal leaves the wire byte-equal");

    for (case, edit, expect_wire) in [
        ("needs", "needs=a,b", true),
        ("tier", "tier=heavy", true),
        ("priority", "priority=blocker", true),
        ("paths", "paths=x,y", true),
        ("column", "column=landed", true),
        ("holder", "holder=h", true),
        ("empty-to-growing", "paths=x", true),
        ("unknown", "needs=a", false),
    ] {
        let input = if case == "empty-to-growing" {
            fs::read(conformance("cases/empty-consumers.wire")).unwrap()
        } else if case == "unknown" {
            fs::read(conformance("cases/future-note.wire")).unwrap()
        } else {
            wire.clone()
        };
        let (mut rec, _) = load(&input).unwrap();
        match crate::apply_edit(&mut rec, edit) {
            Ok(()) => {
                assert!(expect_wire, "{case}: expected a refusal");
                assert_eq!(rec.wire(), fs::read(conformance(&format!("expected/{case}.wire"))).unwrap(), "{case}: wire");
            }
            Err(e) => {
                assert!(!expect_wire, "{case}: expected success, got {e}");
                assert_eq!(crate::refusal_class(&e), "unknown-present", "{case}");
            }
        }
    }
}

const VIEW_VOCABULARY: &str = "\
Card id string value 64\n\
Card sha string value 40\n\
Card tier Tier value -\n\
Card repo string value 96\n\
Card _base string value 96\n\
Card kind string value 24\n\
Card priority Priority value -\n\
Card stream string value 32\n\
Card paths PathText list -\n\
Card _new PathText list -\n\
Card shared PathText list -\n\
Card needs CardID list -\n\
Card test string value 192\n\
Card done string blob -\n\
Card deadline_minutes int32 value -\n\
Card source string blob -\n\
Card epoch int32 value -\n\
Card column Column value -\n\
Card holder string value 32\n\
Card attempt int32 value -\n\
Card head string value 40\n\
Card branch string value 128\n\
Card body string blob -\n\
Card extra KeyValue list -\n\
Card consumers Consumer list -\n\
CardID s string value 64\n\
Consumer kind string value 16\n\
Consumer who string value 32\n\
Consumer route string value 48\n\
Consumer model string value 64\n\
Consumer started int64 value -\n\
Consumer ended int64 value -\n\
Consumer outcome string value 16\n\
Consumer line string value 200\n\
Consumer cost_micro_usd int64 value -\n\
KeyValue key string value 32\n\
KeyValue value string blob -\n\
PathText s string value 192\n";

#[test]
fn view_walks_every_field() {
    let v = crate::view();
    assert_eq!(v, VIEW_VOCABULARY, "the view equals the explicit vocabulary");
    assert_eq!(
        v,
        fs::read_to_string(conformance("expected/view.txt")).unwrap(),
        "the view equals the conformance answer"
    );

    let wire = fs::read(conformance("cases/card-populated.wire")).unwrap();
    let (record, _) = load(&wire).unwrap();
    let mut seen = 0;
    for t in crate::card_descriptors::TABLES.iter() {
        for f in t.fields.iter() {
            seen += 1;
            assert!(!f.name.is_empty());
            let shape = if f.shape == crate::card_descriptors::SHAPE_LIST {
                "list"
            } else if f.shape == crate::card_descriptors::SHAPE_BLOB {
                "blob"
            } else {
                "value"
            };
            assert!(matches!(shape, "value" | "list" | "blob"));
        }
    }
    assert_eq!(seen, 38, "the registry walks every field");

    let mut rec = record;
    crate::set_needs(&mut rec, &["a".to_string()]).unwrap();
    assert!(crate::task_hash(&rec).is_ok());
}
