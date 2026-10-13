// The generated wire modules are not hand-written and bear their own lint
// classes; the facade crate allows them crate-wide so clippy -D warnings runs
// clean over the hand-written facade (make/rust.mk cards-rust-test).
#![allow(
    dead_code,
    unused_imports,
    unused_variables,
    clippy::borrow_deref_ref,
    clippy::collapsible_if,
    clippy::derivable_impls,
    clippy::manual_is_multiple_of,
    clippy::match_single_binding,
    clippy::module_inception,
    clippy::needless_option_as_deref,
    clippy::redundant_pattern_matching,
    clippy::too_many_arguments,
    clippy::unnecessary_cast
)]

// The cards facade's Rust leg: L1 is the owning Record and the customer
// bounds, L2 parse once and render many. The generated variable modules are the
// wire and text codecs; this crate is the thin owning layer a customer imports
// (docs/SPEC-TABLES.md §2.5, §2.9), each rule ported from tables/cards/cards.go,
// parse.go and render.go.

mod card_table;
mod card_variable;
mod variable_runtime;
pub mod sha256;
mod card_descriptors;
pub use card_descriptors::*;

pub use card_table::*;
pub use variable_runtime::*;

/// The customer ceiling on one card's opaque body (docs/SPEC-TABLES.md §2.5).
pub const MAX_BODY_BYTES: usize = 64 << 10;

/// The customer ceiling on each collection field (docs/SPEC-TABLES.md §2.9).
pub const MAX_LIST_ENTRIES: usize = 512;

/// A typed work card that owns a private copy of its wire and its Card.
#[derive(Clone)]
pub struct Record {
    wire: Vec<u8>,
    card: Card,
    report: TableReport,
}

impl Record {
    /// A defensive copy of the wire bytes; mutating it never changes the record.
    pub fn wire(&self) -> Vec<u8> {
        self.wire.clone()
    }

    /// The typed card.
    pub fn card(&self) -> &Card {
        &self.card
    }

    /// The load/save report.
    pub fn report(&self) -> TableReport {
        self.report
    }
}

/// Load reads wire into a Record, refusing malformed input and the customer
/// bounds, never upgrading implicitly (cards.go Load). The original wire is
/// copied, never rewritten.
pub fn load(wire: &[u8]) -> Result<(Record, TableReport), String> {
    let mut report = TableReport::default();
    if card_load_measure(wire) < 0 {
        return Err("cards: wire is not a card".to_string());
    }
    let card = card_load(wire, &mut report).ok_or_else(|| "cards: wire is malformed".to_string())?;
    check_card(&card)?;
    let record = Record {
        wire: wire.to_vec(),
        card,
        report,
    };
    Ok((record, report))
}

/// FromJSON builds a Record from JSON text through the generated card_from_json,
/// then the generated save. Unknown, mismatched, clamped and malformed inputs
/// are refused, as are the customer bounds. Bytes are published only on complete
/// success (cards.go FromJSON).
pub fn from_json(text: &str) -> Result<Record, String> {
    let mut report = TableReport::default();
    let card = card_from_json(text, &mut report).ok_or_else(|| "cards: JSON is malformed".to_string())?;
    if report.unknown > 0 || report.kind_mismatch > 0 || report.clamped > 0 {
        return Err(format!(
            "cards: JSON has loss events (unknown={} kindMismatch={} clamped={}): refused, never truncated",
            report.unknown, report.kind_mismatch, report.clamped
        ));
    }
    check_card(&card)?;
    let wire = card_save(&card);
    Ok(Record { wire, card, report })
}

/// ToJSON writes the record's known-field JSON text and returns its load report.
/// The customer ceiling on the retained-record byte store (docs/SPEC-TABLES.md §6.6).
pub const MAX_RETAIN_BYTES: usize = 1 << 20;

/// The customer ceiling on the retained-id entry store (docs/SPEC-TABLES.md §6.6).
pub const MAX_RETAIN_IDS: usize = 4096;

/// Upgrade rewrites wire through the generated retaining path so that every
/// field the current declaration cannot name is preserved, byte for byte, at
/// the end of its own body (docs/SPEC-TABLES.md §6.6). It returns an error when
/// any value would be lost — RetainLost, KindMismatch, Clamped or Malformed —
/// or when the wire is refused; the input wire is never mutated and no Record
/// replacement occurs on error. The result is idempotent: upgrading an
/// already-upgraded wire is byte-for-byte the same (tla/CardVersioning.tla NoFieldLost,
/// BodyUnchanged, RefusalKeepsOriginal, ShippedUpgradeSucceeds; ports tables/cards/upgrade.go Upgrade).
pub fn upgrade(wire: &[u8]) -> Result<(Vec<u8>, TableReport), String> {
    upgrade_with_capacity(wire, MAX_RETAIN_BYTES, MAX_RETAIN_IDS)
}

