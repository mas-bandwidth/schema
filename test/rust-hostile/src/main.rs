// THE RUST PORT'S SHARED HOSTILE-CORPUS RUNNER.
//
// Reads the committed corpus (internal/ci/hostilecorpus/corpus) with a line
// reader, decodes each raw input through this port's GENERATED reader with the
// carrier the manifest names, and writes its verdict to an output directory.
// The Go test beside it (internal/ci/schema_rust_runs_hostile_corpus_test.go)
// compares that verdict, the named reason and the SHA-256 of the canonical text
// against the manifest. The runner never compares; it only answers, so the
// planted-bad-pattern control can hand it a changed manifest and watch the
// comparison go red.

use std::env;
use std::fs;
use std::path::Path;
use std::process::exit;

struct Case {
    name: String,
    carrier: String,
    schema: String,
    table: String,
    input: String,
}

fn die(what: &str) -> ! {
    eprintln!("rust-hostile: {what}");
    exit(2)
}

fn main() {
    let args: Vec<String> = env::args().collect();
    if args.len() != 3 {
        die("usage: rust-hostile <corpus-dir> <out-dir>");
    }
    let corpus = &args[1];
    let out = &args[2];
    if let Err(e) = fs::create_dir_all(out) {
        die(&format!("cannot create {out}: {e}"));
    }
    let manifest = match fs::read_to_string(Path::new(corpus).join("manifest.tsv")) {
        Ok(t) => t,
        Err(e) => die(&format!("cannot read manifest: {e}")),
    };
    for (i, line) in manifest.lines().enumerate() {
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        let f: Vec<&str> = line.split('\t').collect();
        if f.len() != 9 {
            die(&format!("manifest line {}: {} fields, want 9", i + 1, f.len()));
        }
        let c = Case {
            name: f[0].into(),
            carrier: f[1].into(),
            schema: f[2].into(),
            table: f[3].into(),
            input: f[4].into(),
        };
        let payload = match fs::read(Path::new(corpus).join(&c.input)) {
            Ok(b) => b,
            Err(e) => die(&format!("{}: cannot read {}: {e}", c.name, c.input)),
        };
        let (mut verdict, mut reason, mut text) = run_case(&c, &payload);
        // THE PLANTED READER BUG (internal/ci/schema_rust_runs_hostile_corpus_test.go):
        // a scratch switch the fast gate never sets, which makes this runner
        // answer one named case wrongly so the comparison must name it red.
        if env::var("SCHEMA_RUST_HOSTILE_PLANT").ok().as_deref() == Some(c.name.as_str()) {
            verdict = "refuse".into();
            reason = "damaged".into();
            text = None;
        }
        let mut body = format!("{verdict}\n{reason}\n");
        if let Some(t) = text {
            body.push_str(&t);
        }
        if let Err(e) = fs::write(Path::new(out).join(format!("{}.verdict", c.name)), body) {
            die(&format!("{}: cannot write verdict: {e}", c.name));
        }
    }
}

// one root's generated verbs, selected by the unit and root the manifest names
macro_rules! with_root {
    ($unit:expr, $root:expr, |$from:ident, $to:ident, $load:ident| $body:expr) => {{
        match ($unit, $root) {
            ("tabledemo", "GlobalSettings") => {
                use tabledemo::{global_settings_from_json as $from, global_settings_to_json_checked as $to, global_settings_load as $load};
                Some($body)
            }
            ("tabledemo", "PackConfig") => {
                use tabledemo::{pack_config_from_json as $from, pack_config_to_json_checked as $to, pack_config_load as $load};
                Some($body)
            }
            ("tabledemo", "RootConfig") => {
                use tabledemo::{root_config_from_json as $from, root_config_to_json_checked as $to, root_config_load as $load};
                Some($body)
            }
            ("tabledemo", "ShipEntry") => {
                use tabledemo::{ship_entry_from_json as $from, ship_entry_to_json_checked as $to, ship_entry_load as $load};
                Some($body)
            }
            ("graphdemo", "Scene") => {
                use graphdemo::{scene_from_json as $from, scene_to_json_checked as $to, scene_load as $load};
                Some($body)
            }
            ("blobdemo", "Catalog") => {
                use blobdemo::{catalog_from_json as $from, catalog_to_json_checked as $to, catalog_load as $load};
                Some($body)
            }
            ("streamdemo", "Feed") => {
                use streamdemo::{feed_from_json as $from, feed_to_json_checked as $to, feed_load as $load};
                Some($body)
            }
            ("messagedemo", "ToolMessage") => {
                use messagedemo::{tool_message_from_json as $from, tool_message_to_json_checked as $to, tool_message_load as $load};
                Some($body)
            }
            ("probe", "Root") => {
                use probe::{root_from_json as $from, root_to_json_checked as $to, root_load as $load};
                Some($body)
            }
            _ => None,
        }
    }};
}

