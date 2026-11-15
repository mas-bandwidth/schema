package rusttable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A compressed float's -0 lands as +0 and is written as +0
// (docs/SPEC-TABLES.md §3.3, docs/rulings.json fixed-compressed-negzero). The
// vector is the reference writer's file holding the sign bit alone in the
// compressed field; this leg reads it through its generated load, lands the
// value 0 with the sign bit clear, refuses nothing, and saves the canonical
// bytes the +0 record writes. Plain float32 and float64 fields keep -0
// bit-exact.
func TestFixedCompressedNegZeroRead(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (a generated fixed-load probe)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}
	corpus := versionCorpus(t)
	u := versionUnit(t, versionSchema(t, "FNZ"))
	if len(fixedvector.NegativeZero) == 0 {
		t.Fatal("fixedvector.NegativeZero names no vector")
	}
	for _, vec := range fixedvector.NegativeZero {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			file := filepath.Join(corpus, vec.File)
			if _, err := os.Stat(file); err != nil {
				t.Fatalf("the negative-zero vector %s is not in %s: %v", vec.File, corpus, err)
			}
			body := fmt.Sprintf(`
    let data = std::fs::read(%[1]q).expect("read negative-zero vector");
    let layout_bytes = u32::from_le_bytes(
        data[TABLE_FIXED_HEADER_BYTES..TABLE_FIXED_HEADER_BYTES + 4]
            .try_into()
            .expect("four bytes"),
    ) as usize;
    let value_at = TABLE_FIXED_HEADER_BYTES + 4 + layout_bytes + 8 + 4;
    let got = u32::from_le_bytes(data[value_at..value_at + 4].try_into().expect("four bytes"));
    assert_eq!(got, 0x8000_0000, "the vector does not hold a compressed float's -0: {got:#010x}");

    let mut back = vec![NegZeroRow::default(); 1];
    let mut plan = vec![TableFixedEntry::default(); 64];
    let mut remap = vec![0u16; 64];
    let mut report = TableFixedReport::default();
    let n = neg_zero_fixed_load(&mut back, &data, &mut plan, &mut remap, &mut report);
    assert_eq!(n, Some(1), "the reader refused a compressed -0: n={n:?} {report:?}");
    assert_eq!(report, TableFixedReport::default(), "the read of a compressed -0 reported {report:?}");
    assert_eq!(back[0].lead, 1, "the lead bracket moved: {}", back[0].lead);
    assert_eq!(back[0].trail, 2, "the trail bracket moved: {}", back[0].trail);
    assert_eq!(back[0].value.to_bits(), 0, "a compressed float's -0 kept its sign: {:#010x}", back[0].value.to_bits());

    let mut out = vec![0u8; neg_zero_fixed_measure(1)];
    let m = neg_zero_fixed_save(&back, &mut out);
    assert_eq!(m, Some(data.len()), "the save wrote {m:?} bytes, want {}", data.len());
    let mut want = data.clone();
    want[value_at..value_at + 4].fill(0);
    assert_eq!(out, want, "saving the landed +0 did not write the canonical bytes");
`, file)
			out, err := runFixedNegZeroProbe(t, u, body)
			if err != nil {
				t.Fatalf("%s: %v\n%s", vec.File, err, out)
			}
		})
	}

	t.Run("plain_floats_keep_negative_zero", func(t *testing.T) {
		t.Parallel()
		mixed := versionUnit(t, `package probe

fixed table MixedZero
{
    c_sym     float32 | min = -1, max = 1, resolution = 0.01
    c_zero_lo float32 | min = 0, max = 1, resolution = 0.01
    c_zero_hi float32 | min = -1, max = 0, resolution = 0.01
    c_arr     [2]float32 | min = -1, max = 1, resolution = 0.01
    p32       float32
    p64       float64
}
`)
		body := `
    let neg0_32 = f32::from_bits(0x8000_0000);
    let neg0_64 = f64::from_bits(0x8000_0000_0000_0000);
    let row = MixedZeroRow {
        c_sym: 0.0,
        c_zero_lo: 0.0,
        c_zero_hi: 0.0,
        c_arr: [0.0, 0.0],
        p32: neg0_32,
        p64: neg0_64,
    };
    let mut canonical = vec![0u8; mixed_zero_fixed_measure(1)];
    assert_eq!(mixed_zero_fixed_save(&[row], &mut canonical), Some(canonical.len()));

    let layout_bytes = u32::from_le_bytes(
        canonical[TABLE_FIXED_HEADER_BYTES..TABLE_FIXED_HEADER_BYTES + 4]
            .try_into()
            .expect("four bytes"),
    ) as usize;
    let body_at = TABLE_FIXED_HEADER_BYTES + 4 + layout_bytes + 8;
    let mut wire = canonical.clone();
    for off in [0usize, 4, 8, 12, 16] {
        wire[body_at + off..body_at + off + 4].copy_from_slice(&0x8000_0000u32.to_le_bytes());
    }
    assert_eq!(
        u32::from_le_bytes(wire[body_at + 20..body_at + 24].try_into().expect("four bytes")),
        0x8000_0000,
    );
    assert_eq!(
        u64::from_le_bytes(wire[body_at + 24..body_at + 32].try_into().expect("eight bytes")),
        0x8000_0000_0000_0000,
    );

    let mut back = vec![MixedZeroRow::default(); 1];
    let mut plan = vec![TableFixedEntry::default(); 64];
    let mut remap = vec![0u16; 64];
    let mut report = TableFixedReport::default();
    let n = mixed_zero_fixed_load(&mut back, &wire, &mut plan, &mut remap, &mut report);
    assert_eq!(n, Some(1), "the reader refused a mixed -0 record: n={n:?} {report:?}");
    assert_eq!(report, TableFixedReport::default(), "the read reported {report:?}");
    assert_eq!(back[0].c_sym.to_bits(), 0, "c_sym kept -0: {:#010x}", back[0].c_sym.to_bits());
    assert_eq!(back[0].c_zero_lo.to_bits(), 0, "c_zero_lo kept -0: {:#010x}", back[0].c_zero_lo.to_bits());
    assert_eq!(back[0].c_zero_hi.to_bits(), 0, "c_zero_hi kept -0: {:#010x}", back[0].c_zero_hi.to_bits());
    assert_eq!(back[0].c_arr[0].to_bits(), 0, "c_arr[0] kept -0: {:#010x}", back[0].c_arr[0].to_bits());
    assert_eq!(back[0].c_arr[1].to_bits(), 0, "c_arr[1] kept -0: {:#010x}", back[0].c_arr[1].to_bits());
    assert_eq!(back[0].p32.to_bits(), 0x8000_0000, "plain float32 lost -0: {:#010x}", back[0].p32.to_bits());
    assert_eq!(back[0].p64.to_bits(), 0x8000_0000_0000_0000, "plain float64 lost -0: {:#018x}", back[0].p64.to_bits());

    let mut out = vec![0u8; mixed_zero_fixed_measure(1)];
    assert_eq!(mixed_zero_fixed_save(&back, &mut out), Some(canonical.len()));
    assert_eq!(out, canonical, "saving the landed record did not keep plain -0 and canonicalize compressed +0");
`
		out, err := runFixedNegZeroProbe(t, mixed, body)
		if err != nil {
			t.Fatalf("plain_floats_keep_negative_zero: %v\n%s", err, out)
		}
	})
}

func runFixedNegZeroProbe(t *testing.T, u *ir.Unit, body string) (string, error) {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	var mods []string
	for name, data := range files {
		keep := name == BuildVersionModule+".rs" || name == FixedRuntimeModule+".rs" ||
			name == TableEnumsModule+".rs" ||
			strings.HasSuffix(name, "_records.rs") || strings.HasSuffix(name, "_fixed.rs")
		if !keep {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		mods = append(mods, strings.TrimSuffix(name, ".rs"))
	}
	sort.Strings(mods)
	var main strings.Builder
	main.WriteString("#![allow(dead_code, unused_imports)]\n")
	for _, m := range mods {
		fmt.Fprintf(&main, "mod %s;\npub use %s::*;\n", m, m)
	}
	main.WriteString("\nfn main() {\n")
	main.WriteString(body)
	main.WriteString("}\n")
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	build := exec.Command("rustc", "--edition", "2024", "-D", "warnings", "-o", bin, "main.rs")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the probe did not build: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	return string(out), err
}
