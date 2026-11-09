package goosetable

// THE STRICT PROOF SUITE ORACLE (docs/FIXED-FORM-ALGORITHM.md §4.5, §4.6, §5.3;
// ROADMAP.md, Fixed Tables): the Goose fixed-table proof suite cross-checks
// every generated Goose FixedSave and FixedLoad against a live C++ reference
// oracle compiled from cpp.Generate + cpptable.GenerateLineage. After the
// strict cut removed the tolerant TableReport counters (clamped, kind_mismatch,
// duplicate, malformed), the C++ oracle must hold the strict verdict
// (r.unknown || r.widened || r.damaged || r.refused) on both its self-load and
// its candidate verification, and both sides must refuse out-of-range scalars,
// forged ordinals, and non-0/1 bool or present bytes.

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpptable"
)

const strictProofOracleSchema = `package probe
enum Mode { Idle, Active, Fault }
union Choice {
    a int32
    b int32
}
fixed table R0 {
    x    int32 = 1 | min = -10, max = 10
    mode Mode = Idle
    ch   Choice
    flag bool = false
    note ?int32 | min = 0, max = 100
}
`

// strictOracleProblems reports every way the C++ fixed proof oracle source
// departs from the strict TableReport verdict. Empty means the oracle source
// holds the strict rule.
func strictOracleProblems(src string) []string {
	var bad []string
	const wantCheck = "r.unknown || r.widened || r.damaged || r.refused"
	if got := strings.Count(src, wantCheck); got != 2 {
		bad = append(bad, "oracle source must check `"+wantCheck+"` in both self-load and candidate-verify branches")
	}
	for _, retired := range []string{"r.clamped", "r.kind_mismatch", "r.duplicate", "r.malformed"} {
		if strings.Contains(src, retired) {
			bad = append(bad, "oracle source still references retired TableReport field "+retired)
		}
	}
	return bad
}

func writeCppOracleHeaders(t *testing.T, schemaText string) (string, string) {
	t.Helper()
	u := parseCheckUnit(t, schemaText)
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatalf("cpp.Generate: %v", err)
	}
	tables, err := cpptable.GenerateLineage(u, nil)
	if err != nil {
		t.Fatalf("cpptable.GenerateLineage: %v", err)
	}
	maps.Copy(files, tables)

	outDir := t.TempDir()
	var tableHeader string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(outDir, name), data, 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		if strings.HasSuffix(name, "Table.h") {
			tableHeader = name
		}
	}
	if tableHeader == "" {
		t.Fatal("no Table.h generated")
	}
	return outDir, tableHeader
}

// TestGooseFixedProofSuiteStrict pins the C++ fixed proof oracle to the strict
// TableReport verdict and proves bidirectional C++ and Goose agreement on valid
// records and strict refusal on out-of-range scalars, forged ordinals, and
// wire-damaged bool/present bytes.
func TestGooseFixedProofSuiteStrict(t *testing.T) {
	t.Parallel()
	const cppSetup = `    value[0].x = 7;
    value[0].mode = Mode::Active;
    value[0].ch.type = ChoiceType::A;
    value[0].ch.a = 19;
    value[0].flag = true;
    value[0].note_present = true;
    value[0].note = 42;
`
	src := cppFixedOracleSource("R0", 1, cppSetup, "ProbeTable.h")
	for _, b := range strictOracleProblems(src) {
		t.Error(b)
	}

	u := parseCheckUnit(t, strictProofOracleSchema)
	cppBytes, verifyCpp := buildAndRunCppFixedOracle(t, "strict-oracle", u, "R0", 1, cppSetup)

	gooseMain := withGooseCrossCheck(`fn main() {
    var rec = probe::R0 {
        X: 7,
        Mode: 2,
        Ch: probe::Choice { Type: 1, A: 19, B: 0 },
        Flag: true,
        Note: 42,
        NotePresent: true,
    };
    var out: u8[>..] = [];
    let w = probe::R0FixedSave([rec], out);
    var back: probe::R0[1] = [probe::R0 { .. }];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::R0FixedLoad(back, out, rep);
    print(w == out.len && n == 1 && rep.reason == 0 && rep.oor == 0 && back[0].X == 7 && back[0].Mode == 2 && back[0].Ch.Type == 1 && back[0].Ch.A == 19 && back[0].Flag && back[0].NotePresent && back[0].Note == 42);
}`, "R0", 1)

	rawOut := runCustomFixedGoose(t, u, nil, gooseMain, map[string][]byte{"cpp.bin": cppBytes})
	lines := strings.Split(strings.TrimSpace(rawOut), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 output lines, got %d:\n%s", len(lines), rawOut)
	}
	if got := strings.TrimSpace(lines[0]); got != "true" {
		t.Fatalf("Goose self-load check failed: %q", got)
	}
	gooseBytes, err := hex.DecodeString(strings.TrimSpace(lines[1]))
	if err != nil {
		t.Fatalf("decode goose hex: %v", err)
	}
	if !bytes.Equal(gooseBytes, cppBytes) {
		t.Fatalf("Goose FixedSave bytes != C++ reference FixedSave bytes:\ngoose=%x\ncpp  =%x", gooseBytes, cppBytes)
	}
	if crossOk := strings.TrimSpace(lines[2]); crossOk != "true" {
		t.Fatalf("Goose FixedLoad of C++ reference bytes failed: %s", crossOk)
	}
	reSavedBytes, err := hex.DecodeString(strings.TrimSpace(lines[3]))
	if err != nil {
		t.Fatalf("decode goose re-saved hex: %v", err)
	}
	if !bytes.Equal(reSavedBytes, cppBytes) {
		t.Fatalf("Goose re-save after loading C++ bytes differed:\ngot =%x\nwant=%x", reSavedBytes, cppBytes)
	}
	if err := verifyCpp(gooseBytes); err != nil {
		t.Fatalf("C++ oracle rejected clean Goose FixedSave bytes: %v", err)
	}

	// Record body starts after 20-byte header + layoutLen + 8-byte record hash.
	layoutLen := int(binary.LittleEndian.Uint32(cppBytes[16:20]))
	bodyAt := 20 + layoutLen + 8
	// R0 body layout:
	// x: +0 (4B), mode: +4 (1B), ch: +5 (1B tag + 4B arm = 5B), flag: +10 (1B),
	// note_present: +11 (1B), note: +12 (4B).
	for _, tc := range []struct {
		name   string
		mutate func([]byte)
	}{
		{
			name: "ranged-scalar-out-of-range",
			mutate: func(b []byte) {
				binary.LittleEndian.PutUint32(b[bodyAt+0:], 99)
			},
		},
		{
			name: "enum-ordinal-forged",
			mutate: func(b []byte) {
				b[bodyAt+4] = 99
			},
		},
		{
			name: "union-tag-forged",
			mutate: func(b []byte) {
				b[bodyAt+5] = 99
			},
		},
		{
			name: "bool-byte-damaged",
			mutate: func(b []byte) {
				b[bodyAt+10] = 2
			},
		},
		{
			name: "optional-present-damaged",
			mutate: func(b []byte) {
				b[bodyAt+11] = 2
			},
		},
	} {
		corrupted := bytes.Clone(cppBytes)
		tc.mutate(corrupted)
		if err := verifyCpp(corrupted); err == nil {
			t.Fatalf("%s: C++ strict oracle accepted corrupted candidate bytes", tc.name)
		}
	}
}