// The generated text writers end with the file's newline; the engine's
// canonical text (tabletext.Model.Write, the source of every digest) does not.
fn canonical(s: &str) -> String {
    s.strip_suffix('\n').unwrap_or(s).to_string()
}

fn unit_of(schema: &str) -> &'static str {
    match schema {
        "tables/examples" => "tabledemo",
        "tables/pointers" => "graphdemo",
        "tables/blobs" => "blobdemo",
        "tables/stream" => "streamdemo",
        "tables/messages" => "messagedemo",
        "schemas/probe.schema" => "probe",
        _ => "",
    }
}

fn run_case(c: &Case, payload: &[u8]) -> (String, String, Option<String>) {
    let unit = unit_of(&c.schema);
    if unit.is_empty() {
        // The one schema this port's generator emits no text reader for: a
        // pure `fixed table` root reaches only the fixed form. This is the
        // finding the corpus carries, not a silent pass.
        return ("unavailable".into(), "no_text_reader_for_fixed_table".into(), None);
    }
    match c.carrier.as_str() {
        "text" => {
            // The text form is bytes, and the generated Rust reader takes
            // &str: decode lossily so an ill-formed byte reaches the reader as
            // the replacement the engine's own text form spells.
            let text = String::from_utf8_lossy(payload).into_owned();
            let answer = with_root!(unit, c.table.as_str(), |from, to, _load| {
                let mut report = Default::default();
                match from(&text, &mut report) {
                    Some(v) => match to(&v) {
                        Some(t) => Some(canonical(&t)),
                        None => None,
                    },
                    None => None,
                }
            });
            match answer {
                Some(Some(t)) => ("accept".into(), "-".into(), Some(t)),
                Some(None) => ("refuse".into(), "damaged".into(), None),
                None => ("unavailable".into(), "no_text_verb_for_root".into(), None),
            }
        }
        "wire" => {
            let answer = with_root!(unit, c.table.as_str(), |_from, to, load| {
                let mut report = Default::default();
                match load(payload, &mut report) {
                    Some(v) if !report.malformed && !report.refused => {
                        to(&v).map(|t| (true, canonical(&t), String::new()))
                    }
                    Some(_) => Some((false, String::new(), String::new())),
                    None => {
                        let r = report.reason.name().to_string();
                        Some((false, String::new(), r))
                    }
                }
            });
            match answer {
                Some(Some((true, text, _))) => ("accept".into(), "-".into(), Some(text)),
                Some(Some((false, _, reason))) => {
                    ("refuse".into(), wire_reason(payload, &reason), None)
                }
                Some(None) => ("refuse".into(), "damaged".into(), None),
                None => ("unavailable".into(), "no_wire_verb_for_root".into(), None),
            }
        }
        "announce" => {
            let mut entries = vec![tabledemo::TableMessageEntry::default(); 4096];
            let mut v = tabledemo::TableVocabulary::new(&mut entries);
            let mut report = tabledemo::TableReport::default();
            if tabledemo::announce_read(&mut v, payload, &mut report) {
                ("accept".into(), "-".into(), None)
            } else {
                ("refuse".into(), report.reason.name().into(), None)
            }
        }
        "announce_second" => {
            let mut entries = vec![tabledemo::TableMessageEntry::default(); 4096];
            let mut v = tabledemo::TableVocabulary::new(&mut entries);
            let mut report = tabledemo::TableReport::default();
            if !tabledemo::announce_read(&mut v, payload, &mut report) {
                return ("refuse".into(), "first_announcement_failed".into(), None);
            }
            let mut second = tabledemo::TableReport::default();
            if tabledemo::announce_read(&mut v, payload, &mut second) {
                ("accept".into(), "-".into(), None)
            } else {
                ("refuse".into(), second.reason.name().into(), None)
            }
        }
        "announce_bytes" => {
            let mut entries = vec![tabledemo::TableMessageEntry::default(); 4096];
            let mut v = tabledemo::TableVocabulary::new(&mut entries);
            v.max_bytes = 8;
            let mut report = tabledemo::TableReport::default();
            if tabledemo::announce_read(&mut v, payload, &mut report) {
                ("accept".into(), "-".into(), None)
            } else {
                ("refuse".into(), report.reason.name().into(), None)
            }
        }
        "announce_entries" => {
            let mut entries = vec![tabledemo::TableMessageEntry::default(); 1];
            let mut v = tabledemo::TableVocabulary::new(&mut entries);
            let mut report = tabledemo::TableReport::default();
            if tabledemo::announce_read(&mut v, payload, &mut report) {
                ("accept".into(), "-".into(), None)
            } else {
                ("refuse".into(), report.reason.name().into(), None)
            }
        }
        "message" | "message_novocab" => {
            let mut entries = vec![messagedemo::TableMessageEntry::default(); 4096];
            let mut v = messagedemo::TableVocabulary::new(&mut entries);
            if c.carrier == "message" {
                let mut announcement = [0u8; 4096];
                let n = messagedemo::announce(&mut announcement);
                if n < 0 {
                    return ("refuse".into(), "announcement_too_large".into(), None);
                }
                let mut report = messagedemo::TableReport::default();
                if !messagedemo::announce_read(&mut v, &announcement[..n as usize], &mut report) {
                    return ("refuse".into(), report.reason.name().into(), None);
                }
            }
            let mut values = vec![messagedemo::ToolMessageRow::default(); 1];
            let mut report = messagedemo::TableReport::default();
            let (count, ok) =
                messagedemo::tool_message_load_messages(&mut values, &v, payload, &mut report);
            if !ok || count != 1 {
                let reason = if report.reason.name() != "none" {
                    report.reason.name().to_string()
                } else {
                    "damaged".into()
                };
                ("refuse".into(), reason, None)
            } else {
                // A message body fills the fixed-form Row, and this port
                // emits no bridge from that Row to the canonical text type.
                // The accept digest is therefore unavailable here, and the
                // comparison reports it rather than silently passing.
                let _ = values;
                ("unavailable".into(), "no_text_conversion_for_message_row".into(), None)
            }
        }
        other => ("error".into(), format!("unknown_carrier_{other}"), None),
    }
}

// The Rust runtime's reason names and the engine's differ in two places: the
// form byte is checked before the form's name is known, and the builder-path
// storage cap has no enum of its own. Map them by the wire the case carries.
fn wire_reason(payload: &[u8], reason: &str) -> String {
    match reason {
        "message_form_as_file" => "message_form_as_file".into(),
        "newer_form" => "newer_form".into(),
        "unknown_form" => match payload.first() {
            Some(2) => "message_form_as_file".into(),
            _ => "newer_form".into(),
        },
        "count_over_length" | "count_over_extent_cap" => "count_refusal".into(),
        "wire_damaged" | "truncated" | "invalid_value" => "damaged".into(),
        "" | "none" | "ok" => "damaged".into(),
        other => other.into(),
    }
}
