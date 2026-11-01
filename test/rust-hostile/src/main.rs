// THE RUST PORT RUNS THE SHARED HOSTILE CORPUS (docs/HOSTILE-CORPUS.md).
//
// One raw input per manifest line, the port's generated reader underneath, and
// the Go engine's committed verdict as the oracle. The corpus is read as files:
// no network, no clock beyond the per-case budget, no outside service. The
// runner prints one PASS line per case it agrees with, one FIX-CARD line per
// carried disagreement, one SKIP line per carrier the port has no API for, and
// fails on a disagreement no FIX-CARD names (or a FIX-CARD whose case has gone
// green).
//
// usage: rust-hostile <corpus-dir> <known-findings.tsv> <profile>
//        rust-hostile --case <carrier> <schema> <table> <input-file>
//
// ISOLATION. Every case runs in a child process of this binary (`--case`), on
// a thread with a fixed 512 KiB stack, under catch_unwind. A panic is caught
// and named; a stack overflow, an abort or a signal kills only the child; a
// case over its budget (5 s, HOSTILE_CASE_BUDGET_MS overrides) is killed and
// named. Each is a FAIL for that case and the run goes on to the next. Cases
// run concurrently (HOSTILE_JOBS, default min(cores, 4)) and are reported in
// manifest order.
//
// A carrier maps to the unit crate's generated functions (build.rs binds them
// by name from the crate's source):
//   wire              <table>_load
//   wire_retain       <table>_load_retain into a TableRetain store
//   text              <table>_from_json_bytes
//   message           announce + announce_read on a fresh vocabulary, then
//                     <table>_load_messages into one row (capacity 1)
//   message_novocab   <table>_load_messages on a vocabulary never announced
//   announce          announce_read on a fresh vocabulary
//   announce_second   announce_read twice on one vocabulary
//   announce_bytes    announce_read with max_bytes 8
//   announce_entries  announce_read with one entry of storage
//   packet            read_<type> from bit 0 of the raw bytes, then the port's
//                     own write_<type>; the digest is of the written bytes
//
// After an accepted wire, wire_retain or text case the runner also runs
// <table>_load_measure (on the input wire and on the saved file),
// <table>_save_checked, <table>_save_retain (wire_retain) and
// <table>_to_json_checked on the decoded value; after an accepted message case,
// <table>_measure_messages and <table>_save_messages. A named refusal is a
// clean answer; a panic, an abort or a hang there is the case's FAIL.
//
// The canonical decoded value is the port's text writer with the engine's one
// shape difference removed: the port ends its text with one newline and the
// engine's canonical text (internal/tabletext.Model.Write) does not.

use std::collections::HashMap;
use std::io::Read as _;
use std::panic::{self, AssertUnwindSafe};
use std::path::Path;
use std::process::{Command, Stdio};
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};
use std::{env, fs, thread};

mod sha256;

const CASE_STACK: usize = 512 * 1024;

pub struct TableOps {
    pub wire: fn(&[u8], bool) -> String,
    pub text: fn(&[u8]) -> String,
    pub message: Option<fn(&[u8], bool) -> String>,
}

pub struct VocabularyOps {
    pub run: fn(&[u8], &str) -> String,
}

pub struct PacketOps {
    pub run: fn(&[u8]) -> String,
}

fn accept(digest: &str) -> String {
    format!("accept {digest}")
}

fn refuse(reason: &str) -> String {
    format!("refuse {reason}")
}

// The digest of the port's text, its trailing newlines dropped.
fn text_digest(text: Option<String>) -> String {
    match text {
        None => "digest-error: to_json_checked refused the decoded value".to_string(),
        Some(t) => accept(&sha256::hex(t.trim_end_matches('\n').as_bytes())),
    }
}

// A read's verdict off its report: a named refusal is its reason, a malformed
// read (no name) is the corpus's `damaged`.
fn refusal(refused: bool, malformed: bool, reason: &str, landed: bool) -> Option<String> {
    let named = !reason.is_empty() && reason != "none" && reason != "ok";
    if refused || !landed {
        return Some(refuse(if named { reason } else { "damaged" }));
    }
    if malformed {
        return Some(refuse("damaged"));
    }
    None
}