/// upgrade_with_capacity is the package-private retention seam (ports tables/cards/upgrade.go upgrade):
/// upgrade fixes the two customer capacities, and refusal probes drive smaller caller-owned
/// storage through the same path so a tiny capacity can be shown to name RetainLost and
/// return Err while the input is preserved (docs/SPEC-TABLES.md §6.6).
pub fn upgrade_with_capacity(
    wire: &[u8],
    retain_bytes: usize,
    retain_ids: usize,
) -> Result<(Vec<u8>, TableReport), String> {
    let mut report = TableReport::default();
    if card_load_measure(wire) < 0 {
        return Err("cards: upgrade: wire is not a card".to_string());
    }
    let mut retain = TableRetain {
        bytes: Vec::with_capacity(retain_bytes),
        ids: Vec::with_capacity(retain_ids),
    };
    let card = card_load_retain(wire, &mut retain, &mut report)
        .ok_or_else(|| "cards: upgrade: wire is malformed".to_string())?;
    check_card(&card).map_err(|e| format!("cards: upgrade: {e}"))?;
    let out = card_save_retain(&card, &mut retain, &mut report);
    loss_err(&report)?;
    Ok((out, report))
}

fn loss_err(report: &TableReport) -> Result<(), String> {
    if report.retain_lost > 0 {
        return Err(format!(
            "cards: upgrade: {} retained field(s) lost, refusing to replace",
            report.retain_lost
        ));
    }
    if report.kind_mismatch > 0 {
        return Err(format!(
            "cards: upgrade: {} kind mismatch(es), refusing to replace",
            report.kind_mismatch
        ));
    }
    if report.clamped > 0 {
        return Err(format!(
            "cards: upgrade: {} clamped value(s), refusing to replace",
            report.clamped
        ));
    }
    if report.malformed {
        return Err("cards: upgrade: malformed wire, refusing to replace".to_string());
    }
    Ok(())
}

/// ReadStrict reads wire and refuses any data the current declaration cannot
/// support: Unknown, KindMismatch, Clamped, Malformed and every load failure
/// (docs/SPEC-TABLES.md §4; ports tables/cards/upgrade.go ReadStrict).
pub fn read_strict(wire: &[u8]) -> Result<Record, String> {
    let (rec, report) = load(wire)?;
    if report.unknown > 0 {
        return Err(format!(
            "cards: strict: {} unknown field(s), unsupported data",
            report.unknown
        ));
    }
    if report.kind_mismatch > 0 {
        return Err(format!(
            "cards: strict: {} kind mismatch(es), unsupported data",
            report.kind_mismatch
        ));
    }
    if report.clamped > 0 {
        return Err(format!(
            "cards: strict: {} clamped value(s), unsupported data",
            report.clamped
        ));
    }
    if report.malformed {
        return Err("cards: strict: malformed wire, unsupported data".to_string());
    }
    Ok(rec)
}

/// format_report formats a TableReport as Name=value lines in declaration order
/// (tables/cards/conformance/FORMAT.md "The read report").
pub fn format_report(r: &TableReport) -> String {
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
    s
}

