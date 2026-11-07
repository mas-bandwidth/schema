package rusttable

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// A malformed read returns no records and resets caller storage. A named
// preflight refusal leaves caller storage untouched. An accumulated malformed
// bit from an earlier read must not turn a later valid read into fresh damage.
func TestFixedLoadDamageDefaultsAndReportReuseRust(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it builds and runs the generated fixed-load probe)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatal(err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
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
    for (name, data) in [("empty", &[][..]), ("short-form3", &[3][..]), ("reserved-byte3", &reserved[..])] {
        let mut values = vec![poison(); 2];
        let mut report = TableFixedReport::default();
        let got = root_fixed_load(&mut values, data, &mut plan, &mut remap, &mut report);
        if got.is_some() || !report.malformed || report.refused {
            failures.push(format!("{name}: expected malformed without usable records"));
        }
        if values.iter().any(|v| v.a != 0 || v.b != 0 || v.inner.n != 0 || v.inner.ok || v.f != 0.0) {
            failures.push(format!("{name}: damaged load left caller values"));
        }
    }
    for (form, reason) in [(0u8, TableFixedReason::NewerForm),
                           (1u8, TableFixedReason::PreviousForm),
                           (2u8, TableFixedReason::MessageFormAsFile)] {
        let mut values = vec![poison(); 2];
        let mut report = TableFixedReport::default();
        let got = root_fixed_load(&mut values, &[form], &mut plan, &mut remap, &mut report);
        if got.is_some() || report.malformed || !report.refused || report.reason != reason {
            failures.push(format!("form{form}: expected named refusal"));
        }
        if values.iter().any(|v| v.a != 77 || v.b != 77 || v.inner.n != 77 || !v.inner.ok || v.f != 0.0) {
            failures.push(format!("form{form}: named preflight refusal changed caller values"));
        }
    }
    let mut values = vec![RootRow::default(); 2];
    let mut report = TableFixedReport::default();
    report.malformed = true;
    if root_fixed_load(&mut values, &valid, &mut plan, &mut remap, &mut report) != Some(2)
        || values[0].a != 5 || values[1].a != 6 || !report.malformed {
        failures.push("reused report: old damage changed this valid read".to_string());
    }
    if failures.is_empty() { println!("FIXED_TRANSACTION ok"); }
    else { for failure in failures { println!("FIXED_TRANSACTION BAD {failure}"); } std::process::exit(1); }
 `
	out, err := runRustDiscardProbe(t, u, body)
	if err != nil || !strings.Contains(out, "FIXED_TRANSACTION ok") {
		t.Fatalf("%v\n%s", err, out)
	}
}