#[allow(unused_macros)]
macro_rules! table_ops {
    ($k:ident, $load:ident, $load_retain:ident, $save_checked:ident, $save_retain:ident,
     $load_measure:ident, $to_json:ident, $from_json:ident, $message:expr) => {
        $crate::TableOps {
            wire: |input, retaining| {
                let mut report = $k::TableReport::default();
                let mut store = $k::TableRetain {
                    bytes: Vec::with_capacity(65536.max(2 * input.len())),
                    ids: Vec::with_capacity(4096),
                };
                let value = if retaining {
                    $k::$load_retain(input, &mut store, &mut report)
                } else {
                    $k::$load(input, &mut report)
                };
                if let Some(r) = $crate::refusal(report.refused, report.malformed, report.reason.name(), value.is_some()) {
                    return r;
                }
                let value = value.expect("landed");
                let verdict = $crate::text_digest($k::$to_json(&value));
                // the rest of the port's surface on the decoded value
                let _ = $k::$load_measure(input);
                if let Ok(file) = $k::$save_checked(&value) {
                    let _ = $k::$load_measure(&file);
                    if retaining {
                        let mut again = $k::TableReport::default();
                        let _ = $k::$save_retain(&value, &mut store, &mut again);
                    }
                }
                verdict
            },
            text: |input| {
                let mut report = $k::TableReport::default();
                let value = $k::$from_json(input, &mut report);
                if let Some(r) = $crate::refusal(report.refused, report.malformed, report.reason.name(), value.is_some()) {
                    return r;
                }
                let value = value.expect("landed");
                let verdict = $crate::text_digest($k::$to_json(&value));
                if let Ok(file) = $k::$save_checked(&value) {
                    let _ = $k::$load_measure(&file);
                }
                verdict
            },
            message: $message,
        }
    };
}

#[allow(unused_macros)]
macro_rules! message_ops {
    ($k:ident, $load_messages:ident, $measure_messages:ident, $save_messages:ident,
     $message_default:ident, $write_body:ident, $from_json:ident, $to_json:ident) => {
        |input: &[u8], announced: bool| -> String {
            let mut entries = vec![$k::TableMessageEntry::default(); 4096];
            let mut vocab = $k::TableVocabulary::new(&mut entries);
            if announced {
                let mut own = vec![0u8; $k::announce_measure()];
                $k::announce(&mut own);
                let mut ar = $k::TableReport::default();
                if !$k::announce_read(&mut vocab, &own, &mut ar) || ar.malformed {
                    return "error: the unit's own announcement was refused".to_string();
                }
            }
            let mut rows = [$k::$message_default()];
            let mut report = $k::TableReport::default();
            let (n, ok) = $k::$load_messages(&mut rows, &vocab, input, &mut report);
            if report.refused {
                return $crate::refuse(match report.reason.name() {
                    "" | "none" | "ok" => "damaged",
                    r => r,
                });
            }
            if report.malformed || !ok || n != 1 {
                return $crate::refuse("damaged");
            }
            // the rest of the port's surface on the decoded batch
            let mut again = $k::TableReport::default();
            let size = $k::$measure_messages(&rows, &mut again);
            if size >= 0 {
                let mut buffer = vec![0u8; size as usize];
                let _ = $k::$save_messages(&rows, &mut buffer, &mut again);
            }
            // The batch's one body is a fixed-class row; the port's text writer
            // takes the variable value. A body that sets no field decodes to
            // the table's defaults, and from_json("{}") is where the port
            // states them: the runner compares the decoded row's bytes with a
            // default row's.
            let mut a = vec![0u8; 1 << 16];
            let mut b = vec![0u8; 1 << 16];
            $k::$write_body(&mut a, &rows[0]);
            $k::$write_body(&mut b, &$k::$message_default());
            if a != b {
                return "unhandled decoded value: the body sets a field".to_string();
            }
            let mut dr = $k::TableReport::default();
            match $k::$from_json(b"{}", &mut dr) {
                Some(defaults) => $crate::text_digest($k::$to_json(&defaults)),
                None => "error: the port refused {}".to_string(),
            }
        }
    };
}

