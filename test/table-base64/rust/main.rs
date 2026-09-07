use base64test::{Blob, TableReport, blob_from_json, blob_to_json, blob_to_json_measure};
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
        let text: Vec<u8> = (0..line.len())
            .step_by(2)
            .map(|i| u8::from_str_radix(&line[i..i + 2], 16).expect("hex"))
            .collect();
        let mut value = Blob::default();
        let mut report = TableReport::default();
        blob_from_json(&mut value, &text, &mut report);
        if report.malformed {
            println!("1 0 0 - -");
            continue;
        }
        let mut out = vec![0; blob_to_json_measure(&value) as usize];
        assert_eq!(blob_to_json(&value, &mut out), out.len() as i64);
        println!(
            "0 {} {} {} {}",
            report.clamped,
            report.kind_mismatch,
            hex(&value.payload[..value.payload_length as usize]),
            hex(&out)
        );
    }
}
