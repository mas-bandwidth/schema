package rusttable

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// EVERY DEAD BYTE IS ZERO (docs/SPEC-TABLES.md §3.4, THE SLACK RULE; the
// owner's ruling of 2026-11-10, docs/rulings.json fixed-dead-bytes-zero): one
// stained byte of each class of slack refuses the whole load as damage — an
// absent optional's payload, a counted array's tail, a text's and a byte
// buffer's bytes past the used length, a union's arm bytes behind a narrower
// arm, under tag 0 and under a payload-free arm, a text's tail inside a LIVE
// array element, and a whole DEAD element. The identity read holds this
// through the generated d_fixed_dead; the plan a read through the lock runs
// holds it through its Absent, Tail and Arm entries, exercised here by
// compiling this build's own block against itself and running the loop over
// the same stains. The controls: the clean record reads, a ranged `min = 1`
// field inside a DEAD element reads (slack is tested for zero and nothing
// else), and a byte of the widest arm that is LIVE under its tag reads.
const rustDeadBytesUnit = `package dprobe

type Item
{
    name string(4)
    level int8 = 1 | min = 1, max = 9
}

union Pick
{
    narrow int8
    wide int32
    ping
    word string(3)
}

fixed table D
{
    opt ?int32
    list [..3]int16
    label string(5)
    blob bytes(4)
    pick Pick
    items [..2]Item
}
`

func TestFixedDeadBytesRefusedRust(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it builds and runs the generated fixed-load probe)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatal(err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}
	u := unitFrom(t, rustDeadBytesUnit)
	// The body offsets: opt's present 0 and payload 1..5; list's count 5..9 and
	// slots 9..15; label's length 15..19 and bytes 19..24; blob's length 24..28
	// and bytes 28..32; pick's tag 32 and arms 33..40 (the widest, word, is
	// 4 + 3); items' count 40..44 and two elements of 9 at 44 and 53, each a
	// length word, four bytes and the ranged level.
	body := `
    fn row(tag: u8) -> DRow {
        let mut row = DRow::default();
        row.opt_present = false;
        row.list_count = 1;
        row.list[0] = 7;
        row.label[..2].copy_from_slice(b"ab");
        row.label_length = 2;
        row.blob[0] = 9;
        row.blob_length = 1;
        row.pick.tag = tag;
        match tag {
            1 => row.pick.arms.narrow = 5,
            2 => row.pick.arms.wide = -1,
            _ => {}
        }
        row.items_count = 1;
        row.items[0].name[0] = b'x';
        row.items[0].name_length = 1;
        row.items[0].level = 1;
        row
    }
    fn file(tag: u8) -> (Vec<u8>, usize) {
        let mut data = vec![0u8; d_fixed_measure(1)];
        assert!(d_fixed_save(&[row(tag)], &mut data).is_some());
        let block = u32::from_le_bytes(data[16..20].try_into().unwrap()) as usize;
        (data, 16 + 4 + block + 8)
    }
    fn load(data: &[u8]) -> (Option<usize>, TableFixedReport, DRow) {
        let mut plan = vec![TableFixedEntry::default(); 4096];
        let mut remap = vec![0u16; 4096];
        let mut values = vec![row(2); 1];
        let mut report = TableFixedReport::default();
        let got = d_fixed_load(&mut values, data, &mut plan, &mut remap, &mut report);
        (got, report, values[0])
    }
    fn lineage_damaged(body: &[u8]) -> bool {
        let mine = TableFixedBlock::parse(&D_FIXED_BLOCK).expect("this build's own block parses");
        let mut plan = vec![TableFixedEntry::default(); 4096];
        let mut guards = vec![TableFixedGuard::default(); 4096];
        let mut remap = vec![0u16; 4096];
        let mut report = TableFixedReport::default();
        let n = table_fixed_compile(&mine, &mine, &D_FIXED_COUNTED, &mut plan, &mut guards, &mut remap, &mut report)
            .expect("the own-against-own plan compiles");
        let mut image = vec![0u8; D_FIXED_BODY_BYTES];
        table_fixed_run(&plan[..n], &guards, &remap, body, &mut image, &mut report);
        report.damaged
    }
    let mut failures = Vec::new();
    for tag in [0u8, 1, 2, 3, 4] {
        let (data, at) = file(tag);
        let (got, report, _) = load(&data);
        if got != Some(1) || report.damaged || report.refused {
            failures.push(format!("clean tag {tag}: the saved file must read (a min = 1 field in a dead element included): {:?}", report));
        }
        if lineage_damaged(&data[at..at + D_FIXED_BODY_BYTES]) {
            failures.push(format!("clean tag {tag}: the compiled plan called a clean record damaged"));
        }
    }
    // a byte of the widest arm that is LIVE under tag 2 (wide, bytes 33..37) is data
    {
        let (mut data, at) = file(2);
        data[at + 36] = 0x40;
        let (got, report, value) = load(&data);
        if got != Some(1) || report.damaged || unsafe { value.pick.arms.wide } != 0x40ff_ffff {
            failures.push(format!("live arm byte: must read as data: {:?}", report));
        }
    }
    for (name, tag, off) in [
        ("absent-optional-payload", 1u8, 2usize),
        ("counted-array-tail", 1, 11),
        ("text-tail", 1, 23),
        ("bytes-tail", 1, 30),
        ("behind-narrow-arm", 1, 35),
        ("under-tag-0", 0, 34),
        ("under-payload-free-arm", 3, 33),
        ("behind-text-arm-used-length", 4, 37),
        ("text-tail-in-live-element", 1, 50),
        ("dead-element", 1, 55),
    ] {
        for stain in [1u8, 0x80] {
            let (mut data, at) = file(tag);
            data[at + off] = stain;
            let (got, report, value) = load(&data);
            if got.is_some() || !report.damaged || report.refused {
                failures.push(format!("{name} ({stain}): stained slack must be damage: {:?} {:?}", got, report));
            }
            if value.list_count != 0 || value.pick.tag != 0 || value.items_count != 0 {
                failures.push(format!("{name}: a damaged load left a caller value"));
            }
            if !lineage_damaged(&data[at..at + D_FIXED_BODY_BYTES]) {
                failures.push(format!("{name} ({stain}): the compiled plan let stained slack through"));
            }
        }
    }
    if failures.is_empty() { println!("FIXED_DEAD_BYTES ok"); }
    else { for failure in failures { println!("FIXED_DEAD_BYTES BAD {failure}"); } std::process::exit(1); }
`
	out, err := runRustDiscardProbe(t, u, body)
	if err != nil || !strings.Contains(out, "FIXED_DEAD_BYTES ok") {
		t.Fatalf("%v\n%s", err, out)
	}
}