#[allow(unused_macros)]
macro_rules! vocabulary_ops {
    ($k:ident) => {
        $crate::VocabularyOps {
            run: |input, carrier| {
                let mut entries = vec![$k::TableMessageEntry::default(); if carrier == "announce_entries" { 1 } else { 4096 }];
                let mut vocab = $k::TableVocabulary::new(&mut entries);
                if carrier == "announce_bytes" {
                    vocab.max_bytes = 8;
                }
                if carrier == "announce_second" && !$k::announce_read(&mut vocab, input, &mut $k::TableReport::default()) {
                    return "error: the first announcement was refused".to_string();
                }
                let mut report = $k::TableReport::default();
                let ok = $k::announce_read(&mut vocab, input, &mut report);
                if let Some(r) = $crate::refusal(report.refused, report.malformed, report.reason.name(), ok) {
                    return r;
                }
                $crate::accept("-")
            },
        }
    };
}

#[allow(unused_macros)]
macro_rules! packet_ops {
    ($k:ident, $read:ident, $write:ident) => {
        $crate::PacketOps {
            run: |input| {
                // the reader's buffer extends past the packet so every read
                // takes the window path; numBits is 8 * len
                let mut buffer = input.to_vec();
                buffer.resize(input.len() + 64, 0);
                let mut value = Default::default();
                let mut r = serialize::ReadStream::new(&buffer, input.len());
                if $k::$read(&mut r, &mut value).is_err() {
                    return $crate::refuse("packet_refused");
                }
                let size = (input.len() * 2 + 4096).next_multiple_of(8);
                let mut out = vec![0u8; size];
                let mut w = serialize::WriteStream::new(&mut out);
                if $k::$write(&mut w, &value).is_err() {
                    return "error: the port's writer refused the decoded value".to_string();
                }
                w.flush();
                $crate::accept(&$crate::sha256::hex(w.data()))
            },
        }
    };
}

mod dispatch {
    use super::*;
    include!(concat!(env!("OUT_DIR"), "/dispatch.rs"));
}

// One case, in this process: the verdict text, or a SKIP line's reason
// prefixed with "SKIP ".
fn run_case(carrier: &str, schema: &str, table: &str, input: &[u8]) -> String {
    match carrier {
        "wire" | "wire_retain" | "text" | "message" | "message_novocab" => {
            let Some(ops) = dispatch::table(schema, table) else {
                return format!("runner error: no Rust table binding for {schema} {table}");
            };
            match carrier {
                "wire" => (ops.wire)(input, false),
                "wire_retain" => (ops.wire)(input, true),
                "text" => (ops.text)(input),
                _ => match ops.message {
                    Some(m) => m(input, carrier == "message"),
                    None => format!("SKIP {table} in {schema} has no message form in the Rust port"),
                },
            }
        }
        "announce" | "announce_second" | "announce_bytes" | "announce_entries" => match dispatch::vocabulary(schema) {
            Some(ops) => (ops.run)(input, carrier),
            None => format!("runner error: no Rust vocabulary binding for {schema}"),
        },
        "packet" => match dispatch::packet(schema, table) {
            Some(ops) => (ops.run)(input),
            None => format!("runner error: no Rust packet binding for {schema} {table}"),
        },
        _ => format!("SKIP carrier {carrier} has no Rust API"),
    }
}

