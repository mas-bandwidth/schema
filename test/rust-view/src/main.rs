// THE EDITOR GATE, in Rust (docs/SPEC-TABLES.md §8.7). This program has the
// generated unit and NOTHING ELSE: no schema files on disk, no compiler, no
// knowledge of a single declaration's name. It calls unit_view() and prints
// the whole build — every constant with its value, every enum and flags and
// union with its variants in order, every type and every table with every
// property — in the listing format test/c-tables/view_main.c prints, and the
// listing is byte-compared against the one the compiler produces from its own
// IR (internal/viewlisting). If it cannot reach a declaration or a property of
// one, the view is incomplete and the gate says so.
//
// The unit is named by the build, not by this source: the crate `viewunit` is
// whichever unit `make tables-rust-view` generated, so one program serves
// every unit in the corpus and knows nothing about any of them. It names the
// unit's items by path and never globs them in, because a schema is free to
// declare any name this file might use.
//
// A MULTI-LINE doc prints as ONE LINE, each newline written \n and the
// escape's own backslash written \\, by the same rule both halves flatten by.

use std::fmt::Write;

use viewunit::{TableFieldInfo, TableUnitView, TableViewType, TableViewVocabulary};

// flatten writes one doc column as a single line. The column is never absent
// (§8.1), so it is written with no test.
fn flatten(out: &mut String, doc: &str) {
    for c in doc.chars() {
        match c {
            '\\' => out.push_str("\\\\"),
            '\n' => out.push_str("\\n"),
            _ => out.push(c),
        }
    }
}

// annotation prints a row's tags and its doc, the doc LAST because it is
// free-form text and everything after it on the line would be ambiguous. The
// tag count is checked before the list is walked, whatever the list holds.
fn annotation(out: &mut String, num_tags: i32, tags: &[&str], doc: &str) {
    out.push_str(" tags=[");
    for (i, tag) in tags.iter().take(num_tags as usize).enumerate() {
        if i > 0 {
            out.push(',');
        }
        out.push_str(tag);
    }
    out.push_str("] doc=");
    flatten(out, doc);
    out.push('\n');
}

fn vocabulary(out: &mut String, what: &str, v: &TableViewVocabulary, row: &str) {
    let _ = write!(
        out,
        "{} {} file={} max={} bits={} variants={}",
        what, v.name, v.file, v.max, v.storage_bits, v.num_variants
    );
    annotation(out, v.num_tags, v.tags, v.doc);
    for r in v.variants.iter().take(v.num_variants as usize) {
        let _ = write!(
            out,
            "{} {} {} {} {} id={:016x}",
            what, v.name, row, r.value, r.name, r.id
        );
        annotation(out, r.num_tags, r.tags, r.doc);
    }
}

// THE HOLDER (§8.7): the first registry entry declaring a field of this
// union's type, the tables in registry order and then the types. A union's
// arms table hangs off a field descriptor and not off the vocabulary row, so a
// generic walker reaches it through such a field.
fn holder(unit: &TableUnitView, union_name: &str) -> Option<&'static TableFieldInfo> {
    for set in [unit.tables, unit.types] {
        for entry in set {
            for f in entry
                .type_info
                .fields
                .iter()
                .take(entry.type_info.num_fields as usize)
            {
                if f.arms.is_some_and(|arms| arms.name == union_name) {
                    return Some(f);
                }
            }
        }
    }
    None
}

fn union_vocabulary(out: &mut String, unit: &TableUnitView, v: &TableViewVocabulary) {
    let _ = write!(
        out,
        "union {} file={} max={} bits={} variants={}",
        v.name, v.file, v.max, v.storage_bits, v.num_variants
    );
    annotation(out, v.num_tags, v.tags, v.doc);
    let arms = holder(unit, v.name).and_then(|f| f.arms);
    for r in v.variants.iter().take(v.num_variants as usize) {
        let _ = write!(
            out,
            "union {} arm {} {} id={:016x} payload={} record={} field={}",
            v.name,
            r.value,
            r.name,
            r.id,
            r.payload_name.unwrap_or("-"),
            if r.payload.is_some() { "yes" } else { "no" },
            if r.field.is_some() { "yes" } else { "no" }
        );
        match r.field {
            Some(field) => {
                let _ = write!(out, " kind={} bound={}", field.kind, field.array_bound);
                match arms {
                    // EVERY ARM OVERLAYS THE UNION'S PAYLOAD BASE: the arm
                    // row's own offset against the base the holder's arms
                    // table carries for its first general arm
                    Some(arms) => {
                        let base = arms
                            .arms
                            .iter()
                            .find(|a| a.field.is_some())
                            .map(|a| a.offset as i64)
                            .unwrap_or(0);
                        let _ = write!(out, " overlay={}", field.offset as i64 - base);
                    }
                    None => out.push_str(" overlay=-"),
                }
            }
            None => out.push_str(" kind=- bound=- overlay=-"),
        }
        annotation(out, r.num_tags, r.tags, r.doc);
    }
}

fn declaration(out: &mut String, what: &str, entry: &TableViewType) {
    let t = entry.type_info;
    let _ = write!(
        out,
        "{} {} file={} fields={}",
        what, entry.name, entry.file, t.num_fields
    );
    annotation(out, entry.num_tags, entry.tags, entry.doc);
    for f in t.fields.iter().take(t.num_fields as usize) {
        let _ = write!(
            out,
            "{} {} field {} json={} type={} id={:016x} optional={}",
            what,
            entry.name,
            f.name,
            f.json.unwrap_or("-"),
            f.type_name,
            f.id,
            f.optional
        );
        annotation(out, f.num_tags, f.tags, f.doc);
    }
}

fn main() {
    let unit = viewunit::unit_view();
    let mut out = String::new();
    let _ = writeln!(
        out,
        "unit package={} protocol={:016x}",
        unit.package, unit.protocol_id
    );
    for c in unit.constants.iter().take(unit.num_constants as usize) {
        let real = if c.is_float {
            c.float_value.to_bits()
        } else {
            0
        };
        let _ = write!(
            out,
            "constant {} file={} type={} float={} int={} real={:016x}",
            c.name, c.file, c.type_name, c.is_float, c.int_value, real
        );
        annotation(&mut out, c.num_tags, c.tags, c.doc);
    }
    for e in unit.enums.iter().take(unit.num_enums as usize) {
        vocabulary(&mut out, "enum", e, "variant");
    }
    for f in unit.flags.iter().take(unit.num_flags as usize) {
        vocabulary(&mut out, "flags", f, "bit");
    }
    for u in unit.unions.iter().take(unit.num_unions as usize) {
        union_vocabulary(&mut out, unit, u);
    }
    for t in unit.types.iter().take(unit.num_types as usize) {
        declaration(&mut out, "type", t);
    }
    for t in unit.tables.iter().take(unit.num_tables as usize) {
        declaration(&mut out, "table", t);
    }
    print!("{}", out);
}
