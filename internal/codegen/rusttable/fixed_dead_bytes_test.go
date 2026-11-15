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
}