// The child: run one case on a 512 KiB thread under catch_unwind and print
// its answer on one line.
fn child(args: &[String]) -> ! {
    let [carrier, schema, table, path] = args else {
        eprintln!("usage: rust-hostile --case <carrier> <schema> <table> <input-file>");
        std::process::exit(2);
    };
    let input = fs::read(path).unwrap_or_else(|e| {
        println!("RESULT runner error: {path}: {e}");
        std::process::exit(0);
    });
    let message: Arc<Mutex<String>> = Arc::new(Mutex::new(String::new()));
    let hook_message = Arc::clone(&message);
    panic::set_hook(Box::new(move |info| {
        // a generated unit's file is named from its crate directory on
        let at = info
            .location()
            .map(|l| {
                let f = l.file();
                let f = f.find("/units/").map_or(f, |i| &f[i + 7..]);
                format!(" at {f}:{}", l.line())
            })
            .unwrap_or_default();
        let what = if let Some(s) = info.payload().downcast_ref::<&str>() {
            (*s).to_string()
        } else if let Some(s) = info.payload().downcast_ref::<String>() {
            s.clone()
        } else {
            "non-string payload".to_string()
        };
        *hook_message.lock().unwrap_or_else(|p| p.into_inner()) = format!("{what}{at}");
    }));
    let (c, s, t) = (carrier.clone(), schema.clone(), table.clone());
    let handle = thread::Builder::new()
        .stack_size(CASE_STACK)
        .spawn(move || panic::catch_unwind(AssertUnwindSafe(|| run_case(&c, &s, &t, &input))))
        .expect("spawn the case thread");
    let answer = match handle.join() {
        Ok(Ok(v)) => v,
        Ok(Err(_)) | Err(_) => {
            let m = message.lock().unwrap_or_else(|p| p.into_inner()).clone();
            format!("panicked: {}", m.replace(['\n', '\t'], " "))
        }
    };
    println!("RESULT {answer}");
    std::process::exit(0);
}

fn rows(path: &Path, fields: usize) -> Result<Vec<Vec<String>>, String> {
    let text = fs::read_to_string(path).map_err(|e| format!("{}: {e}", path.display()))?;
    let mut out = Vec::new();
    for line in text.lines() {
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        let f: Vec<String> = line.splitn(fields, '\t').map(str::to_string).collect();
        if f.len() != fields {
            return Err(format!("{}: line has {} fields, want {fields}: {line}", path.display(), f.len()));
        }
        out.push(f);
    }
    Ok(out)
}

// The parent's view of one child: its answer line, or how it died.
fn supervise(exe: &Path, row: &[String], corpus: &Path, budget: Duration) -> String {
    let input = corpus.join(&row[4]);
    let mut proc = match Command::new(exe)
        .arg("--case")
        .args([&row[1], &row[2], &row[3]])
        .arg(&input)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
    {
        Ok(p) => p,
        Err(e) => return format!("runner error: spawn: {e}"),
    };
    // drain both pipes off-thread so a chatty child cannot block on a full pipe
    let mut out = proc.stdout.take().expect("stdout");
    let mut err = proc.stderr.take().expect("stderr");
    let out_reader = thread::spawn(move || {
        let mut s = String::new();
        let _ = out.read_to_string(&mut s);
        s
    });
    let err_reader = thread::spawn(move || {
        let mut s = String::new();
        let _ = err.read_to_string(&mut s);
        s
    });
    let start = Instant::now();
    let status = loop {
        match proc.try_wait() {
            Ok(Some(status)) => break Some(status),
            Ok(None) if start.elapsed() > budget => {
                let _ = proc.kill();
                let _ = proc.wait();
                break None;
            }
            Ok(None) => thread::sleep(Duration::from_millis(2)),
            Err(e) => return format!("runner error: wait: {e}"),
        }
    };
    let stdout = out_reader.join().unwrap_or_default();
    let stderr = err_reader.join().unwrap_or_default();
    let Some(status) = status else {
        return format!("timeout: over the {} ms budget", budget.as_millis());
    };
    if let Some(line) = stdout.lines().find_map(|l| l.strip_prefix("RESULT ")) {
        return line.to_string();
    }
    let last = stderr.lines().filter(|l| !l.trim().is_empty()).last().unwrap_or("").to_string();
    #[cfg(unix)]
    {
        use std::os::unix::process::ExitStatusExt;
        if let Some(sig) = status.signal() {
            return format!("crashed: signal {sig}: {last}");
        }
    }
    format!("crashed: exit {status}: {last}")
}

