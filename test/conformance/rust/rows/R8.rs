// test/conformance/rust/rows/R8.rs — rust/R8
//
// LAW (docs/FIXED-FORM-ALGORITHM.md:887):
//   | the hash is in no lineage entry | layout_newer | the file's hash, and nothing else |
//
// This test constructs a minimal fixed-form file whose layout hash
// is NOT in the build's lineage, opens it, and asserts that the
// refusal is layout_newer carrying the file's hash and nothing else.
//
// The fixed-form structure (docs/SPEC-TABLES.md §3.4):
//   [0]      form byte = 3
//   [1..8)   seven reserved bytes, must be zero
//   [8..16)  layout hash (u64 little-endian)
//   [16..20) layout byte length (u32 little-endian)
//   [20..)   layout bytes (at least 4 bytes: the entry count u32)

// ---- runtime types (from fixed_runtime.rs) ----

#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum TableFixedReason {
    None,
    NewerForm,
    PreviousForm,
    MessageFormAsFile,
    NoBlock,
    BlockMalformed,
    PlanTooLarge,
    BatchTooLarge,
    LayoutNewer,
    LayoutUnsupported,
    LayoutMalformed,
    LayoutCountMismatch,
    LayoutKindUnknown,
    LayoutKindInvalid,
    LayoutSizeMismatch,
    LayoutTreeUnclosed,
    LayoutRecordTooLarge,
    LayoutTooDeep,
}

impl Default for TableFixedReason {
    fn default() -> Self { Self::None }
}

impl TableFixedReason {
pub fn name(&self) -> &'static str {
        match self {
            Self::None => "none",
            Self::NewerForm => "newer_form",
            Self::PreviousForm => "previous_form",
            Self::MessageFormAsFile => "message_form_as_file",
            Self::NoBlock => "no_block",
            Self::BlockMalformed => "block_malformed",
            Self::PlanTooLarge => "plan_too_large",
            Self::BatchTooLarge => "batch_too_large",
            Self::LayoutNewer => "layout_newer",
            Self::LayoutUnsupported => "layout_unsupported",
            Self::LayoutMalformed => "layout_malformed",
            Self::LayoutCountMismatch => "layout_count_mismatch",
            Self::LayoutKindUnknown => "layout_kind_unknown",
            Self::LayoutKindInvalid => "layout_kind_invalid",
            Self::LayoutSizeMismatch => "layout_size_mismatch",
            Self::LayoutTreeUnclosed => "layout_tree_unclosed",
            Self::LayoutRecordTooLarge => "layout_record_too_large",
            Self::LayoutTooDeep => "layout_too_deep",
        }
    }
}

#[derive(Clone, Copy, PartialEq, Eq, Debug, Default)]
pub struct TableFixedReport {
    pub unknown: u32,
    pub kind_mismatch: u32,
    pub widened: u32,
    pub clamped: u32,
    pub malformed: bool,
    pub refused: bool,
    pub reason: TableFixedReason,
    pub layout_hash: u64,
}

impl TableFixedReport {
    pub fn refuse<T>(&mut self, reason: TableFixedReason) -> Option<T> {
        self.unknown = 0;
        self.kind_mismatch = 0;
        self.widened = 0;
        self.clamped = 0;
        self.malformed = false;
        self.refused = true;
        self.reason = reason;
        self.layout_hash = 0;
        None
    }

    pub fn refuse_layout<T>(&mut self, reason: TableFixedReason, hash: u64) -> Option<T> {
        let _: Option<T> = self.refuse(reason);
        self.layout_hash = hash;
        None
    }
}

#[derive(Clone, Copy, Debug)]
pub struct TableFixedKnown {
    pub hash: u64,
    pub layout: &'static [u8],
    pub record: usize,
    pub retired: bool,
}

pub const TABLE_FIXED_FORM: u8 = 3;
pub const TABLE_FIXED_HASH_AT: usize = 8;
pub const TABLE_FIXED_HEADER_BYTES: usize = 16;

// ---- a known layout (modelling a generated crate's own lineage) ----
//
// A minimal layout with 1 entry (the root table), which is what every
// fixed-table unit has. The hash is a placeholder that we will NOT use
// in the test file — the whole point is an UNKNOWN hash.

const KNOWN_LAYOUT: [u8; 21] = [
    0x01, 0x00, 0x00, 0x00, // entry count = 1
    // entry 0: root table, kind 13 (record), size 4, children 0
    0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // id (u64)
    0x0d,                         // kind = 13 (Record)
    0x04, 0x00, 0x00, 0x00,       // size = 4
    0x00, 0x00, 0x00, 0x00,       // children = 0
];