/// refusal_class maps a facade error to one class of the closed list FORMAT.md
/// fixes, never free text, so every language can spell the same refusal
/// (tables/cards/conformance/FORMAT.md "Refusals").
pub fn refusal_class(err: &str) -> String {
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

/// A value the writer cannot name refuses rather than emitting "???" (§16).
pub fn to_json(record: &Record) -> Result<(String, TableReport), String> {
    let text = card_to_json_checked(&record.card)
        .ok_or_else(|| "cards: the card has no JSON spelling".to_string())?;
    Ok((text, record.report))
}

/// The checked-write helper: the customer bounds, enforced before bytes are
/// published (cards.go checkCard).
fn check_card(card: &Card) -> Result<(), String> {
    if let Some(body) = &card.body {
        if body.len() > MAX_BODY_BYTES {
            return Err(format!(
                "cards: body is {} bytes, past the {}-byte ceiling",
                body.len(),
                MAX_BODY_BYTES
            ));
        }
    }
    check_path_list(&card.paths, "paths")?;
    check_path_list(&card._new, "_new")?;
    check_path_list(&card.shared, "shared")?;
    check_id_list(&card.needs, "needs")?;
    check_extras(&card.extra)?;
    check_consumers(&card.consumers)?;
    Ok(())
}

fn check_path_list(list: &[card_table::PathText], name: &str) -> Result<(), String> {
    if list.len() > MAX_LIST_ENTRIES {
        return Err(format!(
            "cards: {} has {} entries, past the {}-entry ceiling",
            name,
            list.len(),
            MAX_LIST_ENTRIES
        ));
    }
    Ok(())
}

fn check_id_list(list: &[card_table::CardID], name: &str) -> Result<(), String> {
    if list.len() > MAX_LIST_ENTRIES {
        return Err(format!(
            "cards: {} has {} entries, past the {}-entry ceiling",
            name,
            list.len(),
            MAX_LIST_ENTRIES
        ));
    }
    Ok(())
}

fn check_consumers(list: &[card_table::Consumer]) -> Result<(), String> {
    if list.len() > MAX_LIST_ENTRIES {
        return Err(format!(
            "cards: consumers has {} entries, past the {}-entry ceiling",
            list.len(),
            MAX_LIST_ENTRIES
        ));
    }
    Ok(())
}

fn check_extras(list: &[KeyValue]) -> Result<(), String> {
    if list.len() > MAX_LIST_ENTRIES {
        return Err(format!(
            "cards: extra has {} entries, past the {}-entry ceiling",
            list.len(),
            MAX_LIST_ENTRIES
        ));
    }
    Ok(())
}

// L2: parse once, render many (docs/SPEC-TABLES.md §2, §6.2), ported from
// tables/cards/parse.go and render.go. Parse reads the admitted brief text
// — the header block of KEY: value lines under the RESULT line, known keys
// into typed fields, unknown keys into extra in arrival order, the body the
// rest byte for byte — and publishes a typed Record only on complete checked
// success. Render is the informational text view: RESULT first, the mapped
// keys in a fixed documented order, extras in stored order, then the body
// bytes verbatim.

// The stranded header keys refused once prose has begun (parse.go
// strandedKeys, nova-sprint pkg/swarm/lintheader.go cardHeaderKeys).
const STRANDED_KEYS: &[&str] = &[
    "KIND", "PATHS", "TEST", "LEGS", "SOURCE", "START", "STOP", "DEPENDS-ON",
    "PRIORITY", "NEW", "SHARED", "REPO", "BASE", "tier", "RESULT",
];

// The mapped header vocabulary, minus RESULT and the agreeing tier line
// (parse.go reservedHeaderKeys); the two are handled before this set.
const RESERVED_HEADER_KEYS: &[&str] = &[
    "REPO", "BASE", "KIND", "PRIORITY", "PATHS", "NEW", "SHARED", "DEPENDS-ON",
    "TEST", "DONE-WHEN", "DEADLINE", "SOURCE",
];

fn is_reserved(key: &str) -> bool {
    RESERVED_HEADER_KEYS.contains(&key)
}

fn is_stranded(key: &str) -> bool {
    STRANDED_KEYS.contains(&key)
}

/// kv — one extra header entry, kept in arrival order (parse.go kv).
struct Kv {
    key: String,
    value: String,
}

/// parsedCard — the intermediate result of reading header lines (parse.go
/// parsedCard), before any generated storage exists.
#[derive(Default)]
struct ParsedCard {
    id: String,
    sha: String,
    tier: Tier,
    repo: String,
    base: String,
    kind: String,
    priority: Priority,
    paths: Vec<String>,
    news: Vec<String>,
    shared: Vec<String>,
    needs: Vec<String>,
    test: String,
    done_when: String,
    source: String,
    deadline: i32,
    extras: Vec<Kv>,
}

/// line — one physical line: its content without the LF/CRLF terminator and
/// the byte offset just past its terminator (parse.go lineRef).
struct Line<'a> {
    content: &'a str,
    term_end: usize,
    num: usize,
}

/// Parse reads admitted brief text and publishes a typed Record only on
/// complete checked success (docs/SPEC-TABLES.md §2, §6.2). The body is the
/// exact suffix immediately after the last header line's terminator; header
/// layout may canonicalize, body bytes never do (parse.go Parse).
pub fn parse(text: &[u8]) -> Result<Record, String> {
    let s = std::str::from_utf8(text).map_err(|_| "cards: brief is not valid UTF-8".to_string())?;
    if text.contains(&0) {
        return Err("cards: brief contains a NUL byte".to_string());
    }

    let lines = split_lines(text);
    if lines.is_empty() {
        return Err("cards: brief has no RESULT line".to_string());
    }

    let (key, value) = match_header(lines[0].content);
    if key.as_deref() != Some("RESULT") {
        return Err("cards: line 1 is not a RESULT line".to_string());
    }
    let mut p = ParsedCard::default();
    apply_result(&mut p, &value)?;

    let mut last_header_end = lines[0].term_end;
    let mut seen = std::collections::HashMap::new();
    seen.insert("RESULT".to_string(), 1usize);
    let mut prose = false;
    for ln in lines.iter().skip(1) {
        if is_blank(ln.content) {
            continue;
        }
        let (k, v) = match_header(ln.content);
        if k.is_none() {
            prose = true;
            continue;
        }
        let k = k.unwrap();
        if prose {
            if is_stranded(&k) {
                return Err(format!("cards: stranded header key {:?} on line {}", k, ln.num));
            }
            continue;
        }
        apply_header(&mut p, &k, &v, ln.num, &mut seen)?;
        last_header_end = ln.term_end;
    }

    let body = &s[last_header_end..];
    build(&p, body)
}

/// splitLines returns every physical line with its content and terminator end
/// offset. A trailing LF does not invent a final empty line (parse.go
/// splitLines).
fn split_lines(text: &[u8]) -> Vec<Line<'_>> {
    let mut refs = Vec::new();
    let mut i = 0;
    let mut n = 1;
    while i < text.len() {
        let start = i;
        let (line_end, term_end) = match text[i..].iter().position(|&b| b == b'\n') {
            Some(j) => (start + j, start + j + 1),
            None => (text.len(), text.len()),
        };
        let mut content = &text[start..line_end];
        if !content.is_empty() && content[content.len() - 1] == b'\r' {
            content = &content[..content.len() - 1];
        }
        refs.push(Line {
            content: std::str::from_utf8(content).unwrap(),
            term_end,
            num: n,
        });
        n += 1;
        i = term_end;
    }
    refs
}

