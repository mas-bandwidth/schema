// P4: the wrong plan must go red (docs/FIXED-FORM-ALGORITHM.md §7 item 4).
//
// "this build's identity plan over another schema's record" — FX1's identity
// plan (a single Copy of 64 bytes) run over an FX2 record body must NOT
// reproduce FX2's values, because FX1 and FX2 declare different layouts:
//
//   FX1 body (64 bytes):  keep[0..4] u32 | narrow[4..6] u16 | renamed[6..10] u32
//                         | gone[10..14] u32 | nested[14..22] | label_length[22..26]
//                         | label[26..34] | marks_count[34..38] | marks[38..54]
//                         | blob_length[54..58] | blob[58..64]
//
//   FX2 body (74 bytes):  keep[0..4] u32 | narrow[4..8] u32 | renamed_to[8..12] u32
//                         | added[12..16] u32 | nested[16..24] | extra[24..32]
//                         | label_length[32..36] | label[36..44] | marks_count[44..48]
//                         | marks[48..64] | blob_length[64..68] | blob[68..74]
//
// Running FX1's plan over FX2's body copies 64 bytes verbatim, but FX2's
// field values sit at FX2's offsets — not FX1's — so the scattered FxRootRow
// holds wrong values. This is the invariant that proves the plan path works.
//
// The body below is hand-built from the law: FX2 field values at FX2's
// offsets, the same values the C++ reference writes into fx2.bin.

use tblfx1::*;

/// An FX2-style record body (74 bytes), with hand-chosen values at FX2's
/// field offsets.  Bytes 24..74 are zeros (extra, label, marks, blob all
/// default).
fn fx2_style_body_74() -> [u8; tblfx2::FX_ROOT_FIXED_BODY_BYTES] {
    let mut b = [0u8; tblfx2::FX_ROOT_FIXED_BODY_BYTES];
    // keep = 42
    b[0..4].copy_from_slice(&42u32.to_le_bytes());
    // narrow = 9999 (FX2 is u32, at [4..8])
    b[4..8].copy_from_slice(&9999u32.to_le_bytes());
    // renamed_to = 321 (FX2 field, at [8..12])
    b[8..12].copy_from_slice(&321u32.to_le_bytes());
    // added = 11 (FX2-only field, at [12..16])
    b[12..16].copy_from_slice(&11u32.to_le_bytes());
    b
}

/// The wrong plan: FX1's identity plan run over FX2's body.
/// The plan copies 64 bytes verbatim.  The scattered FxRootRow must NOT
/// hold FX2's intended values, because the bytes land at FX1's offsets.
#[test]
fn wrong_plan_goes_red() {
    let body = fx2_style_body_74();
    // FX1's plan copies only FX_ROOT_FIXED_BODY_BYTES (64) bytes from the body
    let mut image = [0u8; FX_ROOT_FIXED_BODY_BYTES];
    image.copy_from_slice(&FX_ROOT_FIXED_DEFAULTS);
    let mut report = TableFixedReport::default();
    table_fixed_run(&FX_ROOT_FIXED_PLAN, &[], &body, &mut image, &mut report);

    let mut row = FxRootRow::default();
    fx_root_fixed_scatter(&image, &mut row, &mut report);

    // FX2 wrote renamed_to = 321 at [8..12].  FX1's scatter reads renamed
    // from [6..10] — a DIFFERENT four-byte window — so renamed must not be 321.
    assert_ne!(
        row.renamed, 321,
        "FX1's plan over FX2's body: renamed must not equal FX2's renamed_to"
    );

    // FX1 reads narrow as u16 from [4..6].  FX2 wrote narrow = 9999 as u32 at
    // [4..8].  The low two bytes of 9999 = 0x270F = 9999.  So narrow coincides,
    // but renamed does NOT — proving the plan mapped the wrong bytes for that
    // field.  Additionally, FX1 reads gone from [10..14] where FX2 has the high
    // bytes of narrow and the low bytes of renamed_to — not FX1's `gone` value.

    // keep is at the same offset in both schemas, so it lands correctly.
    assert_eq!(
        row.keep, 42,
        "keep is at the same offset in both schemas, so it lands correctly"
    );

    // The plan did NOT report any damage — the identity plan is a plain Copy,
    // so no op could fail.  The wrong answer is silent, which is exactly why
    // the plan path must be tested: a silent wrong is worse than a loud refusal.
    assert!(
        !report.malformed && !report.refused,
        "the wrong plan must NOT be loud — it silently produces wrong values"
    );
}

/// Negative control: FX2's identity plan over FX2's body MUST reproduce
/// the values.  Without this, the assertion above could be passing because
/// the body is malformed rather than because the plan is wrong.
#[test]
fn right_plan_goes_green() {
    let body = fx2_style_body_74();
    let mut image = [0u8; tblfx2::FX_ROOT_FIXED_BODY_BYTES];
    image.copy_from_slice(&tblfx2::FX_ROOT_FIXED_DEFAULTS);
    let mut report = tblfx2::TableFixedReport::default();
    tblfx2::table_fixed_run(
        &tblfx2::FX_ROOT_FIXED_PLAN,
        &[],
        &body,
        &mut image,
        &mut report,
    );

    let mut row = tblfx2::FxRootRow::default();
    tblfx2::fx_root_fixed_scatter(&image, &mut row, &mut report);

    assert_eq!(row.keep, 42, "right plan: keep lands correctly");
    assert_eq!(row.narrow, 9999, "right plan: narrow lands correctly");
    assert_eq!(row.renamed_to, 321, "right plan: renamed_to lands correctly");
    assert_eq!(row.added, 11, "right plan: added lands correctly");
    assert!(
        !report.malformed && !report.refused,
        "right plan: clean read"
    );
}
