// Single-unit Rust conformance driver for table-write-bounds-rust.
// Uses the same generated writebounds crate and shared mutation logic as
// test/conformance/rust/src/main.rs without compiling the 43 unrelated units.

use std::fs;
use std::io::Write;
use std::path::Path;
use std::process::exit;

#[path = "../../src/writebounds_cases.rs"]
mod writebounds_cases;

struct Manifest {
    lines: Vec<Vec<String>>,
}

impl Manifest {
    fn read(path: &str) -> Manifest {
        let text = fs::read_to_string(path).unwrap_or_else(|e| fail(&format!("cannot open {path}: {e}")));
        let mut lines = Vec::new();
        for line in text.lines() {
            if line.is_empty() || line.starts_with('#') {
                continue;
            }
            let fields: Vec<String> = line
                .split([' ', '\t', '\r'])
                .filter(|s| !s.is_empty())
                .map(|s| s.to_string())
                .collect();
            if fields.is_empty() || fields[0].starts_with('#') {
                continue;
            }
            lines.push(fields);
        }
        Manifest { lines }
    }

    fn of_kind<'a>(&'a self, kind: &'a str) -> impl Iterator<Item = &'a Vec<String>> {
        self.lines.iter().filter(move |f| f[0] == kind)
    }
}

fn fail(what: &str) -> ! {
    eprintln!("driver: {what}");
    exit(1)
}

fn slurp(path: &str) -> Vec<u8> {
    fs::read(path).unwrap_or_else(|e| fail(&format!("cannot read {path}: {e}")))
}

fn spill(dir: &str, name: &str, data: &[u8]) {
    let path = Path::new(dir).join(name);
    let mut file = fs::File::create(&path).unwrap_or_else(|e| fail(&format!("cannot write {}: {e}", path.display())));
    file.write_all(data).unwrap_or_else(|e| fail(&format!("cannot write {}: {e}", path.display())));
}

fn spill_refused(dir: &str, name: &str) {
    spill(dir, name, b"refused\n");
}

fn no_text(f: &[String]) -> bool {
    f.get(5).map(String::as_str) == Some("no-text")
}

fn wref_run<T>(write: impl FnOnce() -> Result<T, String>) -> Result<T, String> {
    let previous = std::panic::take_hook();
    std::panic::set_hook(Box::new(|_| {}));
    let outcome = std::panic::catch_unwind(std::panic::AssertUnwindSafe(write));
    std::panic::set_hook(previous);
    outcome.unwrap_or_else(|_| Err(String::from("debug_assert!")))
}

fn surface_wire(manifest: &Manifest, out: &str) {
    for f in manifest.of_kind("instance") {
        let data = slurp(&f[4]);
        let wire_refused = f.iter().skip(5).any(|m| m == "wire-refused");
        if (f[2].as_str(), f[3].as_str()) != ("writebounds", "TortureVarWref") {
            fail(&format!("wire: unsupported root {} {}", f[2], f[3]));
        }
        let mut report = writebounds::TableReport::default();
        match writebounds::torture_var_wref_load(&data, &mut report) {
            Some(v) => match writebounds::torture_var_wref_save_checked(&v) {
                Ok(bytes) => spill(out, &f[1], &bytes),
                Err(why) => spill(out, &format!("{}.absent", f[1]), why.as_bytes()),
            },
            None if wire_refused || (!report.damaged && report.reason.name() == "blob_shared") => {
                spill_refused(out, &f[1]);
            }
            None => {
                let why = format!("load refused: {}", if report.damaged { "malformed" } else { report.reason.name() });
                spill(out, &format!("{}.absent", f[1]), why.as_bytes());
            }
        }
    }
}

