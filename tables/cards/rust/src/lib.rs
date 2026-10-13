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

#[cfg(test)]
#[path = "tests.rs"]
mod tests;