/// matchHeader recognizes a header key line: a key of one letter then
/// letters/digits/hyphens, a colon, optional whitespace, the rest as the
/// value (parse.go headerLineRE, applied to a line with no terminator).
fn match_header(line: &str) -> (Option<String>, String) {
    let Some(colon) = line.find(':') else {
        return (None, String::new());
    };
    let key = &line[..colon];
    if key.is_empty() || !key.as_bytes()[0].is_ascii_alphabetic() {
        return (None, String::new());
    }
    if !key.bytes().all(|b| b.is_ascii_alphanumeric() || b == b'-') {
        return (None, String::new());
    }
    let rest = &line[colon + 1..];
    let value = rest.trim_start_matches([' ', '\t', '\r', '\x0c']);
    (Some(key.to_string()), value.to_string())
}

/// applyResult parses the RESULT value "id sha=<sha> tier: <tier>" and
/// validates each part (docs/SPEC-TABLES.md §2, §4; parse.go applyResult).
fn apply_result(p: &mut ParsedCard, value: &str) -> Result<(), String> {
    let Some(i) = value.rfind(" tier:") else {
        return Err("cards: RESULT line has no \" tier:\"".to_string());
    };
    let tier_str = value[i + " tier:".len()..].trim();
    let rest = &value[..i];
    let Some(j) = rest.rfind(" sha=") else {
        return Err("cards: RESULT line has no \" sha=\"".to_string());
    };
    let id_str = rest[..j].trim();
    let sha_str = &rest[j + " sha=".len()..];
    check_str_bound("id", id_str, 64)?;
    if !is_hex(sha_str) || (sha_str.len() != 12 && sha_str.len() != 40) {
        return Err(format!("cards: RESULT sha {:?} is not 12 or 40 hex digits", sha_str));
    }
    let tier = parse_tier(tier_str).map_err(|e| format!("cards: RESULT tier: {e}"))?;
    p.id = id_str.to_string();
    p.sha = sha_str.to_string();
    p.tier = tier;
    Ok(())
}

/// applyHeader dispatches one header line to its field, an agreeing tier, or
/// an extra, refusing duplicates and CR/LF inside any single-line value
/// (parse.go applyHeader).
fn apply_header(
    p: &mut ParsedCard,
    key: &str,
    value: &str,
    num: usize,
    seen: &mut std::collections::HashMap<String, usize>,
) -> Result<(), String> {
    if value.contains('\r') || value.contains('\n') {
        return Err(format!("cards: {} on line {} has a CR or LF in its value", key, num));
    }
    if key == "tier" {
        if let Some(&prev) = seen.get("tier") {
            return Err(format!("cards: duplicate header key {:?} on lines {} and {}", key, prev, num));
        }
        seen.insert("tier".to_string(), num);
        let t = parse_tier(value.trim()).map_err(|e| format!("cards: tier on line {}: {}", num, e))?;
        if t != p.tier {
            return Err(format!("cards: tier on line {} disagrees with RESULT", num));
        }
        return Ok(());
    }
    if key == "RESULT" {
        return Err(format!(
            "cards: duplicate header key {:?} on lines {} and {}",
            key,
            seen.get("RESULT").copied().unwrap_or(1),
            num
        ));
    }
    if is_reserved(key) {
        if let Some(&prev) = seen.get(key) {
            return Err(format!("cards: duplicate header key {:?} on lines {} and {}", key, prev, num));
        }
        seen.insert(key.to_string(), num);
        return assign_mapped(p, key, value, num);
    }
    check_str_bound("extra key", key, 32).map_err(|e| format!("cards: line {}: {}", num, e))?;
    p.extras.push(Kv { key: key.to_string(), value: value.to_string() });
    Ok(())
}

/// assignMapped fills one mapped header field with its checked value
/// (parse.go assignMapped).
fn assign_mapped(p: &mut ParsedCard, key: &str, value: &str, num: usize) -> Result<(), String> {
    match key {
        "REPO" => assign_str(&mut p.repo, value, 96, "REPO", num),
        "BASE" => assign_str(&mut p.base, value, 96, "BASE", num),
        "KIND" => assign_str(&mut p.kind, value, 24, "KIND", num),
        "PRIORITY" => {
            let pr = parse_priority(value.trim()).map_err(|e| format!("cards: PRIORITY on line {}: {}", num, e))?;
            p.priority = pr;
            Ok(())
        }
        "PATHS" => {
            p.paths = split_list_checked(value, 192, true, "PATHS", num)?;
            Ok(())
        }
        "NEW" => {
            p.news = split_list_checked(value, 192, false, "NEW", num)?;
            Ok(())
        }
        "SHARED" => {
            p.shared = split_list_checked(value, 192, false, "SHARED", num)?;
            Ok(())
        }
        "DEPENDS-ON" => {
            p.needs = split_list_checked(value, 64, false, "DEPENDS-ON", num)?;
            Ok(())
        }
        "TEST" => assign_str(&mut p.test, value, 192, "TEST", num),
        "DONE-WHEN" => {
            p.done_when = value.to_string();
            Ok(())
        }
        "DEADLINE" => {
            let n = parse_deadline(value).map_err(|e| format!("cards: DEADLINE on line {}: {}", num, e))?;
            p.deadline = n;
            Ok(())
        }
        "SOURCE" => {
            p.source = value.to_string();
            Ok(())
        }
        _ => Ok(()),
    }
}

