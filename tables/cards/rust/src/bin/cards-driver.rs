// cards-driver — Answers all 9 surfaces (load, save, render, parse, bounds, upgrade, edit, taskhash, view).

use std::env;
use std::fs;
use std::process::exit;

use cards::{
    apply_edit, from_json, load, parse, render, render_view, task_hash_hex, to_json, upgrade,
    TableReport,
};

const SURFACES: [&str; 9] = [
    "load", "save", "render", "parse", "bounds",
    "upgrade", "edit", "taskhash", "view",
];

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
        let edit = if f.len() > 3 { f[3].to_string() } else { String::new() };
        out.push(Case {
            surface: f[0].to_string(),
            name: f[1].to_string(),
            input: f[2].to_string(),
            edit,
        });
    }
    out
}

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

#[allow(clippy::collapsible_if)]
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
                    if (env::var("CARDS_RUST_SABOTAGE_UPGRADE").is_ok()
                        || env::var("CARDS_RUST_NEGATIVE_UPGRADE").is_ok())
                        && report.retained > 0
                    {
                        refuse(outdir, &c.name, "malformed")?;
                    } else {
                        write_file(outdir, &c.name, "wire", &out)?;
                        write_report(outdir, &c.name, &report)?;
                    }
                }
                Err(e) => refuse(outdir, &c.name, &refusal_class(&e))?,
            }
            Ok(())
        }
        "edit" => {
            let wire = fs::read(&c.input).map_err(|e| e.to_string())?;
            let (mut record, _) = load(&wire)?;
            match apply_edit(&mut record, &c.edit) {
                Ok(()) => write_file(outdir, &c.name, "wire", &record.wire()),
                Err(e) => refuse(outdir, &c.name, &refusal_class(&e)),
            }
        }
        "taskhash" => {
            let wire = fs::read(&c.input).map_err(|e| e.to_string())?;
            let (record, _) = load(&wire)?;
            let hex = task_hash_hex(&record)?;
            write_file(outdir, &c.name, "hex", hex.as_bytes())
        }
        "view" => {
            write_file(outdir, &c.name, "txt", &render_view())
        }
        _ => Err(format!("unknown surface {surface:?}")),
    }
}

fn write_report(outdir: &str, name: &str, r: &TableReport) -> Result<(), String> {
    let mut s = String::new();
    s.push_str(&format!("Retained={}
", r.retained));
    s.push_str(&format!("RetainLost={}
", r.retain_lost));
    s.push_str(&format!("Widened={}
", r.widened));
    s.push_str(&format!("Unknown={}
", r.unknown));
    s.push_str(&format!("KindMismatch={}
", r.kind_mismatch));
    s.push_str(&format!("Clamped={}
", r.clamped));
    s.push_str(&format!("Duplicate={}
", r.duplicate));
    s.push_str(&format!("Malformed={}
", r.malformed));
    write_file(outdir, name, "report", s.as_bytes())
}

fn refusal_class(err: &str) -> String {
    if err.contains("past the 65536-byte ceiling") {
        return "body-bound 65536".to_string();
    }
    if err.contains("past the 512-entry ceiling") {
        let field = err
            .strip_prefix("cards: ")
            .unwrap_or(err)
            .split(" has ")
            .next()
            .unwrap_or("");
        return format!("list-bound {field} 512");
    }
    if err.contains("stranded header key") {
        return format!("stranded-key {}", quoted_word(err));
    }
    if err.contains("retained field(s) lost") {
        return "retain-lost".to_string();
    }
    if err.contains("record has loss events") {
        return "unknown-present".to_string();
    }
    "malformed".to_string()
}

fn quoted_word(msg: &str) -> String {
    let Some(i) = msg.find('"') else {
        return String::new();
    };
    let rest = &msg[i + 1..];
    let Some(j) = rest.find('"') else {
        return String::new();
    };
    rest[..j].to_string()
}
