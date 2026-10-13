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