fn assign_str(dst: &mut String, value: &str, bound: usize, name: &str, num: usize) -> Result<(), String> {
    check_str_bound(name, value, bound).map_err(|e| format!("cards: {} on line {}: {}", name, num, e))?;
    *dst = value.to_string();
    Ok(())
}

fn check_str_bound(name: &str, value: &str, bound: usize) -> Result<(), String> {
    if value.len() > bound {
        return Err(format!("cards: {} is {} bytes, past the {}-byte bound", name, value.len(), bound));
    }
    Ok(())
}

/// splitListChecked splits a comma-separated list, trims elements, drops empty
/// ones, and enforces the element bound and the entry ceiling (docs/SPEC-TABLES.md
/// §2.9; parse.go splitListChecked). A whole value of "-" or empty is an empty
/// list; PATHS alone also takes the customer sentinel "none".
fn split_list_checked(value: &str, bound: usize, none_sentinel: bool, name: &str, num: usize) -> Result<Vec<String>, String> {
    let v = value.trim();
    if v.is_empty() || v == "-" {
        return Ok(Vec::new());
    }
    if none_sentinel && v == "none" {
        return Ok(Vec::new());
    }
    let mut out = Vec::new();
    for part in v.split(',') {
        let part = part.trim();
        if part.is_empty() {
            continue;
        }
        check_str_bound(&format!("{} element", name), part, bound)
            .map_err(|e| format!("cards: {} on line {}: {}", name, num, e))?;
        out.push(part.to_string());
    }
    if out.len() > MAX_LIST_ENTRIES {
        return Err(format!(
            "cards: {} on line {} has {} entries, past the {}-entry ceiling",
            name,
            num,
            out.len(),
            MAX_LIST_ENTRIES
        ));
    }
    Ok(out)
}

fn parse_tier(s: &str) -> Result<Tier, String> {
    match s {
        "flash" => Ok(Tier::Flash),
        "pro" => Ok(Tier::Pro),
        "heavy" => Ok(Tier::Heavy),
        "frontier" => Ok(Tier::Frontier),
        _ => Err(format!("unknown tier {:?}", s)),
    }
}

fn parse_priority(s: &str) -> Result<Priority, String> {
    match s {
        "normal" => Ok(Priority::Normal),
        "high" => Ok(Priority::High),
        "critical" => Ok(Priority::Critical),
        "blocker" => Ok(Priority::Blocker),
        _ => Err(format!("unknown priority {:?}", s)),
    }
}

fn parse_deadline(s: &str) -> Result<i32, String> {
    let s = s.trim();
    const PREFIX: &str = "finish within ";
    const SUFFIX: &str = " minutes.";
    if !s.starts_with(PREFIX) || !s.ends_with(SUFFIX) {
        return Err("not of the form \"finish within N minutes.\"".to_string());
    }
    let mid = &s[PREFIX.len()..s.len() - SUFFIX.len()];
    if mid.is_empty() || !mid.bytes().all(|b| b.is_ascii_digit()) {
        return Err("not of the form \"finish within N minutes.\"".to_string());
    }
    let n: i64 = mid.parse().map_err(|_| "minutes out of range".to_string())?;
    if !(0..=100000).contains(&n) {
        return Err("minutes out of range".to_string());
    }
    Ok(n as i32)
}

fn is_hex(s: &str) -> bool {
    if s.is_empty() {
        return false;
    }
    s.bytes().all(|c| c.is_ascii_hexdigit())
}

fn is_blank(line: &str) -> bool {
    line.bytes().all(|b| b == b' ' || b == b'\t' || b == b'\r')
}

/// build publishes the parsed card through the generated save and the
/// checked-write bounds (docs/SPEC-TABLES.md §6.2; parse.go build). Bytes are
/// published only on complete success; a refused write mutates nothing.
fn build(p: &ParsedCard, body: &str) -> Result<Record, String> {
    let card = Card {
        id: p.id.clone(),
        sha: p.sha.clone(),
        tier: p.tier,
        repo: p.repo.clone(),
        _base: p.base.clone(),
        kind: p.kind.clone(),
        priority: p.priority,
        paths: p.paths.iter().map(|s| PathText { s: s.clone() }).collect(),
        _new: p.news.iter().map(|s| PathText { s: s.clone() }).collect(),
        shared: p.shared.iter().map(|s| PathText { s: s.clone() }).collect(),
        needs: p.needs.iter().map(|s| CardID { s: s.clone() }).collect(),
        test: p.test.clone(),
        done: if p.done_when.is_empty() {
            None
        } else {
            Some(std::rc::Rc::from(p.done_when.as_str()))
        },
        deadline_minutes: p.deadline,
        source: if p.source.is_empty() {
            None
        } else {
            Some(std::rc::Rc::from(p.source.as_str()))
        },
        body: if body.is_empty() {
            None
        } else {
            Some(std::rc::Rc::from(body))
        },
        extra: p
            .extras
            .iter()
            .map(|e| KeyValue {
                key: e.key.clone(),
                value: Some(std::rc::Rc::from(e.value.as_str())),
            })
            .collect(),
        ..Default::default()
    };
    check_card(&card)?;
    let wire = card_save(&card);
    Ok(Record {
        wire,
        card,
        report: TableReport::default(),
    })
}

