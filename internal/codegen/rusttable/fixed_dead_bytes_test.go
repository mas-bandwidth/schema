package rusttable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// EVERY DEAD BYTE IN A FIXED RECORD IS ZERO, AND A NON-ZERO ONE REFUSES THE
// WHOLE LOAD (docs/rulings.json fixed-dead-bytes-are-zero; docs/SPEC-TABLES.md
// §3.4, §13.10; docs/FIXED-FORM-ALGORITHM.md §3.1, §4.5, §5.3 step 11). The
// dead bytes are an absent optional's payload, a counted array's slots past its
// count, a string(N)'s or bytes(N)'s bytes past its used length, and an
// inactive union arm. The generated Rust load answers the reference's verdict
// for every vector in fixedvector.DeadBytes: a changed file refuses the whole
// load and leaves the caller's destination untouched, and a control reads and
// saves back byte for byte.
func TestFixedDeadBytesAreZero(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "cargo (it runs the generated Rust reader over the reference's files)")
	corpus := versionCorpus(t)
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

	var tests strings.Builder
	var ran []string
	for _, vec := range fixedvector.DeadBytes {
		want, err := fixedvector.Read(corpus, vec.File)
		if err != nil {
			t.Fatal(err)
		}
		if want.Refused == vec.Reads {
			t.Fatalf("%s: the vector list says reads=%v and the manifest says refused=%v", vec.File, vec.Reads, want.Refused)
		}
		fn := "dead_" + vec.Name
		ran = append(ran, fn)
		if vec.Reads {
			fmt.Fprintf(&tests, `
#[test]
fn %[1]s() {
    let data = std::fs::read(%[2]q).expect("the reference's %[3]s");
    let mut values = vec![ROWTYPE::default(); 8];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
    assert_eq!(n, Some(1), "%[3]s: a control record reads: {:?}", report);
    let mut out = vec![0u8; data.len()];
    let written = SAVEFN(&values[..1], &mut out).expect("%[3]s: the read value saves");
    assert_eq!(written, data.len(), "%[3]s: the save writes one whole record");
    assert_eq!(&out[..written], &data[..], "%[3]s: save writes the writer's image byte for byte");
}
`, fn, filepath.Join(corpus, vec.File), vec.File)
			continue
		}
		fmt.Fprintf(&tests, `
#[test]
fn %[1]s() {
    let data = std::fs::read(%[2]q).expect("the reference's %[3]s");
    let mut values = vec![ROWTYPE::default(); 8];
    // POISON THE DESTINATION: a load that lands nothing must leave it as it was.
    unsafe {
        core::ptr::write_bytes(
            values.as_mut_ptr() as *mut u8,
            0x5A,
            core::mem::size_of::<ROWTYPE>() * values.len(),
        );
    }
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
    assert!(n.is_none(), "%[3]s: a non-zero dead byte refuses the whole load: {:?} {:?}", n, report);
    for (k, value) in values.iter().enumerate() {
        for (i, &b) in as_bytes(value).iter().enumerate() {
            assert!(b == 0x5A, "%[3]s: a refused load wrote destination {k}.{i} = {b:#04x}");
        }
    }
}
`, fn, filepath.Join(corpus, vec.File), vec.File)
	}

	root := t.TempDir()
	versionWriteCrate(t, root, serialize, versionProbe{
		name:   "probe_dead_bytes",
		reader: "DeadBytes",
		root:   "DeadRoot",
		src:    versionProbeSource(tests.String()),
	})
	cmd := exec.Command("cargo", "test", "--", "--nocapture")
	cmd.Dir = filepath.Join(root, "probe_dead_bytes")
	cmd.Env = append(os.Environ(), "CARGO_TARGET_DIR="+filepath.Join(repo, "build", "rust-versioning-target"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the dead-byte probe did not run: %v\n%s", err, out)
	}
	for _, fn := range ran {
		versionAssert(t, string(out), fn)
	}
	t.Logf("%s", out)

	// AN OLDER WRITER'S DEAD TAIL. The own-layout vectors above take the identity
	// plan. string_grow's file was written by string(8) and is read by string(16),
	// so the load compiles a plan. Shortening the length word from 8 to 7 leaves
	// the last text byte ('h') as one non-zero dead byte of the WRITER's span.
	// The whole file is refused and the caller's destination stays the sentinel.
	// The unmodified file still reads, so a check that refuses every old file
	// cannot pass this row. Verdict only: no reason string.
	oldFile := filepath.Join(corpus, "old_string_grow.bin")
	oldTests := fmt.Sprintf(`
#[test]
fn dead_old_string_clean() {
    let data = std::fs::read(%[1]q).expect("the older writer's clean file");
    let mut values = vec![ROWTYPE::default(); 8];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
    assert_eq!(n, Some(1), "the older writer's clean file reads");
}

#[test]
fn dead_old_string_tail() {
    let mut data = std::fs::read(%[1]q).expect("the older writer's clean file");
    let block = u32::from_le_bytes(data[16..20].try_into().expect("four bytes")) as usize;
    let len_at = 16 + 4 + block + 8 + 4;
    data[len_at..len_at + 4].copy_from_slice(&7i32.to_le_bytes());
    let mut values = vec![ROWTYPE::default(); 8];
    unsafe {
        core::ptr::write_bytes(
            values.as_mut_ptr() as *mut u8,
            0xA5,
            core::mem::size_of::<ROWTYPE>() * values.len(),
        );
    }
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
    assert!(n.is_none(), "the older writer's dead tail loaded {n:?}");
    for (k, value) in values.iter().enumerate() {
        for (i, &b) in as_bytes(value).iter().enumerate() {
            assert!(b == 0xA5, "a refused load wrote destination {k}.{i} = {b:#04x}");
        }
    }
}
`, oldFile)
	oldRoot := t.TempDir()
	versionWriteCrate(t, oldRoot, serialize, versionProbe{
		name:   "probe_dead_bytes_old",
		reader: "VNEW_string_grow",
		older:  []string{"VOLD_string_grow"},
		root:   "StringGrow",
		src:    versionProbeSource(oldTests),
	})
	oldCmd := exec.Command("cargo", "test", "--", "--nocapture")
	oldCmd.Dir = filepath.Join(oldRoot, "probe_dead_bytes_old")
	oldCmd.Env = append(os.Environ(), "CARGO_TARGET_DIR="+filepath.Join(repo, "build", "rust-versioning-target"))
	oldOut, oldErr := oldCmd.CombinedOutput()
	if oldErr != nil {
		t.Fatalf("the older-writer dead-byte probe did not run: %v\n%s", oldErr, oldOut)
	}
	for _, fn := range []string{"dead_old_string_clean", "dead_old_string_tail"} {
		versionAssert(t, string(oldOut), fn)
	}
	t.Logf("%s", oldOut)

	// A writer and reader with different layouts exercise a compiled plan.
	file := filepath.Join(t.TempDir(), "old_optional.bin")
	writer := fmt.Sprintf(`
#[test]
fn write_old_optional() {
    let mut row = ROWTYPE::default();
    row.lead = 17;
    row.trail = 23;
    let mut data = vec![0u8; 65536];
    let n = SAVEFN(&[row], &mut data).expect("save older layout");
    std::fs::write(%[1]q, &data[..n]).expect("write older layout");
}
`, file)
	reader := fmt.Sprintf(`
#[test]
fn old_optional_dead_payload() {
    let clean = std::fs::read(%[1]q).expect("older layout");
    let block = u32::from_le_bytes(clean[16..20].try_into().unwrap()) as usize;
    let body = 16 + 4 + block + 8;
    assert_eq!(clean[body + 1], 0, "optional is absent");
    assert_eq!(&clean[body + 2..body + 6], &[0; 4]);
    for bad in [false, true] {
        let mut data = clean.clone();
        if bad { data[body + 2] = 1; }
        let mut values = vec![ROWTYPE::default(); 1];
        let before = as_bytes(&values[0]).to_vec();
        let mut plan = vec![TableFixedEntry::default(); 4096];
        let mut remap = vec![0u16; 4096];
        let mut report = TableFixedReport::default();
        let n = LOADFN(&mut values, &data, &mut plan, &mut remap, &mut report);
        if bad {
            assert!(n.is_none(), "nonzero absent payload loaded {n:?}");
            assert_eq!(as_bytes(&values[0]), before);
        } else {
            assert_eq!(n, Some(1), "clean absent optional reads");
            assert_eq!((values[0].lead, values[0].trail), (17, 23));
        }
    }
}
`, file)
	for _, probe := range []versionProbe{
		{name: "dead_optional_writer", reader: "VOLD_hostile_bool", root: "Hbb", src: versionProbeSource(writer)},
		{name: "dead_optional_reader", reader: "VNEW_hostile_bool", older: []string{"VOLD_hostile_bool"}, root: "Hbb", src: versionProbeSource(reader)},
	} {
		probeRoot := t.TempDir()
		versionWriteCrate(t, probeRoot, serialize, probe)
		cmd := exec.Command("cargo", "test", "--", "--nocapture")
		cmd.Dir = filepath.Join(probeRoot, probe.name)
		cmd.Env = append(os.Environ(), "CARGO_TARGET_DIR="+filepath.Join(repo, "build", "rust-versioning-target"), "CARGO_NET_OFFLINE=true")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", probe.name, err, out)
		}
		t.Logf("%s", out)
	}

}
