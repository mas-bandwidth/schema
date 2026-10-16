// THE WIRE-FUZZ DRIVER (test/conformance/harness/wirefuzz.go): the Rust leg of
// the independent table-wire differential, over the Card root only. The harness
// owns the mutations and the oracle; this process binds the roster entry to the
// generated cards codec and returns the report, the region measure and the saved
// value, on a pipe (docs/SPEC-TABLES.md §4.2).
//
// It is a STANDALONE binary, built with rustc beside the card's three generated
// wire modules (variable_runtime.rs, card_table.rs, card_variable.rs), exactly
// the way this port's own tests run generated code — the card's fixed, block and
// cook forms are not part of this port (docs/SPEC-TABLES.md §3).
//
// The protocol is the harness's: a roster of (unit, root, form, retain), one
// availability byte per entry, then a stream of (index, size, wire) and, per
// mutant, the reply (loaded, the six counters, the verdict, the region measure,
// the two retention counters and the saved bytes).
//
// THE RETENTION ARM (docs/SPEC-TABLES.md §6.6): a roster entry whose retain
// byte is set is driven through the retaining pair — card_load_retain and
// card_save_retain over the caller-owned stores the arm declares — and the two
// retention counters ride the reply beside the six.

mod variable_runtime;
mod card_table;
mod card_variable;
use variable_runtime::*;
use card_table::*;

use std::io::{self, Read, Write};

fn read_u32() -> io::Result<u32> {
    let mut b = [0u8; 4];
    io::stdin().read_exact(&mut b)?;
    Ok(u32::from_le_bytes(b))
}

fn read_name() -> io::Result<Vec<u8>> {
    let mut n = [0u8; 2];
    io::stdin().read_exact(&mut n)?;
    let mut b = vec![0u8; u16::from_le_bytes(n) as usize];
    io::stdin().read_exact(&mut b)?;
    Ok(b)
}

fn main() {
    let stdout = io::stdout();
    let mut out = stdout.lock();
    let count = match read_u32() {
        Ok(c) => c as usize,
        Err(_) => return,
    };
    let mut roster: Vec<(bool, bool)> = Vec::with_capacity(count);
    for _ in 0..count {
        let unit = read_name().unwrap_or_default();
        let root = read_name().unwrap_or_default();
        let mut form = [0u8; 1];
        let mut retain = [0u8; 1];
        if io::stdin().read_exact(&mut form).is_err()
            || io::stdin().read_exact(&mut retain).is_err()
        {
            return;
        }
        // the Card root, form 1, with or without retention: the only roster
        // entry this leg has a codec for (docs/SPEC-TABLES.md §3, §6.6)
        let available = unit == b"cards" && root == b"Card" && form[0] == 1;
        roster.push((available, retain[0] == 1));
        let _ = out.write_all(&[available as u8]);
    }
    let _ = out.flush();
    loop {
        let index = match read_u32() {
            Ok(i) => i as usize,
            Err(_) => return, // EOF ends the stream
        };
        let size = match read_u32() {
            Ok(s) => s as usize,
            Err(_) => return,
        };
        if index >= roster.len() || !roster[index].0 {
            return;
        }
        let retain_mode = roster[index].1;
        let mut wire = vec![0u8; size];
        if io::stdin().read_exact(&mut wire).is_err() {
            return;
        }
        let mut report = TableReport::default();
        let measure = card_load_measure(&wire);
        let loaded;
        let saved;
        let retained;
        let retain_lost;
        if retain_mode {
            // THE RETAINING PAIR (docs/SPEC-TABLES.md §6.6): the two stores the
            // arm declares, large on purpose, so the arm compares the feature and
            // not the record layout.
            let mut store = TableRetain {
                bytes: Vec::with_capacity(1 << 20),
                ids: Vec::with_capacity(1 << 17),
                nodes: Default::default(),
            };
            let value = card_load_retain(&wire, &mut store, &mut report);
            loaded = value.is_some();
            saved = card_save_retain(&value.unwrap_or_default(), &mut store, &mut report);
            retained = report.retained;
            retain_lost = report.retain_lost;
        } else {
            let value = card_load(&wire, &mut report);
            loaded = value.is_some();
            saved = card_save(&value.unwrap_or_default());
            retained = 0;
            retain_lost = 0;
        }
        // loaded, unknown, kind_mismatch, widened, clamped, duplicate, malformed,
        // refused, measure (i64), retained, retain_lost, saved length
        let mut head = [0u8; 47];
        head[0] = loaded as u8;
        head[1..5].copy_from_slice(&(report.unknown as u32).to_le_bytes());
        head[5..9].copy_from_slice(&(report.kind_mismatch as u32).to_le_bytes());
        head[9..13].copy_from_slice(&(report.widened as u32).to_le_bytes());
        head[13..17].copy_from_slice(&(report.clamped as u32).to_le_bytes());
        head[17..21].copy_from_slice(&(report.duplicate as u32).to_le_bytes());
        head[21] = report.malformed as u8;
        head[22] = report.refused as u8;
        head[23..31].copy_from_slice(&(measure as i64).to_le_bytes());
        head[31..35].copy_from_slice(&(retained as u32).to_le_bytes());
        head[35..39].copy_from_slice(&(retain_lost as u32).to_le_bytes());
        head[39..47].copy_from_slice(&(saved.len() as i64).to_le_bytes());
        let _ = out.write_all(&head);
        let _ = out.write_all(&saved);
        let _ = out.flush();
    }
}
