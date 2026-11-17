package rusttable

// A declaration-free census is separate from value decoding: enum/arm/key
// references, skipped fields and node type IDs all use the file's one table.
const wireIDUseRust = `
// A complete traversal can prove an unused transport ID (§3). An incomplete
// traversal defers to the existing framing/value walk, which owns its named
// refusal (including count caps and non-finite diagnostics).
//
// THE SAME TRAVERSAL HOLDS THE WRITER'S GRAPH SPELLING (§3, §3.1;
// docs/rulings.json reads-refuse-non-canonical): it meets references in the
// wire's byte order, which is the order a writer first names them, root body
// first and the node table's records in index order, so an entry first named
// out of turn is an id table out of first-use order; and the node table is the
// root body's last field and holds at least one record. Each is a spelling no
// conforming writer produces, so Some(false) refuses it like an unused entry.
pub fn table_ids_all_used(body: &[u8], ids: &[u64]) -> Option<bool> {
    if ids.len() > 4096 { return None; }
    let mut walk = TableIdUse { ids, seen: [false; 4096], next: 0, canonical: true };
    walk.body(body, true, 0)?;
    Some(walk.canonical && walk.seen[..ids.len()].iter().all(|used| *used))
}

struct TableIdUse<'a> { ids: &'a [u64], seen: [bool; 4096], next: usize, canonical: bool }

impl TableIdUse<'_> {
    fn reference(&mut self, reference: u64) -> Option<u64> {
        let index = usize::try_from(reference.checked_sub(1)?).ok()?;
        let id = *self.ids.get(index)?;
        if !self.seen[index] {
            // first named out of turn: the id table is not in first-use order
            if index != self.next { self.canonical = false; }
            self.next += 1;
        }
        self.seen[index] = true;
        Some(id)
    }

    fn frame<'b>(bytes: &'b [u8], at: &mut usize) -> Option<&'b [u8]> {
        let length = usize::try_from(leb_read(bytes, at)?).ok()?;
        let end = at.checked_add(length)?;
        let payload = bytes.get(*at..end)?;
        *at = end;
        Some(payload)
    }

    fn body(&mut self, bytes: &[u8], root: bool, depth: usize) -> Option<()> {
        let mut at = 0;
        let mut after_nodes = false;
        loop {
            let reference = leb_read(bytes, &mut at)?;
            if reference == 0 { return (at == bytes.len()).then_some(()); }
            // a field after the node table: the table is out of its place (§3.1)
            if after_nodes { self.canonical = false; }
            let id = self.reference(reference)?;
            let kind = *bytes.get(at)?; at += 1;
            if id == NODE_TABLE_ID {
                if !root || kind != 12 { return None; }
                after_nodes = true;
                self.nodes(Self::frame(bytes, &mut at)?, depth)?;
            } else {
                self.payload(bytes, &mut at, kind, depth)?;
            }
        }
    }

    fn nodes(&mut self, bytes: &[u8], depth: usize) -> Option<()> {
        let mut at = 0;
        let count = leb_read(bytes, &mut at)?;
        if count > bytes.len() as u64 { return None; }
        // a node table with no record is one no writer emits (§3.1)
        if count == 0 { self.canonical = false; }
        for _ in 0..count {
            let type_id = self.reference(leb_read(bytes, &mut at)?)?;
            let body = Self::frame(bytes, &mut at)?;
            if type_id != BYTES_TYPE_ID && type_id != STRING_TYPE_ID {
                self.body(body, false, depth)?;
            }
        }
        (at == bytes.len()).then_some(())
    }

    fn payload(&mut self, bytes: &[u8], at: &mut usize, kind: u8, depth: usize) -> Option<()> {
        match kind {
            30 => {
                let reference = leb_read(bytes, at)?;
                if reference != 0 { self.reference(reference)?; }
            }
            15 => {
                let reference = leb_read(bytes, at)?;
                if reference != 0 {
                    self.reference(reference)?;
                    let arm_kind = *bytes.get(*at)?; *at += 1;
                    let structural = matches!(arm_kind, 13 | 14 | 15 | 16);
                    if structural && depth >= BINARY_MAX_DEPTH { return None; }
                    self.content(Self::frame(bytes, at)?, arm_kind, depth + usize::from(structural))?;
                }
            }
            13 | 14 | 16 => self.content(Self::frame(bytes, at)?, kind, depth)?,
            _ => { if !leb_skip(bytes, at, bytes.len(), kind) { return None; } }
        }
        Some(())
    }

    fn content(&mut self, bytes: &[u8], kind: u8, depth: usize) -> Option<()> {
        if matches!(kind, 13 | 14 | 16) {
            if depth >= BINARY_MAX_DEPTH { return None; }
            if kind == 13 { return self.body(bytes, false, depth + 1); }
            let element = *bytes.first()?;
            let mut at = 1;
            let count = leb_read(bytes, &mut at)?;
            if count > bytes.len() as u64 { return None; }
            for _ in 0..count {
                if kind == 16 {
                    self.reference(leb_read(bytes, &mut at)?)?;
                    self.content(Self::frame(bytes, &mut at)?, element, depth + 1)?;
                } else {
                    self.payload(bytes, &mut at, element, depth + 1)?;
                }
            }
            return (at == bytes.len()).then_some(());
        }
        if matches!(kind, 12 | 31 | 32) { return Some(()); }
        let mut at = 0;
        self.payload(bytes, &mut at, kind, depth)?;
        (at == bytes.len()).then_some(())
    }
}
`
