package rusttable

import (
	"os/exec"
	"strings"
	"testing"
)

// Framing damage — no first byte, a short header, a nonzero reserved byte — is
// a refusal BY NAME (docs/FIXED-FORM-ALGORITHM.md §5.3 steps 1 and 2:
// truncated, truncated, reserved_not_zero; this form's report has no malformed
// flag), and it returns no records and resets caller storage. A named preflight
// refusal resets every row handed in too (ruling R4). A read clears the
// report it is handed (SPEC-TABLES §6.5, §6.6): an earlier read's damage bit neither
// turns a later valid read into damage nor survives it.
func TestFixedLoadDamageDefaultsAndReportReuseRust(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("rustc"); err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, rustDiscardUnit)
	body := `
    let rows = [RootRow { a: 5, inner: InnerRow { ok: true, n: 3 }, b: 9, f: 0.0 },
                RootRow { a: 6, inner: InnerRow { ok: true, n: 4 }, b: 10, f: 0.0 }];
    let mut valid = vec![0u8; root_fixed_measure(2)];
    assert!(root_fixed_save(&rows, &mut valid).is_some());
    let mut plan = [TableFixedEntry::default(); 64];
    let mut remap = [0u16; 64];
    let poison = || RootRow { a: 77, inner: InnerRow { ok: true, n: 77 }, b: 77, f: 0.0 };
    let mut reserved = valid.clone(); reserved[3] = 1;
    let mut failures = Vec::new();
    for (name, data, reason) in [("empty", &[][..], TableFixedReason::Truncated),
                                 ("short-form3", &[3][..], TableFixedReason::Truncated),
                                 ("reserved-byte3", &reserved[..], TableFixedReason::ReservedNotZero)] {
        let mut values = vec![poison(); 2];
        let mut report = TableFixedReport::default();
        let got = root_fixed_load(&mut values, data, &mut plan, &mut remap, &mut report);
        if got.is_some() || !report.refused || report.reason != reason {
            failures.push(format!("{name}: expected the refusal {} without usable records: {report:?}", reason.name()));
        }
        if values.iter().any(|v| v.a != 0 || v.b != 0 || v.inner.n != 0 || v.inner.ok || v.f != 0.0) {
            failures.push(format!("{name}: refused load left caller values"));
        }
    }
    for (form, reason) in [(0u8, TableFixedReason::NewerForm),
                           (1u8, TableFixedReason::PreviousForm),
                           (2u8, TableFixedReason::MessageFormAsFile)] {
        let mut values = vec![poison(); 2];
        let mut report = TableFixedReport::default();
        let got = root_fixed_load(&mut values, &[form], &mut plan, &mut remap, &mut report);
        if got.is_some() || !report.refused || report.reason != reason {
            failures.push(format!("form{form}: expected named refusal"));
        }
        if values.iter().any(|v| v.a != 0 || v.b != 0 || v.inner.n != 0 || v.inner.ok || v.f != 0.0) {
            failures.push(format!("form{form}: named refusal left caller values (R4: every row resets)"));
        }
    }
    let mut values = vec![RootRow::default(); 2];
    let mut report = TableFixedReport::default();
    report.refused = true;
    report.reason = TableFixedReason::WireDamaged;
    report.unknown = 3;
    if root_fixed_load(&mut values, &valid, &mut plan, &mut remap, &mut report) != Some(2)
        || values[0].a != 5 || values[1].a != 6 || report != TableFixedReport::default() {
        failures.push(format!("reused report: a valid read did not clear the report it was handed: {report:?}"));
    }
    if failures.is_empty() { println!("FIXED_TRANSACTION ok"); }
    else { for failure in failures { println!("FIXED_TRANSACTION BAD {failure}"); } std::process::exit(1); }
 `
	out, err := runRustDiscardProbe(t, u, body)
	if err != nil || !strings.Contains(out, "FIXED_TRANSACTION ok") {
		t.Fatalf("%v\n%s", err, out)
	}
}