/// Render reads the record's typed card and returns its informational text
/// (docs/SPEC-TABLES.md §6.2, §16; render.go Render): RESULT first, the mapped
/// keys in a fixed documented order, extras in stored order, then the body
/// bytes verbatim.
pub fn render(record: &Record) -> Result<Vec<u8>, String> {
    let card = &record.card;
    let mut out = String::new();
    out.push_str("RESULT: ");
    out.push_str(&card.id);
    out.push_str(" sha=");
    out.push_str(&card.sha);
    out.push_str(" tier: ");
    out.push_str(tier_string(card.tier)?);
    out.push('\n');

    emit_str(&mut out, "REPO", &card.repo);
    emit_str(&mut out, "BASE", &card._base);
    emit_str(&mut out, "KIND", &card.kind);
    let prio = priority_string(card.priority)?;
    if !prio.is_empty() {
        out.push_str("PRIORITY: ");
        out.push_str(prio);
        out.push('\n');
    }
    emit_list(&mut out, "PATHS", &card.paths);
    emit_list(&mut out, "NEW", &card._new);
    emit_list(&mut out, "SHARED", &card.shared);
    emit_id_list(&mut out, "DEPENDS-ON", &card.needs);
    emit_str(&mut out, "TEST", &card.test);
    emit_opt(&mut out, "DONE-WHEN", &card.done);
    emit_deadline(&mut out, card.deadline_minutes);
    emit_opt(&mut out, "SOURCE", &card.source);

    for e in &card.extra {
        out.push_str(&e.key);
        out.push_str(": ");
        if let Some(v) = &e.value {
            out.push_str(v);
        }
        out.push('\n');
    }

    if let Some(b) = &card.body {
        out.push_str(b);
    }
    Ok(out.into_bytes())
}

fn tier_string(t: Tier) -> Result<&'static str, String> {
    match t {
        Tier::Flash => Ok("flash"),
        Tier::Pro => Ok("pro"),
        Tier::Heavy => Ok("heavy"),
        Tier::Frontier => Ok("frontier"),
        _ => Err(format!("cards: tier {:?} has no header spelling", t)),
    }
}

fn priority_string(p: Priority) -> Result<&'static str, String> {
    match p {
        Priority::None => Ok(""),
        Priority::Normal => Ok("normal"),
        Priority::High => Ok("high"),
        Priority::Critical => Ok("critical"),
        Priority::Blocker => Ok("blocker"),
    }
}

fn emit_str(out: &mut String, key: &str, value: &str) {
    if value.is_empty() {
        return;
    }
    out.push_str(key);
    out.push_str(": ");
    out.push_str(value);
    out.push('\n');
}

fn emit_opt(out: &mut String, key: &str, slot: &Option<std::rc::Rc<str>>) {
    if let Some(v) = slot {
        out.push_str(key);
        out.push_str(": ");
        out.push_str(v);
        out.push('\n');
    }
}

fn emit_deadline(out: &mut String, minutes: i32) {
    if minutes == 0 {
        return;
    }
    out.push_str("DEADLINE: finish within ");
    out.push_str(&minutes.to_string());
    out.push_str(" minutes.");
    out.push('\n');
}

fn emit_list(out: &mut String, key: &str, elems: &[PathText]) {
    if elems.is_empty() {
        return;
    }
    out.push_str(key);
    out.push_str(": ");
    for (i, e) in elems.iter().enumerate() {
        if i > 0 {
            out.push_str(", ");
        }
        out.push_str(&e.s);
    }
    out.push('\n');
}

fn emit_id_list(out: &mut String, key: &str, elems: &[CardID]) {
    if elems.is_empty() {
        return;
    }
    out.push_str(key);
    out.push_str(": ");
    for (i, e) in elems.iter().enumerate() {
        if i > 0 {
            out.push_str(", ");
        }
        out.push_str(&e.s);
    }
    out.push('\n');
}