fn main() {
    let args: Vec<String> = env::args().collect();
    if args.get(1).map(String::as_str) == Some("--case") {
        child(&args[2..]);
    }
    if args.len() != 4 {
        eprintln!("usage: rust-hostile <corpus-dir> <known-findings.tsv> <profile>");
        std::process::exit(2);
    }
    let corpus = Path::new(&args[1]).to_path_buf();
    let findings_path = Path::new(&args[2]);
    let profile = args[3].as_str();
    let budget = Duration::from_millis(
        env::var("HOSTILE_CASE_BUDGET_MS").ok().and_then(|v| v.parse().ok()).unwrap_or(5000),
    );
    let jobs = env::var("HOSTILE_JOBS")
        .ok()
        .and_then(|v| v.parse().ok())
        .unwrap_or_else(|| thread::available_parallelism().map_or(1, |n| n.get()).min(4))
        .max(1);

    // known findings: name<TAB>slug<TAB>input<TAB>one line. A name of the form
    // <case>@<profile> is a finding in that build profile only.
    let mut findings: HashMap<String, Vec<String>> = HashMap::new();
    if findings_path.exists() {
        match rows(findings_path, 4) {
            Ok(rs) => {
                for f in rs {
                    let key = match f[0].split_once('@') {
                        Some((name, p)) if p == profile => name.to_string(),
                        Some(_) => continue,
                        None => f[0].clone(),
                    };
                    findings.insert(key, f);
                }
            }
            Err(e) => {
                eprintln!("{e}");
                std::process::exit(2);
            }
        }
    }
    let manifest = match rows(&corpus.join("manifest.tsv"), 9) {
        Ok(m) => m,
        Err(e) => {
            eprintln!("{e}");
            std::process::exit(2);
        }
    };

    let exe = env::current_exe().expect("current_exe");
    let next = Arc::new(AtomicUsize::new(0));
    let results: Arc<Mutex<Vec<Option<String>>>> = Arc::new(Mutex::new(vec![None; manifest.len()]));
    let manifest = Arc::new(manifest);
    let workers: Vec<_> = (0..jobs)
        .map(|_| {
            let (next, results, manifest, exe, corpus) =
                (Arc::clone(&next), Arc::clone(&results), Arc::clone(&manifest), exe.clone(), corpus.clone());
            thread::spawn(move || loop {
                let i = next.fetch_add(1, Ordering::SeqCst);
                if i >= manifest.len() {
                    break;
                }
                let got = supervise(&exe, &manifest[i], &corpus, budget);
                results.lock().expect("results")[i] = Some(got);
            })
        })
        .collect();
    for w in workers {
        w.join().expect("worker");
    }
    let results = results.lock().expect("results");

    let (mut passed, mut carried, mut skipped) = (0, 0, 0);
    let mut bad = Vec::new();
    let mut stale = Vec::new();
    for (f, got) in manifest.iter().zip(results.iter()) {
        let got = got.clone().unwrap_or_else(|| "runner error: no result".to_string());
        let (name, carrier) = (&f[0], &f[1]);
        let expected = if f[6] == "accept" {
            accept(&f[8])
        } else if carrier == "packet" {
            // ports differ in naming a packet refusal; only the verdict compares
            refuse("packet_refused")
        } else {
            refuse(&f[7])
        };
        let finding = findings.get(name);
        if let Some(why) = got.strip_prefix("SKIP ") {
            println!("SKIP {name} carrier={carrier}: {why}");
            skipped += 1;
            continue;
        }
        if got == expected {
            if let Some(fc) = finding {
                println!("STALE {name}: FIX-CARD {} no longer reproduces ({profile})", fc[1]);
                stale.push(name.clone());
            } else {
                println!("PASS {name}");
                passed += 1;
            }
        } else if let Some(fc) = finding {
            println!("FIX-CARD: {}: {} {}", fc[1], fc[2], fc[3]);
            carried += 1;
        } else {
            println!("FAIL {name} carrier={carrier} got={got} want={expected}");
            bad.push(name.clone());
        }
    }
    println!(
        "rust ({profile}): {} cases: {passed} pass, {carried} carried as FIX-CARD, {} unexplained, {} stale, {skipped} skipped",
        manifest.len(),
        bad.len(),
        stale.len()
    );
    if !bad.is_empty() || !stale.is_empty() {
        std::process::exit(1);
    }
}
