package rusttable

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// A BOOL OR PRESENT BYTE OTHER THAN 0 OR 1 IS DAMAGE (docs/FIXED-FORM-ALGORITHM.md
// §4.5, the strict read): the whole load answers None with `damaged` set and
// every caller row back at its declared defaults. ON LIVE DATA ONLY (§3.4, the
// slack rule): the payload under an absent optional and the slots past a
// counted array's count are unspecified on read and never a refusal.
// tables-rust-bool-byte-negative-control takes the scatter's check out and
// requires this test red.
const rustBoolByteUnit = `package bprobe

type Inner
{
    on bool
}

union Pick
{
    flag bool
    inner Inner
}

fixed table B
{
    ok bool
    opt ?bool
    oi ?Inner
    flags [..3]bool
    fixedflags [2]bool
    pick Pick
}
`

func TestFixedBoolByteIsDamageRust(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it builds and runs the generated fixed-load probe)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatal(err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}
	u := unitFrom(t, rustBoolByteUnit)
	// The body offsets are the fixed image's: ok 0, opt's present 1 and payload
	// 2, oi's present 3 and payload 4, flags' count 5..9 and slots 9..12,
	// fixedflags 12..14, pick's tag 14 and arm 15.
	body := `
    let mut row = BRow::default();
    row.ok = true;
    row.opt_present = true;
    row.opt = false;
    row.flags_count = 1;
    row.flags[0] = true;
    row.fixedflags = [true, true];
    row.pick.tag = 1;
    row.pick.arms.flag = true;
    let mut valid = vec![0u8; b_fixed_measure(1)];
    assert!(b_fixed_save(&[row], &mut valid).is_some());
    let block = u32::from_le_bytes(valid[16..20].try_into().unwrap()) as usize;
    let body_at = 16 + 4 + block + 8;
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut failures = Vec::new();
    // the untouched file reads, so every refusal below is the forgery
    {
        let mut values = vec![BRow::default(); 1];
        let mut report = TableFixedReport::default();
        let got = b_fixed_load(&mut values, &valid, &mut plan, &mut remap, &mut report);
        if got != Some(1) || report.damaged || report.refused || !values[0].ok || !unsafe { values[0].pick.arms.flag } {
            failures.push(format!("clean: the saved file must read: {:?}", report));
        }
    }
    for (name, at) in [("ok", 0usize), ("opt-present", 1), ("opt-payload", 2), ("oi-present", 3),
                       ("flags-live-slot", 9), ("fixedflags", 13), ("pick-arm", 15)] {
        let mut data = valid.clone();
        data[body_at + at] = 2;
        let mut values = vec![row; 1];
        let mut report = TableFixedReport::default();
        let got = b_fixed_load(&mut values, &data, &mut plan, &mut remap, &mut report);
        if got.is_some() || !report.damaged || report.refused {
            failures.push(format!("{name}: a live byte of 2 must be damage: {:?} {:?}", got, report));
        }
        if values[0].ok || values[0].opt_present || values[0].flags_count != 0 || values[0].pick.tag != 0 {
            failures.push(format!("{name}: a damaged load left a caller value"));
        }
    }
    for (name, at) in [("oi-payload-absent", 4usize), ("flags-slack-slot", 11)] {
        let mut data = valid.clone();
        data[body_at + at] = 2;
        let mut values = vec![BRow::default(); 1];
        let mut report = TableFixedReport::default();
        let got = b_fixed_load(&mut values, &data, &mut plan, &mut remap, &mut report);
        if got != Some(1) || report.damaged || report.refused {
            failures.push(format!("{name}: slack is never a refusal: {:?} {:?}", got, report));
        }
    }
    if failures.is_empty() { println!("FIXED_BOOL_BYTE ok"); }
    else { for failure in failures { println!("FIXED_BOOL_BYTE BAD {failure}"); } std::process::exit(1); }
`
	out, err := runRustDiscardProbe(t, u, body)
	if err != nil || !strings.Contains(out, "FIXED_BOOL_BYTE ok") {
		t.Fatalf("%v\n%s", err, out)
	}
}
