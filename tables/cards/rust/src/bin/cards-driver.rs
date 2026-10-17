// cards-driver — the Rust leg of the card conformance set
// (tables/cards/conformance/FORMAT.md). It answers the driver contract
// test/conformance/README.md states — `<driver> <manifest> list` and
// `<driver> <manifest> <surface> <outdir>` — over the nine card surfaces
// through the facade in tables/cards/rust (Load, ToJSON, FromJSON, Render,
// Parse, Upgrade, the Set* edits, TaskHash and the generated view).

use std::env;
use std::fs;
use std::process::exit;

use cards::{apply_edit, from_json, load, parse, refusal_class, render, task_hash, to_json, upgrade, view, TableReport};

// surfaces is the nine card surfaces, in the order a leg lists and answers
// them.
const SURFACES: [&str; 9] = [
    "load", "save", "render", "parse", "bounds", "upgrade", "edit", "taskhash", "view",
];

// aCase is one manifest line; edit carries the one edit token after the input.
struct Case {
    surface: String,
    name: String,
    input: String,
    edit: String,
}

fn main() {
    let args: Vec<String> = env::args().collect();
    if args.len() < 3 {
        eprintln!("usage: cards-driver <manifest> (list | <surface> <outdir>)");
        exit(1);
    }
    let manifest = &args[1];
    let verb = &args[2];
    if verb == "list" {
        for s in SURFACES {
            println!("{}", s);
        }
        return;
    }
    if args.len() < 4 {
        eprintln!("usage: cards-driver <manifest> <surface> <outdir>");
        exit(1);
    }
    let surface = verb;
    let outdir = &args[3];

    let cases = read_manifest(manifest);
    if let Err(e) = fs::create_dir_all(outdir) {
        fatalf(&format!("{e}"));
    }
    if let Err(e) = answer(surface, &cases, outdir) {
        fatalf(&e);
    }
}

fn fatalf(msg: &str) -> ! {
    eprintln!("cards-driver: {}", msg);
    exit(1)
}

// readManifest reads the derived manifest: one case per line as
// "<surface> <name> <input>", skipping comments and blanks.
fn read_manifest(path: &str) -> Vec<Case> {
    let content = match fs::read_to_string(path) {
        Ok(c) => c,
        Err(e) => fatalf(&format!("{e}")),
    };
    let mut out = Vec::new();
    for line in content.lines() {
        let line = line.trim();
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        let f: Vec<&str> = line.split_whitespace().collect();
        if f.len() < 3 {
            continue;
        }
        out.push(Case {
            surface: f[0].to_string(),
            name: f[1].to_string(),
            input: f[2].to_string(),
            edit: f[3..].join(" "),
        });
    }
    out
}

// answer runs one surface over every case of that surface in the manifest,
// writing one file per case into outdir.
fn answer(surface: &str, cases: &[Case], outdir: &str) -> Result<(), String> {
    for c in cases {
        if c.surface != surface {
            continue;
        }
        one(surface, c, outdir)?;
    }
    Ok(())
}

fn write_file(outdir: &str, name: &str, ext: &str, data: &[u8]) -> Result<(), String> {
    let path = format!("{outdir}/{name}.{ext}");
    fs::write(&path, data).map_err(|e| e.to_string())
}

fn refuse(outdir: &str, name: &str, class: &str) -> Result<(), String> {
    write_file(outdir, name, "refused", format!("{class}\n").as_bytes())
}