/// TaskHash calculates the canonical SHA-256 task hash of a record
/// (docs/SPEC-TABLES.md §5: identity is the fields a task is; ports tables/cards/edit.go TaskHash; tla/CardHops.tla TaskHashStable).
/// SHA-256 over body, paths, new, shared, test and done in that order,
/// each text as an unsigned 64-bit little-endian byte length then its bytes,
/// each list as an unsigned 64-bit little-endian count then each length-prefixed text;
/// needs, tier, priority and machine fields excluded.
/// Refuses a record whose report has Unknown, KindMismatch, Clamped, RetainLost or Malformed.
pub fn task_hash(record: &Record) -> Result<[u8; 32], String> {
    refuse_lossy(&record.report)?;
    let card = &record.card;
    let mut h = sha256::Sha256::new();

    // 1. body
    hash_opt_text(&mut h, card.body.as_deref());
    // 2. paths
    hash_path_list(&mut h, &card.paths);
    // 3. _new
    hash_path_list(&mut h, &card._new);
    // 4. shared
    hash_path_list(&mut h, &card.shared);
    // 5. test
    hash_text(&mut h, card.test.as_bytes());
    // 6. done
    hash_opt_text(&mut h, card.done.as_deref());

    if std::env::var("CARDS_RUST_NEGATIVE_HASH").is_ok() || std::env::var("CARDS_RUST_SABOTAGE_HASH").is_ok() {
        // a switch that adds needs to the hash must FAIL taskhash alone
        hash_id_list(&mut h, &card.needs);
    }

    Ok(h.finalize())
}

/// TaskHashHex returns the 64 lowercase hex digits of TaskHash.
pub fn task_hash_hex(record: &Record) -> Result<String, String> {
    let digest = task_hash(record)?;
    let mut s = String::with_capacity(64);
    for b in digest {
        use std::fmt::Write;
        write!(&mut s, "{:02x}", b).unwrap();
    }
    Ok(s)
}

fn refuse_lossy(rep: &TableReport) -> Result<(), String> {
    if rep.unknown > 0 || rep.kind_mismatch > 0 || rep.clamped > 0 || rep.retain_lost > 0 || rep.malformed {
        return Err(format!(
            "cards: record has loss events (unknown={} kindMismatch={} clamped={} retainLost={} malformed={}): refused, never hashed or rewritten",
            rep.unknown, rep.kind_mismatch, rep.clamped, rep.retain_lost, rep.malformed
        ));
    }
    Ok(())
}

fn hash_u64(h: &mut sha256::Sha256, v: u64) {
    h.update(&v.to_le_bytes());
}

fn hash_text(h: &mut sha256::Sha256, text: &[u8]) {
    hash_u64(h, text.len() as u64);
    h.update(text);
}

fn hash_opt_text(h: &mut sha256::Sha256, text: Option<&str>) {
    match text {
        Some(s) => hash_text(h, s.as_bytes()),
        None => hash_u64(h, 0),
    }
}

fn hash_path_list(h: &mut sha256::Sha256, list: &[PathText]) {
    hash_u64(h, list.len() as u64);
    for elem in list {
        hash_text(h, elem.s.as_bytes());
    }
}

fn hash_id_list(h: &mut sha256::Sha256, list: &[CardID]) {
    hash_u64(h, list.len() as u64);
    for elem in list {
        hash_text(h, elem.s.as_bytes());
    }
}

/// SetNeeds replaces the record's needs list atomically (docs/SPEC-TABLES.md §2.9; ports tables/cards/edit.go SetNeeds).
pub fn set_needs(record: &mut Record, needs: &[String]) -> Result<(), String> {
    validate_strings("needs", needs, 64)?;
    if needs.len() > MAX_LIST_ENTRIES {
        return Err(format!(
            "cards: needs has {} entries, past the {}-entry ceiling (docs/SPEC-TABLES.md §2.9)",
            needs.len(),
            MAX_LIST_ENTRIES
        ));
    }
    edit(record, |card| {
        card.needs = needs.iter().map(|s| CardID { s: s.clone() }).collect();
        Ok(())
    })
}

/// SetPaths replaces the record's paths list atomically (docs/SPEC-TABLES.md §2.9; ports tables/cards/edit.go SetPaths).
pub fn set_paths(record: &mut Record, paths: &[String]) -> Result<(), String> {
    validate_strings("paths", paths, 192)?;
    if paths.len() > MAX_LIST_ENTRIES {
        return Err(format!(
            "cards: paths has {} entries, past the {}-entry ceiling (docs/SPEC-TABLES.md §2.9)",
            paths.len(),
            MAX_LIST_ENTRIES
        ));
    }
    edit(record, |card| {
        card.paths = paths.iter().map(|s| PathText { s: s.clone() }).collect();
        Ok(())
    })
}

/// SetTier changes a record's tier atomically (docs/SPEC-TABLES.md §8; ports tables/cards/edit.go SetTier).
pub fn set_tier(record: &mut Record, tier: Tier) -> Result<(), String> {
    match tier {
        Tier::Flash | Tier::Pro | Tier::Heavy | Tier::Frontier => (),
        _ => return Err("cards: tier is not a declared variant".to_string()),
    }
    edit(record, |card| {
        card.tier = tier;
        Ok(())
    })
}

/// SetPriority changes a record's priority atomically (docs/SPEC-TABLES.md §8; ports tables/cards/edit.go SetPriority).
pub fn set_priority(record: &mut Record, priority: Priority) -> Result<(), String> {
    match priority {
        Priority::Normal | Priority::High | Priority::Critical | Priority::Blocker => (),
        _ => return Err("cards: priority is not a declared variant".to_string()),
    }
    edit(record, |card| {
        card.priority = priority;
        Ok(())
    })
}

