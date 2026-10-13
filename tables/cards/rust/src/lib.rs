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

#[cfg(test)]
#[path = "tests.rs"]
mod tests;
