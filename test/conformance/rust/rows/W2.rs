// W2.rs — rust/W2: absent optional skips store
//
// Law: docs/FIXED-FORM-ALGORITHM.md:205
//   "An absent optional writes flag 0 and skips the payload store"
//
// Schema: test/tables/P3.schema — Chain has `?Link`, a by-value optional.
// The writer zero-fills the body template, then writes only the live fields.
// When link_present is false, the Link payload stays as the template's zeros —
// not the caller's storage, not the field's declared default.

#[test]
fn test_row_w2() {
    use tblp3::*;

    // --- ABSENT: link_present = false, payload STAINED with 0x5A ---
    let mut v = ChainRow::default();
    v.name[..6].copy_from_slice(b"absent");
    v.name_length = 6;
    v.link_present = false;
    // stain the payload — a byte no clean record carries
    v.link.value = 0x5A5A5A;
    v.link.tag.fill(0x5A);
    v.link.tag_length = 5;

    // the stain is really there in storage
    assert!(
        v.link.tag[0] == 0x5A,
        "CONTROL: the absent payload is stained in storage"
    );

    // save
    let need = chain_fixed_measure(1);
    let mut w = vec![0u8; need];
    let n = chain_fixed_save(&[v], &mut w).expect("save succeeds");
    assert_eq!(n, need, "absent optional: the record saves");

    // the body starts after the header + layout length + layout block
    let body_at = TABLE_FIXED_HEADER_BYTES + 4 + CHAIN_FIXED_BLOCK.len() + 8;
    let body = &w[body_at..body_at + CHAIN_FIXED_BODY_BYTES];

    // THE LAW: not one byte of the absent payload reached the wire
    assert!(
        !body.contains(&0x5A),
        "ABSENT OPTIONAL: not one byte of the absent payload reached the wire"
    );

    // the flag on the wire is 0
    assert_eq!(
        body[20], 0,
        "absent optional: the flag is 0 on the wire"
    );

    // --- READ BACK ---
    let mut back = [ChainRow::default(); 1];
    let mut plan = [TableFixedEntry::default(); 1024];
    let mut remap = [0u16; 1024];
    let mut report = TableFixedReport::default();
    let rn = chain_fixed_load(&mut back, &w, &mut plan, &mut remap, &mut report)
        .expect("read succeeds");
    assert_eq!(rn, 1, "absent optional: the record reads");
    assert!(!back[0].link_present, "absent optional: the flag reads false");
    assert_eq!(
        back[0].link.value, 0,
        "absent optional: the payload value reads as the wire's zeros"
    );
    assert_eq!(
        back[0].link.tag_length, 0,
        "absent optional: the payload tag_length reads as the wire's zeros"
    );
    assert!(
        report.clamped == 0 && !report.malformed && !report.refused,
        "absent optional: a clean read moves no counter"
    );

    // --- NEGATIVE CONTROL: the SAME payload PRESENT ---
    let mut present = v;
    present.link_present = true;
    present.link.tag_length = 4;
    let mut pw = vec![0u8; need];
    chain_fixed_save(&[present], &mut pw).expect("present twin saves");
    let pbody = &pw[body_at..body_at + CHAIN_FIXED_BODY_BYTES];
    assert!(
        pbody.contains(&0x5A),
        "NEGATIVE CONTROL: the SAME payload PRESENT really does reach the wire"
    );
}
