package rusttable

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const idTableBoundProbeSchema = `package probe

table Packet
{
    x    uint32
    next *Packet
}
`

func appendLeb(dst []byte, v uint64) []byte {
	for {
		b := uint8(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		dst = append(dst, b)
		if v == 0 {
			return dst
		}
	}
}

// buildUnknownFieldsWire constructs a form-1 wire whose trailer holds ids and,
// when refAll is true, whose root body references every entry 1..len(ids) as a
// 1-byte kind-0 (bool) field with value 0 before the 0x00 root terminator.
func buildUnknownFieldsWire(ids []uint64, refAll bool) []byte {
	var wire []byte
	wire = append(wire, ir.TableWireForm)
	if refAll {
		for i := range ids {
			wire = appendLeb(wire, uint64(i+1))
			wire = append(wire, ir.TableKindBool, 0) // kind 1 (bool), value 0
		}
	}
	wire = append(wire, 0) // root terminator
	for _, id := range ids {
		wire = binary.LittleEndian.AppendUint64(wire, id)
	}
	wire = binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
	return wire
}

func extractRustFn(src, sig string) (string, error) {
	start := strings.Index(src, sig)
	if start < 0 {
		return "", fmt.Errorf("missing function %q", sig)
	}
	rest := src[start:]
	end := strings.Index(rest, "\n}\n")
	if end < 0 {
		return "", fmt.Errorf("unterminated function %q", sig)
	}
	return rest[:end+3], nil
}

// TestRustIdTableReaderContract pins docs/SPEC-TABLES.md §3 for the Rust
// variable-form reader (<name>_load_inner and <name>_load_measure):
//   - an id table with count > 4096 (TABLE_PROBE_BOUND) is refused as
//     malformed before allocating or walking entries;
//   - distinctness is checked in an 8192-slot (TABLE_PROBE_SLOTS) stack probe
//     array with multiplicative hash (id * 0x9e3779b97f4a7c15) >> 51, with
//     neither Vec::with_capacity(count) nor O(count^2) ids.contains(&id).
func TestRustIdTableReaderContract(t *testing.T) {
	t.Parallel()

	u := unitFrom(t, idTableBoundProbeSchema)
	modules, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	tableRS, ok := modules["probe_table.rs"]
	if !ok {
		t.Fatalf("missing probe_table.rs in generated modules")
	}
	src := string(tableRS)

	for _, decl := range []string{
		"const TABLE_PROBE_BOUND: usize = 4096;",
		"const TABLE_PROBE_SLOTS: usize = 8192;",
	} {
		if !strings.Contains(src, decl) {
			t.Fatalf("probe_table.rs is missing %q", decl)
		}
	}

	for _, fnSig := range []string{
		"fn packet_load_inner(",
		"pub fn packet_load_measure(",
	} {
		body, err := extractRustFn(src, fnSig)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"Vec::with_capacity(count)", "ids.contains"} {
			if strings.Contains(body, forbidden) {
				t.Errorf("%s still contains %q", fnSig, forbidden)
			}
		}
		boundIdx := strings.Index(body, "if count > TABLE_PROBE_BOUND")
		if boundIdx < 0 {
			t.Fatalf("%s is missing the TABLE_PROBE_BOUND refusal", fnSig)
		}
		probeIdx := strings.Index(body, "let mut probe_slots = [0u64; TABLE_PROBE_SLOTS];")
		if probeIdx < 0 {
			t.Fatalf("%s is missing the TABLE_PROBE_SLOTS fixed probe array", fnSig)
		}
		hashIdx := strings.Index(body, "0x9e3779b97f4a7c15")
		if hashIdx < 0 {
			t.Fatalf("%s is missing the multiplicative probe hash", fnSig)
		}
		if boundIdx > probeIdx || boundIdx > hashIdx {
			t.Errorf("%s checks TABLE_PROBE_BOUND after the probe walk", fnSig)
		}
	}

	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}

	// (a) 4097 distinct unknown ids (32,784-byte trailer): refused as malformed.
	ids4097 := make([]uint64, 4097)
	for i := range ids4097 {
		ids4097[i] = 0x1000_0000_0000_0000 + uint64(i+1)
	}
	wire4097 := buildUnknownFieldsWire(ids4097, false)
	if gotTrailer := len(wire4097) - 2; gotTrailer != 32784 {
		t.Fatalf("expected 32784-byte trailer for 4097 ids, got %d", gotTrailer)
	}

	// (b) 4096 distinct ids unknown to the reader, each referenced in the root
	// body so all 4096 entries are walked and resolved.
	ids4096 := make([]uint64, 4096)
	for i := range ids4096 {
		ids4096[i] = 0x2000_0000_0000_0000 + uint64(i+1)
	}
	wire4096 := buildUnknownFieldsWire(ids4096, true)

	// (c) One id carried twice (both non-zero and zero duplicates).
	wireDup := buildUnknownFieldsWire([]uint64{0x3000_0000_0000_0001, 0x3000_0000_0000_0001}, false)
	wireZeroDup := buildUnknownFieldsWire([]uint64{0, 0}, false)

	// (d) Distinct ids chosen to collide in the 8192-slot multiplicative hash
	// ((id * 0x9e3779b97f4a7c15) >> 51), including id == 0 (slot 0) and 15
	// non-zero ids that also hash to slot 0.
	colliding := []uint64{0}
	for cand := uint64(1); len(colliding) < 16; cand++ {
		if ((cand * 0x9e3779b97f4a7c15) >> 51) == 0 {
			colliding = append(colliding, cand)
		}
	}
	wireCollide := buildUnknownFieldsWire(colliding, true)

	// Verify (b), (c), and (d) against the Go oracle (tablewire.DecodeRegion
	// and tablewire.FileRegionMeasure) first.
	m := tabletext.NewModel(u)
	root := m.Lookup("Packet")

	checkOracle := func(name string, wire []byte, wantOK bool, wantMalformed bool, wantUnknown int) int64 {
		t.Helper()
		inst := m.New(root)
		var rep tabletext.Report
		ok, err := tablewire.DecodeRegion(m, inst, wire, &rep)
		if err != nil {
			t.Fatalf("%s: unexpected DecodeRegion error: %v", name, err)
		}
		if ok != wantOK || rep.Malformed != wantMalformed || rep.Unknown != wantUnknown {
			t.Fatalf("%s: DecodeRegion = (ok=%v, rep=%+v), want (ok=%v, malformed=%v, unknown=%d)",
				name, ok, rep, wantOK, wantMalformed, wantUnknown)
		}
		bytes, whole, refused := tablewire.FileRegionMeasure(u, root, wire)
		if !wantOK {
			if whole && !refused {
				t.Fatalf("%s: FileRegionMeasure unexpectedly accepted malformed wire (bytes=%d)", name, bytes)
			}
			return -1
		}
		if !whole || refused || bytes <= 0 {
			t.Fatalf("%s: FileRegionMeasure = (bytes=%d, whole=%v, refused=%v), want accepted", name, bytes, whole, refused)
		}
		return bytes
	}

	measure4096 := checkOracle("4096-unknown", wire4096, true, false, 4096)
	checkOracle("dup-nonzero", wireDup, false, true, 0)
	checkOracle("dup-zero", wireZeroDup, false, true, 0)
	measureCollide := checkOracle("collide-slot0", wireCollide, true, false, len(colliding))

	work := t.TempDir()
	for _, name := range []string{"variable_runtime.rs", "probe_table.rs", "probe_variable.rs"} {
		data, ok := modules[name]
		if !ok {
			t.Fatalf("missing module %s", name)
		}
		if err := os.WriteFile(filepath.Join(work, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range map[string][]byte{
		"w4097.wire":    wire4097,
		"w4096.wire":    wire4096,
		"wdup.wire":     wireDup,
		"wzerodup.wire": wireZeroDup,
		"wcollide.wire": wireCollide,
	} {
		if err := os.WriteFile(filepath.Join(work, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	probeMain := fmt.Sprintf(idTableBoundRustMain, measure4096, len(colliding), measureCollide)
	if err := os.WriteFile(filepath.Join(work, "main.rs"), []byte(probeMain), 0o600); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(work, "idtable_probe")
	cmd := exec.Command("rustc", "--edition", "2024", "-O", "-o", bin, "main.rs")
	cmd.Dir = work
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rustc failed: %v\n%s", err, out)
	}
	run := exec.Command(bin)
	run.Dir = work
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("idtable_probe failed: %v\n%s", err, out)
	} else if !strings.Contains(string(out), "ID TABLE CONTRACT OK") {
		t.Fatalf("unexpected probe output:\n%s", out)
	}

	// Negative control: in a temporary copy inside t.TempDir(), strip the
	// TABLE_PROBE_BOUND refusal from probe_table.rs and verify the 4097-id
	// check fails as expected.
	negDir := t.TempDir()
	sabotaged := strings.ReplaceAll(src,
		"if count > TABLE_PROBE_BOUND { report.malformed = true; return None; }",
		"if false && count > TABLE_PROBE_BOUND { report.malformed = true; return None; }",
	)
	sabotaged = strings.ReplaceAll(sabotaged,
		"if count > TABLE_PROBE_BOUND { return -1; }",
		"if false && count > TABLE_PROBE_BOUND { return -1; }",
	)
	if sabotaged == src {
		t.Fatal("negative control did not modify probe_table.rs")
	}
	for name, data := range map[string][]byte{
		"variable_runtime.rs": modules["variable_runtime.rs"],
		"probe_table.rs":      []byte(sabotaged),
		"probe_variable.rs":   modules["probe_variable.rs"],
		"w4097.wire":          wire4097,
		"w4096.wire":          wire4096,
		"wdup.wire":           wireDup,
		"wzerodup.wire":       wireZeroDup,
		"wcollide.wire":       wireCollide,
		"main.rs":             []byte(probeMain),
	} {
		if err := os.WriteFile(filepath.Join(negDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	negBin := filepath.Join(negDir, "idtable_probe_neg")
	negBuild := exec.Command("rustc", "--edition", "2024", "-O", "-o", negBin, "main.rs")
	negBuild.Dir = negDir
	if out, err := negBuild.CombinedOutput(); err != nil {
		t.Fatalf("negative control build failed: %v\n%s", err, out)
	}
	negRun := exec.Command(negBin)
	negRun.Dir = negDir
	if out, err := negRun.CombinedOutput(); err == nil {
		t.Fatalf("negative control unexpectedly succeeded with TABLE_PROBE_BOUND stripped:\n%s", out)
	} else if !strings.Contains(string(out), "4097-id wire must be refused") {
		t.Fatalf("negative control failed for unexpected reason:\n%s", out)
	}
}

const idTableBoundRustMain = `
mod variable_runtime;
mod probe_table;
mod probe_variable;

use probe_table::*;
use variable_runtime::*;

fn assert_clean_report(rep: &TableReport, want_unknown: u64) {
    assert!(!rep.malformed, "unexpected malformed: {:?}", rep);
    assert!(!rep.refused, "unexpected refused: {:?}", rep);
    assert_eq!(rep.unknown, want_unknown, "unknown mismatch: {:?}", rep);
    assert_eq!(rep.kind_mismatch, 0, "kind_mismatch: {:?}", rep);
    assert_eq!(rep.clamped, 0, "clamped: {:?}", rep);
    assert_eq!(rep.widened, 0, "widened: {:?}", rep);
}

fn main() {
    // (a) 4097 distinct ids: refused with report.malformed == true and measure == -1.
    let w4097 = std::fs::read("w4097.wire").expect("read w4097.wire");
    let mut rep4097 = TableReport::default();
    let got4097 = packet_load(&w4097, &mut rep4097);
    assert!(got4097.is_none(), "4097-id wire must be refused");
    assert!(rep4097.malformed, "4097-id wire must set report.malformed");
    assert_eq!(packet_load_measure(&w4097), -1, "4097-id wire measure must be -1");

    // (b) 4096 distinct unknown ids: accepted and matches Go oracle.
    let w4096 = std::fs::read("w4096.wire").expect("read w4096.wire");
    let mut rep4096 = TableReport::default();
    let got4096 = packet_load(&w4096, &mut rep4096);
    assert!(got4096.is_some(), "4096-id wire must be accepted");
    assert_clean_report(&rep4096, 4096);
    assert_eq!(packet_load_measure(&w4096), %d, "4096-id wire measure must match FileRegionMeasure");

    // (c) Duplicate id (both non-zero and zero): refused as malformed and measure == -1.
    for path in ["wdup.wire", "wzerodup.wire"] {
        let wdup = std::fs::read(path).expect("read dup wire");
        let mut rep_dup = TableReport::default();
        assert!(packet_load(&wdup, &mut rep_dup).is_none(), "{path} must be refused");
        assert!(rep_dup.malformed, "{path} must set report.malformed");
        assert_eq!(packet_load_measure(&wdup), -1, "{path} measure must be -1");
    }

    // (d) Colliding distinct ids (all hashing to slot 0, including id == 0): accepted.
    let wcollide = std::fs::read("wcollide.wire").expect("read wcollide.wire");
    let mut rep_collide = TableReport::default();
    let got_collide = packet_load(&wcollide, &mut rep_collide);
    assert!(got_collide.is_some(), "colliding distinct ids must be accepted");
    assert_clean_report(&rep_collide, %d);
    assert_eq!(packet_load_measure(&wcollide), %d, "colliding wire measure must match FileRegionMeasure");

    println!("ID TABLE CONTRACT OK");
}
`