// TestGooseFixedProofSuiteStrictNegativeControl plants the retired TableReport
// fields and a permissive candidate check into the real C++ oracle artifact
// and requires both the static strict check and the oracle compilation/runtime
// verification to fail.
func TestGooseFixedProofSuiteStrictNegativeControl(t *testing.T) {
	t.Parallel()
	const cppSetup = `    value[0].x = 7;
    value[0].mode = Mode::Active;
    value[0].ch.type = ChoiceType::A;
    value[0].ch.a = 19;
    value[0].flag = true;
    value[0].note_present = true;
    value[0].note = 42;
`
	outDir, tableHeader := writeCppOracleHeaders(t, strictProofOracleSchema)
	cleanSrc := cppFixedOracleSource("R0", 1, cppSetup, tableHeader)
	if probs := strictOracleProblems(cleanSrc); len(probs) != 0 {
		t.Fatalf("clean oracle source failed strict check: %v", probs)
	}

	// Plant 1: restore the retired TableReport fields that broke the oracle at
	// the strict cut. Both the static check and the C++ compile must fail.
	retiredSrc := strings.ReplaceAll(
		cleanSrc,
		"r.unknown || r.widened || r.damaged || r.refused",
		"r.clamped || r.unknown || r.kind_mismatch || r.widened || r.duplicate || r.malformed || r.refused",
	)
	if retiredSrc == cleanSrc {
		t.Fatal("negative control: strict verdict expression not found in oracle source")
	}
	if len(strictOracleProblems(retiredSrc)) == 0 {
		t.Fatal("negative control: restoring retired TableReport fields did not trigger strictOracleProblems")
	}
	if _, _, err := compileAndRunCppFixedOracleFiles(t, "retired-fields", outDir, retiredSrc); err == nil {
		t.Fatal("negative control: C++ oracle with retired TableReport fields compiled against strict Table.h")
	}

	// Plant 2: weaken the candidate-verify branch in the C++ oracle so it ignores
	// FixedLoad refusals/damage and candidate byte differences. Both the static
	// check and the corrupted-candidate rejection check must catch the plant.
	permissiveSrc := strings.Replace(
		cleanSrc,
		"if ( cn != 1 || r.unknown || r.widened || r.damaged || r.refused )",
		"if ( false )",
		1,
	)
	permissiveSrc = strings.Replace(
		permissiveSrc,
		"if ( re_saved != saved || memcmp( re_buf, buffer, (size_t) saved ) != 0 )",
		"if ( false )",
		1,
	)
	permissiveSrc = strings.Replace(
		permissiveSrc,
		"if ( clen != saved || memcmp( cand_buf, buffer, (size_t) saved ) != 0 )",
		"if ( false )",
		1,
	)
	if len(strictOracleProblems(permissiveSrc)) == 0 {
		t.Fatal("negative control: removing candidate strict verdict check did not trigger strictOracleProblems")
	}
	cppBytes, permissiveVerify, err := compileAndRunCppFixedOracleFiles(t, "permissive-oracle", outDir, permissiveSrc)
	if err != nil {
		t.Fatalf("negative control: compile permissive oracle: %v", err)
	}
	layoutLen := int(binary.LittleEndian.Uint32(cppBytes[16:20]))
	corrupted := bytes.Clone(cppBytes)
	binary.LittleEndian.PutUint32(corrupted[20+layoutLen+8:], 99)
	if err := permissiveVerify(corrupted); err != nil {
		t.Fatalf("negative control: permissive oracle unexpectedly rejected out-of-range candidate: %v", err)
	}
}