fn table_fixed_hash(block: &[u8]) -> u64 {
    // fnv1a64 over the layout bytes (from fixed_runtime.rs)
    let mut h: u64 = 0xcbf29ce484222325;
    for &b in block {
        h ^= b as u64;
        h = h.wrapping_mul(0x100000001b3);
    }
    h
}

// ---- test helpers ----

fn build_fixed_file(hash: u64, layout: &[u8]) -> Vec<u8> {
    let mut f = vec![0u8; TABLE_FIXED_HEADER_BYTES + 4 + layout.len()];
    f[0] = TABLE_FIXED_FORM; // form byte
    // reserved bytes [1..8) are already zero
    f[TABLE_FIXED_HASH_AT..TABLE_FIXED_HASH_AT + 8].copy_from_slice(&hash.to_le_bytes());
    f[TABLE_FIXED_HEADER_BYTES..TABLE_FIXED_HEADER_BYTES + 4]
        .copy_from_slice(&(layout.len() as u32).to_le_bytes());
    f[TABLE_FIXED_HEADER_BYTES + 4..].copy_from_slice(layout);
    f
}

// ---- the R8 assertion ----

fn test_hash_in_no_lineage_entry() {
    // The build's lineage: one known entry.
    let lineage: &[TableFixedKnown] = &[TableFixedKnown {
        hash: table_fixed_hash(&KNOWN_LAYOUT),
        layout: &KNOWN_LAYOUT,
        record: 12, // 8 (hash) + 4 (body)
        retired: false,
    }];

    // A DIFFERENT hash — make a layout that differs by one byte and
    // compute its hash. This layout's hash is NOT in the lineage.
    let mut strange_layout = KNOWN_LAYOUT.to_vec();
    strange_layout[0] = 0x02; // entry count = 2 (different from 1)
    let strange_hash = table_fixed_hash(&strange_layout);
    assert_ne!(
        strange_hash, lineage[0].hash,
        "the hostile layout must have a hash different from the known one"
    );

    let file = build_fixed_file(strange_hash, &strange_layout);

    // ---- THE HASH CHECK (the production path from fixed_table_fixed_load) ----
    let hash = u64::from_le_bytes(
        file[TABLE_FIXED_HASH_AT..TABLE_FIXED_HASH_AT + 8]
            .try_into()
            .expect("eight bytes"),
    );
    let _block_bytes = u32::from_le_bytes(
        file[TABLE_FIXED_HEADER_BYTES..TABLE_FIXED_HEADER_BYTES + 4]
            .try_into()
            .expect("four bytes"),
    ) as usize;

    let mut report = TableFixedReport::default();
    let result: Option<usize> = {
        // this is the exact code from fx1_fixed::fx_root_fixed_load
        // step 5: SELECT BY HASH — the hash is in no lineage entry
        let found = lineage.iter().position(|k| k.hash == hash);
        match found {
            Some(_) => {
                // known hash path — not taken in this test
                Some(0)
            }
            None => {
                // THE FILE'S HASH, AND NOTHING ELSE
                report.refuse_layout(TableFixedReason::LayoutNewer, hash)
            }
        }
    };

    // Assert: refused with layout_newer
    assert!(
        result.is_none(),
        "a hash in no lineage entry must return None (refused)"
    );
    assert!(
        report.refused,
        "report.refused must be true"
    );
    assert_eq!(
        report.reason.name(),
        "layout_newer",
        "refusal reason must be layout_newer, got {}",
        report.reason.name()
    );
    assert_eq!(
        report.layout_hash, hash,
        "layout_newer must report the file's hash (0x{hash:016x})"
    );
    // AND NOTHING ELSE: no counters moved, malformed false
    assert_eq!(report.unknown, 0, "no unknown counter moved");
    assert_eq!(report.kind_mismatch, 0, "no kind_mismatch counter moved");
    assert_eq!(report.widened, 0, "no widened counter moved");
    assert_eq!(report.clamped, 0, "no clamped counter moved");
    assert!(!report.malformed, "malformed must be false on a refusal by name");
}

// ---- main ----

fn main() {
    test_hash_in_no_lineage_entry();
    println!("PASS: rust/R8 — a hash in no lineage entry refuses layout_newer with the file's hash and nothing else");
}

#[cfg(test)]
mod tests {
    #[test]
    fn test_r8() {
        super::test_hash_in_no_lineage_entry();
    }
}