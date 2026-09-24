// UnsignedIntegersValidData.rs — cell rust/UnsignedIntegersValidData
//
// "Unsigned integers: 8, 16, 32 and 64 bits: valid-data write/read acceptance"
// (docs/FIXED-FORM-ALGORITHM.md:13, §3.4 record, uint8..uint64).
//
// THE LAW (§3.4): unsigned integers of widths 8, 16, 32 and 64 bits are stored
// at their declared width, little-endian, and round-trip value-for-value through
// fixed-form write and read.
//
// ProfileConfig (tables/examples/Tables.schema:91) carries all four:
//   badge       uint8   — body offset 71
//   port        uint16  — body offset 72
//   experience  uint32  — body offset 56
//   epoch       uint64  — body offset 74
//
// THE PRODUCTION PATH:
//   profile_config_fixed_save  — the write of ProfileConfig (tables_fixed.rs:1370)
//   profile_config_fixed_load  — the read through the IDENTITY plan (tables_fixed.rs:1407)
//
// The test constructs a ProfileConfigRow with non-default values for all four
// unsigned widths, saves it, reads it back, and asserts every value round-trips.
//
//   rustc --edition 2021 --test --extern tabledemo=... -L ... \
//       test/conformance/rust/rows/UnsignedIntegersValidData.rs \
//       -o build/rows-rust-UnsignedIntegersValidData && ./build/rows-rust-UnsignedIntegersValidData

use tabledemo::{
    profile_config_fixed_load, profile_config_fixed_measure, profile_config_fixed_save,
    ProfileConfigRow, TableFixedEntry, TableFixedReport,
};

#[test]
fn unsigned_integers_valid_data_write_read_acceptance() {
    // ---- THE WRITE: one record with non-default values at all four widths ----
    let mut row = ProfileConfigRow::default();
    row.name[0..4].copy_from_slice(b"test");
    row.name_length = 4;

    // uint32: a value that exercises the full 32-bit space pattern
    row.experience = 0xDEAD_BEEF;
    // uint8: max value
    row.badge = 0xA5;
    // uint16: a value that uses both bytes
    row.port = 0xCAFE;
    // uint64: a value that exercises the full 64-bit space pattern
    row.epoch = 0x0123_4567_89AB_CDEF;

    let need = profile_config_fixed_measure(1);
    let mut file = vec![0u8; need];
    let written = profile_config_fixed_save(&[row], &mut file).expect("the writer saves the record");
    assert_eq!(written, need, "the production writer saves the record (returns measure bytes)");

    // ---- THE READ: identity plan (this build's own hash) reads it back ----
    let mut values = [ProfileConfigRow::default(); 1];
    let mut plan = [TableFixedEntry::default(); 1024];
    let mut remap = [0u16; 1024];
    let mut report = TableFixedReport::default();
    let n = profile_config_fixed_load(&mut values, &file, &mut plan, &mut remap, &mut report)
        .expect("the reader opens its own file");
    assert_eq!(n, 1, "the reader reads one record");
    let back = &values[0];

    // THE LAW: all four unsigned widths round-trip value-for-value
    assert_eq!(
        back.experience, 0xDEAD_BEEF,
        "uint32 (experience): 32-bit unsigned integer round-trips: wrote 0xDEADBEEF, read back 0x{:08X}",
        back.experience
    );
    assert_eq!(
        back.badge, 0xA5,
        "uint8 (badge): 8-bit unsigned integer round-trips: wrote 0xA5, read back 0x{:02X}",
        back.badge
    );
    assert_eq!(
        back.port, 0xCAFE,
        "uint16 (port): 16-bit unsigned integer round-trips: wrote 0xCAFE, read back 0x{:04X}",
        back.port
    );
    assert_eq!(
        back.epoch, 0x0123_4567_89AB_CDEF,
        "uint64 (epoch): 64-bit unsigned integer round-trips: wrote 0x0123456789ABCDEF, read back 0x{:016X}",
        back.epoch
    );

    // clean read: no counters moved
    assert!(
        report.clamped == 0 && !report.malformed && !report.refused,
        "clean read: no counters moved, no refusal"
    );

    // ---- NEGATIVE CONTROL: break the uint32 byte on the wire ----
    //
    // The uint32 `experience` lives at body offset 56. The body starts at:
    //   TABLE_FIXED_HEADER_BYTES (16) + 4 (layout-len) + layout block + 8 (hash)
    // We break one byte of the experience field and verify the read value differs.
    let body_start = 16 + 4 + tabledemo::PROFILE_CONFIG_FIXED_BLOCK.len() + 8;
    let exp_file_offset = body_start + 56;
    file[exp_file_offset] ^= 0xFF; // flip all bits of the first byte

    let mut values = [ProfileConfigRow::default(); 1];
    let mut plan = [TableFixedEntry::default(); 1024];
    let mut remap = [0u16; 1024];
    let mut report = TableFixedReport::default();
    let n = profile_config_fixed_load(&mut values, &file, &mut plan, &mut remap, &mut report)
        .expect("the reader still opens the broken wire");
    assert_eq!(n, 1, "NEGATIVE CONTROL: the reader still opens a wire with one byte flipped");
    let back = &values[0];

    // THE LAW: the broken byte produces a different value — the read is faithful to the wire
    assert_ne!(
        back.experience, 0xDEAD_BEEF,
        "NEGATIVE CONTROL: flipping one byte of the uint32 field on the wire changes the read value — \
         the read IS faithful to the wire (wrote 0xDEADBEEF, read back 0x{:08X})",
        back.experience
    );
}
