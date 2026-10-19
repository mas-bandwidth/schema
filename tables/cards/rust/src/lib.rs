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
mod card_descriptors;

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

/// tier_parse resolves a header tier spelling to its variant (the driver's
/// edit surface uses it too).
pub fn tier_parse(s: &str) -> Result<Tier, String> {
    parse_tier(s)
}

/// priority_parse resolves a header priority spelling to its variant.
pub fn priority_parse(s: &str) -> Result<Priority, String> {
    parse_priority(s)
}

/// column_parse resolves a column spelling to its variant (§8).
pub fn column_parse(s: &str) -> Result<Column, String> {
    match s {
        "waiting" => Ok(Column::Waiting),
        "ready" => Ok(Column::Ready),
        "working" => Ok(Column::Working),
        "review" => Ok(Column::Review),
        "merging" => Ok(Column::Merging),
        "landed" => Ok(Column::Landed),
        "plan" => Ok(Column::Plan),
        "check" => Ok(Column::Check),
        "none" => Ok(Column::None),
        _ => Err(format!("unknown column {:?}", s)),
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

// L3: the retained wire upgrade and the strict unsupported-data read
// (tables/cards/upgrade.go; docs/SPEC-TABLES.md §6.6, §4). Upgrade is a REGION
// round trip through the generated retaining path — CardLoadRetain keeps every
// field the current declaration cannot name, CardSaveRetain writes them back at
// the end of their own body. It is pure and byte-idempotent after the first
// save, and it never mutates the input wire. ReadStrict is the strict half of
// the tolerant read (§4): it refuses any data the current declaration cannot
// support. The checked abstraction is tla/CardVersioning.tla: one atomic
// retained round trip holding NoFieldLost, BodyUnchanged, RefusalKeepsOriginal
// and ShippedUpgradeSucceeds (upgrade.go).

/// The customer ceiling on the retained-record byte store
/// (docs/SPEC-TABLES.md §6.6): it caps extra retention memory for a card with
/// a 64-KiB body and bounded collections; it never grows on demand. Go
/// Upgrade  MaxRetainBytes = 1<<20.
pub const MAX_RETAIN_BYTES: usize = 1 << 20;

/// The customer ceiling on the retained-id entry store, the caller-owned id
/// list §6.6 keeps inside the retain store (Go Upgrade: MaxRetainIDs = 4096).
pub const MAX_RETAIN_IDS: usize = 4096;

/// Upgrade rewrites wire through the generated retaining region path so that
/// every field the current declaration cannot name is preserved, byte for byte,
/// at the end of its own body (§6.6). It returns nil output and a named error
/// when any value would be lost — RetainLost, KindMismatch, Clamped or
/// Malformed — or when the wire is refused; the input wire is never mutated.
/// The result is idempotent: upgrading an already-upgraded wire is byte-for-byte
/// the same (§6.6). Ports tables/cards/upgrade.go Upgrade.
pub fn upgrade(wire: &[u8]) -> Result<(Vec<u8>, TableReport), String> {
    upgrade_seam(wire, MAX_RETAIN_BYTES, MAX_RETAIN_IDS)
}

/// upgrade_seam is the package-private retention seam (upgrade.go upgrade): the
/// public facade fixes the two customer capacities, and the refusal probes drive
/// smaller caller-owned storage through the same path so a tiny capacity can be
/// shown to name RetainLost and leave the input unchanged.
fn upgrade_seam(wire: &[u8], retain_bytes: usize, retain_ids: usize) -> Result<(Vec<u8>, TableReport), String> {
    let mut report = TableReport::default();
    if card_load_measure(wire) < 0 {
        return Err("cards: upgrade: wire is not a card".to_string());
    }
    let mut retain = TableRetain {
        bytes: Vec::with_capacity(retain_bytes),
        ids: Vec::with_capacity(retain_ids),
    };
    let card = match card_load_retain(wire, &mut retain, &mut report) {
        Some(c) => c,
        None => return Err("cards: upgrade: wire is malformed".to_string()),
    };
    check_card(&card)?;
    let out = card_save_retain(&card, &mut retain, &mut report);
    if let Some(e) = loss_err(&report) {
        return Err(e);
    }
    Ok((out, report))
}

/// loss_err names the report events that make a rewrite unsafe (§6.6): any
/// retained field that could not be kept, any kind mismatch, any clamped value,
/// or framing damage. Unknown, Retained and Widened alone are not loss (§4).
/// Ports tables/cards/upgrade.go lossErr.
fn loss_err(report: &TableReport) -> Option<String> {
    if report.retain_lost > 0 {
        return Some(format!("cards: upgrade: {} retained field(s) lost, refusing to replace", report.retain_lost));
    }
    if report.kind_mismatch > 0 {
        return Some(format!("cards: upgrade: {} kind mismatch(es), refusing to replace", report.kind_mismatch));
    }
    if report.clamped > 0 {
        return Some(format!("cards: upgrade: {} clamped value(s), refusing to replace", report.clamped));
    }
    if report.malformed {
        return Some("cards: upgrade: malformed wire, refusing to replace".to_string());
    }
    None
}

/// ReadStrict reads wire and refuses any data the current declaration cannot
/// support: Unknown, KindMismatch, Clamped, Malformed and every load failure
/// (docs/SPEC-TABLES.md §4). A safe Widened event need not refuse. It returns no
/// usable Record on rejection. Ports tables/cards/upgrade.go ReadStrict.
pub fn read_strict(wire: &[u8]) -> Result<Record, String> {
    let (record, report) = load(wire)?;
    if report.unknown > 0 {
        return Err(format!("cards: strict: {} unknown field(s), unsupported data", report.unknown));
    }
    if report.kind_mismatch > 0 {
        return Err(format!("cards: strict: {} kind mismatch(es), unsupported data", report.kind_mismatch));
    }
    if report.clamped > 0 {
        return Err(format!("cards: strict: {} clamped value(s), unsupported data", report.clamped));
    }
    if report.malformed {
        return Err("cards: strict: malformed wire, unsupported data".to_string());
    }
    Ok(record)
}

// L4: the task hash, the atomic field edits, and the view that walks a record
// by name and kind (tables/cards/edit.go; docs/SPEC-TABLES.md §5, §2.9, §8).
// The hash is the concrete side of tla/CardHops.tla's TaskHashStable and the
// edits of EditDropsUnknown; the view walks the generated descriptors
// (§8.1, §8.3).

mod sha256 {
    // A small SHA-256 (FIPS 180-4) in the crate because std has none and no
    // crate is licensed. Checked against the FIPS vectors (empty, "abc", the
    // 448-bit message) by sha256_matches_fips_180_vectors.
    const K: [u32; 64] = [
        0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1,
        0x923f82a4, 0xab1c5ed5, 0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3,
        0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174, 0xe49b69c1, 0xefbe4786,
        0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
        0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147,
        0x06ca6351, 0x14292967, 0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
        0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85, 0xa2bfe8a1, 0xa81a664b,
        0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
        0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a,
        0x5b9cca4f, 0x682e6ff3, 0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
        0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
    ];

    pub struct Sha256 {
        h: [u32; 8],
        buf: [u8; 64],
        buflen: usize,
        total: u64,
    }

    impl Sha256 {
        pub fn new() -> Self {
            Sha256 {
                h: [
                    0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a,
                    0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19,
                ],
                buf: [0u8; 64],
                buflen: 0,
                total: 0,
            }
        }

        pub fn update(&mut self, mut data: &[u8]) {
            while !data.is_empty() {
                if self.buflen == 0 && data.len() >= 64 {
                    self.compress(data[..64].try_into().unwrap());
                    self.total += 64;
                    data = &data[64..];
                } else {
                    let take = (64 - self.buflen).min(data.len());
                    self.buf[self.buflen..self.buflen + take].copy_from_slice(&data[..take]);
                    self.buflen += take;
                    self.total += take as u64;
                    data = &data[take..];
                    if self.buflen == 64 {
                        let block = self.buf;
                        self.compress(&block);
                        self.buflen = 0;
                    }
                }
            }
        }

        fn compress(&mut self, block: &[u8; 64]) {
            let mut w = [0u32; 64];
            for (i, wi) in w.iter_mut().enumerate().take(16) {
                let o = i * 4;
                *wi = u32::from_be_bytes([block[o], block[o + 1], block[o + 2], block[o + 3]]);
            }
            for i in 16..64 {
                let s0 = w[i - 15].rotate_right(7) ^ w[i - 15].rotate_right(18) ^ (w[i - 15] >> 3);
                let s1 = w[i - 2].rotate_right(17) ^ w[i - 2].rotate_right(19) ^ (w[i - 2] >> 10);
                w[i] = w[i - 16].wrapping_add(s0).wrapping_add(w[i - 7]).wrapping_add(s1);
            }
            let mut a = self.h[0];
            let mut b = self.h[1];
            let mut c = self.h[2];
            let mut d = self.h[3];
            let mut e = self.h[4];
            let mut f = self.h[5];
            let mut g = self.h[6];
            let mut h = self.h[7];
            for i in 0..64 {
                let s1 = e.rotate_right(6) ^ e.rotate_right(11) ^ e.rotate_right(25);
                let ch = (e & f) ^ (!e & g);
                let temp1 = h.wrapping_add(s1).wrapping_add(ch).wrapping_add(K[i]).wrapping_add(w[i]);
                let s0 = a.rotate_right(2) ^ a.rotate_right(13) ^ a.rotate_right(22);
                let maj = (a & b) ^ (a & c) ^ (b & c);
                let temp2 = s0.wrapping_add(maj);
                h = g;
                g = f;
                f = e;
                e = d.wrapping_add(temp1);
                d = c;
                c = b;
                b = a;
                a = temp1.wrapping_add(temp2);
            }
            self.h[0] = self.h[0].wrapping_add(a);
            self.h[1] = self.h[1].wrapping_add(b);
            self.h[2] = self.h[2].wrapping_add(c);
            self.h[3] = self.h[3].wrapping_add(d);
            self.h[4] = self.h[4].wrapping_add(e);
            self.h[5] = self.h[5].wrapping_add(f);
            self.h[6] = self.h[6].wrapping_add(g);
            self.h[7] = self.h[7].wrapping_add(h);
        }

        pub fn finalize(mut self) -> [u8; 32] {
            let bit_len = self.total.wrapping_mul(8);
            self.update(&[0x80]);
            while self.buflen % 64 != 56 {
                self.update(&[0]);
            }
            self.update(&bit_len.to_be_bytes());
            let mut out = [0u8; 32];
            for i in 0..8 {
                out[i * 4..i * 4 + 4].copy_from_slice(&self.h[i].to_be_bytes());
            }
            out
        }
    }
}

/// TaskHash returns a digest of a record's TASK, the fields a human's memory
/// of a card is made of: body, paths, new, shared, test and done. Everything
/// else — identity/coordination, needs/tier/priority, machine fields — is
/// deliberately excluded, so a change to any of those leaves the digest
/// untouched. The digest is framed: each text is an unsigned 64-bit
/// little-endian byte length followed by its exact bytes, and each list is an
/// entry count followed by its length-prefixed elements (docs/SPEC-TABLES.md
/// §5). A lossy record (unknown or mismatched fields, clamped values, retain
/// loss) is refused, never hashed (tables/cards/edit.go TaskHash, CardHops
/// TaskHashStable).
pub fn task_hash(record: &Record) -> Result<[u8; 32], String> {
    refuse_lossy(&record.report)?;
    let mut h = sha256::Sha256::new();
    hash_text(&mut h, body_bytes(record))?;
    hash_path_list(&mut h, &record.card.paths)?;
    hash_path_list(&mut h, &record.card._new)?;
    hash_path_list(&mut h, &record.card.shared)?;
    hash_text(&mut h, record.card.test.as_bytes())?;
    hash_text(&mut h, done_bytes(record))?;
    Ok(h.finalize())
}

/// refuse_lossy names the read events that make a record unsafe to hash or
/// rewrite: a silent loss means the "task" the hash would cover is incomplete
/// (docs/SPEC-TABLES.md §4, §6.6; edit.go refuseLossy).
fn refuse_lossy(report: &TableReport) -> Result<(), String> {
    if report.unknown > 0 || report.kind_mismatch > 0 || report.clamped > 0 || report.retain_lost > 0 || report.malformed {
        return Err(format!(
            "cards: record has loss events (unknown={} kindMismatch={} clamped={} retainLost={} malformed={}): refused, never hashed or rewritten",
            report.unknown, report.kind_mismatch, report.clamped, report.retain_lost, report.malformed
        ));
    }
    Ok(())
}

fn hash_u64(h: &mut sha256::Sha256, v: u64) {
    h.update(&v.to_le_bytes());
}

fn hash_text(h: &mut sha256::Sha256, text: &[u8]) -> Result<(), String> {
    hash_u64(h, text.len() as u64);
    h.update(text);
    Ok(())
}

fn hash_path_list(h: &mut sha256::Sha256, list: &[PathText]) -> Result<(), String> {
    hash_u64(h, list.len() as u64);
    for e in list {
        hash_text(h, e.s.as_bytes())?;
    }
    Ok(())
}

fn body_bytes(record: &Record) -> &[u8] {
    record.card.body.as_ref().map(|s| s.as_bytes()).unwrap_or(&[])
}

fn done_bytes(record: &Record) -> &[u8] {
    record.card.done.as_ref().map(|s| s.as_bytes()).unwrap_or(&[])
}

/// edit is the shared atomic rewrite (edit.go edit, CardHops EditDropsUnknown):
/// the current record must be safe, the replacement is built fresh from the
/// loaded known fields, validated whole, and only on complete success is the
/// record's owned wire swapped. Any refusal leaves the original wire untouched.
fn edit(record: &mut Record, mutate: impl FnOnce(&mut Card) -> Result<(), String>) -> Result<(), String> {
    refuse_lossy(&record.report)?;
    let mut card = record.card.clone();
    mutate(&mut card)?;
    check_card(&card)?;
    let wire = card_save(&card);
    record.wire = wire;
    record.card = card;
    record.report = TableReport::default();
    Ok(())
}

fn validate_list(name: &str, values: &[String], bound: usize) -> Result<(), String> {
    if values.len() > MAX_LIST_ENTRIES {
        return Err(format!("cards: {} has {} entries, past the {}-entry ceiling", name, values.len(), MAX_LIST_ENTRIES));
    }
    for s in values {
        if s.len() > bound {
            return Err(format!("cards: {} element is {} bytes, past its {}-bytete bound", name, s.len(), bound));
        }
        if s.contains('\0') {
            return Err(format!("cards: {} has an element that contains a NUL byte", name));
        }
    }
    Ok(())
}

/// SetNeeds replaces the record's needs list atomically (edit.go SetNeeds): the
/// incoming values are validated first (each element within its 64-byte bound,
/// no NUL, the list within the shared 512-entry ceiling); only a fully
/// validated, independently built and successfully serialized replacement is
/// published. On any refusal the original wire is preserved byte for byte.
pub fn set_needs(record: &mut Record, needs: &[String]) -> Result<(), String> {
    validate_list("needs", needs, 64)?;
    edit(record, |card| {
        card.needs = needs.iter().map(|s| CardID { s: s.clone() }).collect();
        Ok(())
    })
}

/// SetPaths replaces the record's paths list atomically (edit.go SetPaths).
pub fn set_paths(record: &mut Record, paths: &[String]) -> Result<(), String> {
    validate_list("paths", paths, 192)?;
    edit(record, |card| {
        card.paths = paths.iter().map(|s| PathText { s: s.clone() }).collect();
        Ok(())
    })
}

fn tier_is_declared(t: Tier) -> bool {
    matches!(t, Tier::None | Tier::Flash | Tier::Pro | Tier::Heavy | Tier::Frontier)
}

fn priority_is_declared(p: Priority) -> bool {
    matches!(p, Priority::None | Priority::Normal | Priority::High | Priority::Critical | Priority::Blocker)
}

fn column_is_declared(c: Column) -> bool {
    matches!(c, Column::None | Column::Waiting | Column::Ready | Column::Working | Column::Review | Column::Merging | Column::Landed | Column::Plan | Column::Check)
}

/// SetTier changes a record's tier atomically; only a declared variant is
/// accepted (docs/SPEC-TABLES.md §8; edit.go SetTier).
pub fn set_tier(record: &mut Record, tier: Tier) -> Result<(), String> {
    if !tier_is_declared(tier) {
        return Err("cards: tier is not a declared variant".to_string());
    }
    edit(record, |card| {
        card.tier = tier;
        Ok(())
    })
}

/// SetPriority changes a record's priority atomically (edit.go SetPriority).
pub fn set_priority(record: &mut Record, priority: Priority) -> Result<(), String> {
    if !priority_is_declared(priority) {
        return Err("cards: priority is not a declared variant".to_string());
    }
    edit(record, |card| {
        card.priority = priority;
        Ok(())
    })
}

/// SetColumn changes a record's column atomically (edit.go SetColumn).
pub fn set_column(record: &mut Record, column: Column) -> Result<(), String> {
    if !column_is_declared(column) {
        return Err("cards: column is not a declared variant".to_string());
    }
    edit(record, |card| {
        card.column = column;
        Ok(())
    })
}

/// SetHolder changes a record's holder atomically; the value must be well
/// formed and within its declared 32-byte bound (edit.go SetHolder).
pub fn set_holder(record: &mut Record, holder: &str) -> Result<(), String> {
    validate_list("holder", &[holder.to_string()], 32)?;
    edit(record, |card| {
        card.holder = holder.to_string();
        Ok(())
    })
}

// The view: one line per field of every table in the unit, tables in the
// generated view's order (sorted by table name) and each table's fields in
// declaration order — "<table> <field> <kind> <shape> <bound>"
// (docs/SPEC-TABLES.md §8; conformance/FORMAT.md "The view"). The walk is over
// the generated descriptors (card_descriptors TABLES); the schema element type
// names come from a small fixed vocabulary, the same set the Go reference leg
// emits.
fn field_type_name(kind: u8, field: &str) -> &'static str {
    match kind {
        12 | 17 => "string",
        4 => "int32",
        5 => "int64",
        14 => match field {
            "paths" | "_new" | "shared" => "PathText",
            "needs" => "CardID",
            "extra" => "KeyValue",
            "consumers" => "Consumer",
            _ => "?",
        },
        30 => match field {
            "tier" => "Tier",
            "priority" => "Priority",
            "column" => "Column",
            _ => "?",
        },
        _ => "?",
    }
}

/// view renders the generated view from the descriptors (docs/SPEC-TABLES.md
/// §8.1, §8.3): every field of every table in the unit, in the registry's
/// order, one line each.
pub fn view() -> String {
    let mut s = String::new();
    for t in card_descriptors::TABLES.iter() {
        for f in t.fields.iter() {
            let shape = match f.shape {
                card_descriptors::SHAPE_LIST => "list",
                card_descriptors::SHAPE_BLOB => "blob",
                _ => "value",
            };
            let bound = if f.shape == card_descriptors::SHAPE_VALUE && f.bound > 0 {
                f.bound.to_string()
            } else {
                "-".to_string()
            };
            s.push_str(&format!("{} {} {} {} {}\n", t.name, f.name, field_type_name(f.kind, f.name), shape, bound));
        }
    }
    s
}

/// refusal_class maps a facade error to one class of the closed list
/// conformance/FORMAT.md fixes, never free text, so every language can spell
/// the same refusal (the Go driver's refusalClass).
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
        let s = err
            .find('"')
            .map(|i| &err[i + 1..])
            .and_then(|rest| rest.find('"').map(|j| &rest[..j]))
            .unwrap_or("");
        return format!("stranded-key {s}");
    }
    if err.contains("retained field(s) lost") {
        return "retain-lost".to_string();
    }
    if err.contains("record has loss events") {
        return "unknown-present".to_string();
    }
    "malformed".to_string()
}

fn split_list_value(v: &str) -> Vec<String> {
    if v.is_empty() {
        return Vec::new();
    }
    v.split(',').map(|s| s.to_string()).collect()
}

/// apply_edit applies one edit line — "needs=a,b", "tier=heavy",
/// "priority=blocker", "paths=x,y", "column=review", "holder=h" — through the
/// Set* edit of the same name (the Go driver's applyEdit).
pub fn apply_edit(record: &mut Record, edit: &str) -> Result<(), String> {
    let Some(eq) = edit.find('=') else {
        return Err(format!("cards-driver: edit {edit:?} has no ="));
    };
    let key = &edit[..eq];
    let value = &edit[eq + 1..];
    match key {
        "needs" => set_needs(record, &split_list_value(value)),
        "paths" => set_paths(record, &split_list_value(value)),
        "tier" => set_tier(record, tier_parse(value)?),
        "priority" => set_priority(record, priority_parse(value)?),
        "column" => set_column(record, column_parse(value)?),
        "holder" => set_holder(record, value),
        _ => Err(format!("cards-driver: unknown edit {key:?}")),
    }
}

#[cfg(test)]
#[path = "tests.rs"]
mod tests;
