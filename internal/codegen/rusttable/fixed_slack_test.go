package rusttable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/testproc"
)

// THE SLOTS PAST A COUNT AFTER ANY LOAD, LAWFUL OR REFUSED (docs/SPEC-TABLES.md
// §3.4): test/tables/FixedSlack.schema's recipe, run on the generated Rust. A
// Row that held file A's full arrays, loaded with file B's short ones, reads
// back exactly as a fresh value loaded with B. Rust's Row::default() is the
// cooked zero form, so the FRESH value here is what the refusal's reset makes
// of a row: SLACK_ROOT_FIXED_DEFAULTS scattered in, the declared defaults in
// every slot. Every slot past every count holds its declared default (id 7,
// v 3), never the writer's zeros and never the 77s the row held; and a refused
// load still resets every slot to those defaults. The identity plan and every
// lineage plan land the same image through the same scatter, so the identity
// file is the case the scatter sees for both.
func TestRustFixedSlackPastCountHoldsDefaults(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "cargo (it runs the generated Rust reader)")
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	serialize, err := filepath.Abs(filepath.Join(repo, "..", "serialize.rs"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(serialize); err != nil {
		t.Fatalf("the Rust runtime is not beside this tree at %s: %v", serialize, err)
	}
	tests := `
fn fresh() -> ROWTYPE {
    let mut v = ROWTYPE::default();
    SCATTERFN(&DEFAULTSCONST, &mut v, &mut TableFixedReport::default());
    v
}

fn save(v: &ROWTYPE) -> Vec<u8> {
    let mut out = vec![0u8; slack_root_fixed_measure(1)];
    let n = SAVEFN(core::slice::from_ref(v), &mut out).expect("the value saves");
    assert_eq!(n, out.len());
    out
}

fn load(v: &mut ROWTYPE, data: &[u8]) -> Option<usize> {
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    LOADFN(core::slice::from_mut(v), data, &mut plan, &mut remap, &mut report)
}

// every field of the Row, by name: a byte view would compare padding too
fn same(a: &ROWTYPE, b: &ROWTYPE) -> bool {
    a.cells_count == b.cells_count
        && a.nums_count == b.nums_count
        && a.nums == b.nums
        && (0..3).all(|i| {
            a.cells[i].id == b.cells[i].id
                && a.cells[i].subs_count == b.cells[i].subs_count
                && (0..2).all(|j| a.cells[i].subs[j].v == b.cells[i].subs[j].v)
        })
}

fn check_defaults_past_counts(v: &ROWTYPE, what: &str) {
    for i in 1..3 {
        let c = &v.cells[i];
        assert!(
            c.id == 7 && c.subs_count == 0 && c.subs[0].v == 3 && c.subs[1].v == 3,
            "{what}: cells[{i}] past the count is id {} subs_count {} subs v {} {}, want id 7, subs count 0, every sub v 3",
            c.id, c.subs_count, c.subs[0].v, c.subs[1].v
        );
    }
    assert_eq!(v.cells[0].subs[1].v, 3, "{what}: cells[0].subs[1].v past its count");
    for i in 1..4 {
        assert_eq!(v.nums[i], 0, "{what}: nums[{i}] past the count");
    }
}

#[test]
fn slack_past_count_holds_defaults() {
    let mut a = fresh();
    a.cells_count = 3;
    for c in a.cells.iter_mut() {
        c.id = 77;
        c.subs_count = 2;
        for s in c.subs.iter_mut() {
            s.v = 77;
        }
    }
    a.nums_count = 4;
    a.nums = [77; 4];
    let mut b = fresh();
    b.cells_count = 1;
    b.cells[0].id = 77;
    b.cells[0].subs_count = 1;
    b.cells[0].subs[0].v = 77;
    b.nums_count = 1;
    b.nums[0] = 77;
    let (file_a, file_b) = (save(&a), save(&b));

    let mut want = fresh();
    assert_eq!(load(&mut want, &file_b), Some(1), "B into a fresh value");
    let mut v = fresh();
    assert_eq!(load(&mut v, &file_a), Some(1), "A");
    assert!(v.cells_count == 3 && v.cells[2].subs[1].v == 77, "A did not load whole");
    assert_eq!(load(&mut v, &file_b), Some(1), "B into the reused value");
    check_defaults_past_counts(&v, "lawful");
    assert!(same(&v, &want), "the reused value is not the fresh one");

    // A REFUSED LOAD (a record whose hash is not the header's) resets the row
    // handed in to its declared defaults.
    assert_eq!(load(&mut v, &file_a), Some(1), "A again");
    let mut bad = file_b.clone();
    let layout = u32::from_le_bytes(bad[TABLE_FIXED_HEADER_BYTES..TABLE_FIXED_HEADER_BYTES + 4].try_into().unwrap()) as usize;
    bad[TABLE_FIXED_HEADER_BYTES + 4 + layout] ^= 0xff; // the record's hash
    assert_eq!(load(&mut v, &bad), None, "the forged record read");
    check_defaults_past_counts(&v, "refused");
    assert!(same(&v, &fresh()), "the refused load did not leave the declared defaults");
}
`
	root := t.TempDir()
	versionWriteCrate(t, root, serialize, versionProbe{
		name:   "probe_fixed_slack",
		reader: "FixedSlack",
		root:   "SlackRoot",
		src:    versionProbeSource(tests),
	})
	cmd := testproc.Command(t, "cargo", "test", "--", "--nocapture")
	cmd.Dir = filepath.Join(root, "probe_fixed_slack")
	cmd.Env = append(os.Environ(), "CARGO_TARGET_DIR="+filepath.Join(repo, "build", "rust-versioning-target"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the slack probe failed: %v\n%s", err, out)
	}
	versionAssert(t, string(out), "slack_past_count_holds_defaults")
}

// The emitter pin, with no toolchain: FixedSlack.schema's scatter lands every
// dead slot of both counted arrays from the element's declared-defaults image,
// and a unit whose counted arrays all have all-zero element defaults generates
// no slack code at all.
func TestRustFixedSlackEmitterPin(t *testing.T) {
	t.Parallel()
	text := fixedSlackText(t, versionSchema(t, "FixedSlack"))
	for _, want := range []string{
		"const SCHEMA_FRESH: [u8; 2] = [0x03, 0x00];",
		"const SCHEMA_FRESH: [u8; 12] = [0x07, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03, 0x00, 0x03, 0x00];",
		"let schema_b: &[u8] = &SCHEMA_FRESH;",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the generated FixedSlack scatter lacks %q", want)
		}
	}
	zero := fixedSlackText(t, `package zeroslack

fixed table Cell
{
    id   int32
    subs [..2]int16
}

fixed table Root
{
    cells [..3]Cell
    nums  [..4]int32
}
`)
	if strings.Contains(zero, "SCHEMA_FRESH") {
		t.Fatal("a unit whose counted arrays all have all-zero element defaults generated slack code")
	}
}

func fixedSlackText(t *testing.T, src string) string {
	t.Helper()
	files, err := GenerateLineage(versionUnit(t, src), map[string][]FixedLineageEntry{})
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, data := range files {
		all.Write(data)
	}
	return all.String()
}