/// SetColumn changes a record's column atomically (docs/SPEC-TABLES.md §8; ports tables/cards/edit.go SetColumn).
pub fn set_column(record: &mut Record, column: Column) -> Result<(), String> {
    match column {
        Column::Waiting | Column::Ready | Column::Working | Column::Review |
        Column::Merging | Column::Landed | Column::Plan | Column::Check => (),
        _ => return Err("cards: column is not a declared variant".to_string()),
    }
    edit(record, |card| {
        card.column = column;
        Ok(())
    })
}

/// SetHolder changes a record's holder atomically (docs/SPEC-TABLES.md §6.1; ports tables/cards/edit.go SetHolder).
pub fn set_holder(record: &mut Record, holder: &str) -> Result<(), String> {
    validate_strings("holder", &[holder.to_string()], 32)?;
    edit(record, |card| {
        card.holder = holder.to_string();
        Ok(())
    })
}

/// edit is the shared atomic rewrite (docs/SPEC-TABLES.md §6.2; tla/CardHops.tla EditDropsUnknown).
fn edit<F>(r: &mut Record, mutate: F) -> Result<(), String>
where
    F: FnOnce(&mut Card) -> Result<(), String>,
{
    refuse_lossy(&r.report)?;
    let mut card = r.card.clone();
    mutate(&mut card)?;
    check_card(&card)?;
    let wire = card_save(&card);
    r.card = card;
    r.wire = wire;
    Ok(())
}

fn validate_strings(name: &str, values: &[String], bound: usize) -> Result<(), String> {
    for s in values {
        if s.len() > bound {
            return Err(format!(
                "cards: {} element is {} bytes, past its {}-byte bound",
                name,
                s.len(),
                bound
            ));
        }
        if s.as_bytes().contains(&0) {
            return Err(format!(
                "cards: {} has an element that is not well-formed UTF-8 without NUL",
                name
            ));
        }
    }
    Ok(())
}

/// apply_edit applies one edit command string like "needs=a,b", "tier=heavy",
/// "priority=blocker", "paths=x,y", "column=landed", "holder=h" to a record.
pub fn apply_edit(record: &mut Record, edit_cmd: &str) -> Result<(), String> {
    let Some(eq_idx) = edit_cmd.find('=') else {
        return Err("cards: edit line carries no =".to_string());
    };
    let key = &edit_cmd[..eq_idx];
    let val = &edit_cmd[eq_idx + 1..];
    match key {
        "needs" => {
            let list = split_comma(val);
            set_needs(record, &list)
        }
        "paths" => {
            let list = split_comma(val);
            set_paths(record, &list)
        }
        "tier" => {
            let t = parse_tier(val)?;
            set_tier(record, t)
        }
        "priority" => {
            let p = parse_priority(val)?;
            set_priority(record, p)
        }
        "column" => {
            let c = parse_column(val)?;
            set_column(record, c)
        }
        "holder" => {
            set_holder(record, val)
        }
        _ => Err(format!("cards: unknown edit key {:?}", key)),
    }
}

fn split_comma(s: &str) -> Vec<String> {
    if s.is_empty() {
        return Vec::new();
    }
    s.split(',').map(|p| p.to_string()).collect()
}

pub fn parse_column(s: &str) -> Result<Column, String> {
    match s {
        "waiting" => Ok(Column::Waiting),
        "ready" => Ok(Column::Ready),
        "working" => Ok(Column::Working),
        "review" => Ok(Column::Review),
        "merging" => Ok(Column::Merging),
        "landed" => Ok(Column::Landed),
        "plan" => Ok(Column::Plan),
        "check" => Ok(Column::Check),
        _ => Err(format!("unknown column {:?}", s)),
    }
}

#[cfg(test)]
#[path = "tests.rs"]
mod tests;

fn type_of(_table: &str, f: &TableFieldDesc) -> &'static str {
    match f.kind {
        12 | 17 => "string",
        4 => "int32",
        5 => "int64",
        30 => match f.name {
            "tier" => "Tier",
            "priority" => "Priority",
            "column" => "Column",
            _ => "?",
        },
        14 => match f.name {
            "paths" | "_new" | "shared" => "PathText",
            "needs" => "CardID",
            "extra" => "KeyValue",
            "consumers" => "Consumer",
            _ => "?",
        },
        _ => "?",
    }
}

/// Render the unit descriptors as view.txt (docs/SPEC-TABLES.md §8).
pub fn render_view() -> Vec<u8> {
    let mut out = String::new();
    for t in TABLES.iter() {
        for f in t.fields.iter() {
            let shape = match f.shape {
                SHAPE_VALUE => "value",
                SHAPE_LIST => "list",
                SHAPE_BLOB => "blob",
                _ => "?",
            };
            let bound = if shape == "value" && f.bound > 0 {
                f.bound.to_string()
            } else {
                "-".to_string()
            };
            use std::fmt::Write;
            let _ = writeln!(&mut out, "{} {} {} {} {}", t.name, f.name, type_of(t.name, f), shape, bound);
        }
    }
    out.into_bytes()
}