fn surface_json_read(manifest: &Manifest, out: &str) {
    let json_dir = std::env::var("SCHEMA_CONFORMANCE_JSON_INPUT_DIR")
        .ok()
        .filter(|dir| !dir.is_empty())
        .unwrap_or_else(|| "testdata/conformance/tables/json".into());
    for f in manifest.of_kind("instance") {
        if no_text(f) {
            continue;
        }
        if (f[2].as_str(), f[3].as_str()) != ("writebounds", "TortureVarWref") {
            fail(&format!("json-read: unsupported root {} {}", f[2], f[3]));
        }
        let path = format!("{json_dir}/{}.json", f[1]);
        let text = fs::read_to_string(&path).unwrap_or_else(|e| fail(&format!("cannot read {path}: {e}")));
        let mut report = writebounds::TableReport::default();
        let bytes = match writebounds::torture_var_wref_from_json_bytes(text.as_bytes(), &mut report) {
            Some(v) => writebounds::torture_var_wref_save_checked(&v)
                .unwrap_or_else(|why| fail(&format!("{}: the save refused the value: {why}", f[1]))),
            None => fail(&format!("{}: FromJson refused the text", f[1])),
        };
        spill(out, &f[1], &bytes);
    }
}

fn surface_json_write(manifest: &Manifest, out: &str) {
    for f in manifest.of_kind("instance") {
        if no_text(f) {
            continue;
        }
        if (f[2].as_str(), f[3].as_str()) != ("writebounds", "TortureVarWref") {
            fail(&format!("json-write: unsupported root {} {}", f[2], f[3]));
        }
        let data = slurp(&f[4]);
        let mut report = writebounds::TableReport::default();
        let text = match writebounds::torture_var_wref_load(&data, &mut report) {
            Some(v) => writebounds::torture_var_wref_to_json_checked(&v)
                .unwrap_or_else(|| fail(&format!("{}: ToJson refused the value", f[1]))),
            None => fail(&format!("{}: load refused {}", f[1], f[4])),
        };
        spill(out, &format!("{}.json", f[1]), text.as_bytes());
    }
}

fn surface_write_refusal(manifest: &Manifest, out: &str) {
    for f in manifest.of_kind("write-refusal") {
        if f.len() < 8 {
            fail(&format!("write-refusal line has {} fields", f.len()));
        }
        let (case, unit, root, wire, path, kind, value) =
            (&f[1], f[2].as_str(), f[3].as_str(), &f[4], f[5].as_str(), f[6].as_str(), f[7].as_str());
        if (unit, root) != ("writebounds", "TortureVarWref") {
            fail(&format!("write-refusal: unsupported root {unit} {root}"));
        }
        let data = slurp(wire);
        let mut report = writebounds::TableReport::default();
        let Some(mut v) = writebounds::torture_var_wref_load(&data, &mut report) else {
            fail(&format!("{case}: load refused {wire}"))
        };
        let answer = match writebounds_cases::mutate(&mut v, path, kind, value).unwrap_or_else(|msg| fail(&msg)) {
            writebounds_cases::WriteBoundsMutation::Done => match wref_run(|| writebounds::torture_var_wref_save_checked(&v)) {
                Ok(saved) => {
                    if f.get(8).map(String::as_str) == Some("save-wire") {
                        spill(out, &format!("{case}.wire"), &saved);
                    }
                    "wrote\n"
                }
                Err(_) => "refused\n",
            },
            writebounds_cases::WriteBoundsMutation::Unrepresentable => "unrepresentable\n",
        };
        spill(out, case, answer.as_bytes());
    }
}

fn main() {
    let args: Vec<String> = std::env::args().collect();
    if args.len() < 3 {
        eprintln!("usage: {0} <manifest> list\n       {0} <manifest> <surface> <outdir>", args[0]);
        exit(2);
    }
    let manifest = Manifest::read(&args[1]);
    let surface = args[2].as_str();
    if surface == "list" {
        println!("wire\njson-read\njson-write\nwrite-refusal");
        return;
    }
    if args.len() < 4 {
        eprintln!("usage: {} <manifest> <surface> <outdir>", args[0]);
        exit(2);
    }
    let out = args[3].as_str();
    match surface {
        "wire" => surface_wire(&manifest, out),
        "json-read" => surface_json_read(&manifest, out),
        "json-write" => surface_json_write(&manifest, out),
        "write-refusal" => surface_write_refusal(&manifest, out),
        _ => exit(2),
    }
}