fn one(surface: &str, c: &Case, outdir: &str) -> Result<(), String> {
    match surface {
        "load" => {
            let wire = fs::read(&c.input).map_err(|e| e.to_string())?;
            let (record, report) = load(&wire)?;
            let (json, _) = to_json(&record)?;
            write_file(outdir, &c.name, "json", json.as_bytes())?;
            write_report(outdir, &c.name, &report)
        }
        "save" | "bounds" => {
            let bytes = fs::read(&c.input).map_err(|e| e.to_string())?;
            let text = std::str::from_utf8(&bytes).map_err(|_| "cards: input is not UTF-8".to_string())?;
            match from_json(text) {
                Ok(record) => write_file(outdir, &c.name, "wire", &record.wire()),
                Err(e) => refuse(outdir, &c.name, &refusal_class(&e)),
            }
        }
        "render" => {
            let wire = fs::read(&c.input).map_err(|e| e.to_string())?;
            let (record, _) = load(&wire)?;
            let mut text = render(&record)?;
            if env::var("CARDS_RUST_SABOTAGE_RENDER").is_ok() {
                // the negative control's test-only switch: change one rendered byte.
                if let Some(b) = text.first_mut() {
                    *b ^= 0xff;
                }
            }
            write_file(outdir, &c.name, "txt", &text)
        }
        "parse" => {
            let text = fs::read(&c.input).map_err(|e| e.to_string())?;
            match parse(&text) {
                Ok(record) => write_file(outdir, &c.name, "wire", &record.wire()),
                Err(e) => refuse(outdir, &c.name, &refusal_class(&e)),
            }
        }
        "upgrade" => {
            let wire = fs::read(&c.input).map_err(|e| e.to_string())?;
            match upgrade(&wire) {
                Ok((out, report)) => {
                    let mut out = out;
                    if env::var("CARDS_RUST_SABOTAGE_UPGRADE").is_ok() {
                        // the negative control's test-only switch: drop one byte.
                        if let Some(b) = out.first_mut() {
                            *b ^= 0xff;
                        }
                    }
                    write_file(outdir, &c.name, "wire", &out)?;
                    write_report(outdir, &c.name, &report)
                }
                Err(e) => refuse(outdir, &c.name, &refusal_class(&e)),
            }
        }
        "edit" => {
            let wire = fs::read(&c.input).map_err(|e| e.to_string())?;
            let (mut record, _) = match load(&wire) {
                Ok(r) => r,
                Err(e) => return refuse(outdir, &c.name, &refusal_class(&e)),
            };
            if let Err(e) = apply_edit(&mut record, &c.edit) {
                return refuse(outdir, &c.name, &refusal_class(&e));
            }
            write_file(outdir, &c.name, "wire", &record.wire())
        }
        "taskhash" => {
            let wire = fs::read(&c.input).map_err(|e| e.to_string())?;
            let (record, _) = load(&wire)?;
            let mut hex = task_hash(&record)?.iter().map(|b| format!("{b:02x}")).collect::<String>();
            if env::var("CARDS_RUST_SABOTAGE_HASH").is_ok() {
                // the negative control's test-only switch: change one hex digit.
                if let Some(b) = unsafe { hex.as_bytes_mut() }.first_mut() {
                    *b = if *b == b'a' { b'b' } else { b'a' };
                }
            }
            write_file(outdir, &c.name, "hex", hex.as_bytes())
        }
        "view" => write_file(outdir, &c.name, "txt", view().as_bytes()),
        _ => Err(format!("unknown surface {surface:?}")),
    }
}

// writeReport writes the read report, one Name=value line per counter of the
// Go TableReport in declaration order (tables/cards/conformance/FORMAT.md).
fn write_report(outdir: &str, name: &str, r: &TableReport) -> Result<(), String> {
    let mut s = String::new();
    s.push_str(&format!("Retained={}\n", r.retained));
    s.push_str(&format!("RetainLost={}\n", r.retain_lost));
    s.push_str(&format!("Widened={}\n", r.widened));
    s.push_str(&format!("Unknown={}\n", r.unknown));
    s.push_str(&format!("KindMismatch={}\n", r.kind_mismatch));
    s.push_str(&format!("Clamped={}\n", r.clamped));
    s.push_str(&format!("Duplicate={}\n", r.duplicate));
    s.push_str(&format!("Malformed={}\n", r.malformed));
    write_file(outdir, name, "report", s.as_bytes())
}


