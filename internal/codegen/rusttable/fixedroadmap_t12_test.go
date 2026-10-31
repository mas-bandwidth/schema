package rusttable

// THE ROADMAP'S FRAMING, SHARED BYTE ORACLE AND HOSTILE-INPUT TASKS ON THE RUST
// LEG (docs/roadmap.sexp node `fixed-tables`, leaves `rust/F4`, `rust/W13`,
// `rust/P2`, `rust/R27`, `rust/P3`, `rust/P4`; docs/SPEC-TABLES.md §3.4 and
// docs/FIXED-FORM-ALGORITHM.md §4.1, §4.4, §5.3, §5.7, §5.9, §7).
//
// Every subtest here is gated on the C++ reference's byte oracle in
// `build/fixedform-corpus` (built by `make tables-fixedform-corpus`): when the
// corpus directory is absent and SCHEMA_REQUIRE_CORPUS is unset, `versionCorpus`
// skips by name; when SCHEMA_REQUIRE_CORPUS=1 (as under `make
// tables-rust-versioning`), a missing corpus or toolchain fails by name.
//
// `rust/F7` ("malformed, under 20 bytes") is already pinned by
// `TestFixedFormUnder20Bytes` in `fixedform_under_20_bytes_test.go` and is
// cited directly in `docs/roadmap-evidence/fixed-rust.sexp`.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedRoadmapRustT12FramingText(t *testing.T) {
	t.Parallel()
	corpus := versionCorpus(t)
	t12RequireRustToolchain(t)

	t.Run("F4", func(t *testing.T) {
		t.Parallel()
		t12F4(t, corpus)
	})
	t.Run("W13", func(t *testing.T) {
		t.Parallel()
		t12W13(t, corpus)
	})
	t.Run("P2", func(t *testing.T) {
		t.Parallel()
		t12P2(t, corpus)
	})
	t.Run("R27", func(t *testing.T) {
		t.Parallel()
		t12R27(t, corpus)
	})
	t.Run("P3", func(t *testing.T) {
		t.Parallel()
		t12P3(t, corpus)
	})
	t.Run("P4", func(t *testing.T) {
		t.Parallel()
		t12P4(t, corpus)
	})
}

// t12RequireRustToolchain gates the functional probe on rustc and cargo using
// the same SCHEMA_REQUIRE_CORPUS contract as `versionCorpus`.
func t12RequireRustToolchain(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"rustc", "cargo"} {
		if _, err := exec.LookPath(bin); err != nil {
			if os.Getenv("SCHEMA_REQUIRE_CORPUS") == "1" {
				t.Fatalf("SCHEMA_REQUIRE_CORPUS=1, and %s is not on PATH: %v", bin, err)
			}
			t.Skipf("%s is not on PATH: %v", bin, err)
		}
	}
}

// ---- rust/F4: layout_malformed, truncated (§5.3 row 3) ----------------------

// t12F4 holds rust/F4 to docs/FIXED-FORM-ALGORITHM.md §5.3 step 3 (row 3 of the
// refusal table): when `20 + L` reaches past the end of the file, the reader
// refuses `layout_malformed` before any record, with `malformed` false,
// `layout_hash` zero, every counter zero, and the `0x5A`-stained destination
// untouched.
func t12F4(t *testing.T, corpus string) {
	out := t12ProbeRun(t, corpus)
	if line := t12Line(out, "F4"); line != "T12 F4 ok" {
		t.Fatalf("rust/F4: truncated layout was not refused as layout_malformed: %s", line)
	}
}

// ---- rust/W13: layout+hash == C++ reference (§3.4, §5.1, §5.9 #12) ----------

// t12W13 holds rust/W13 to docs/SPEC-TABLES.md §3.4 and
// docs/FIXED-FORM-ALGORITHM.md §5.1 / §5.9 #12: the layout block and 64-bit
// FNV-1a wire hash emitted on the Rust leg equal the bytes the C++ reference
// writes at `8..16` and `20..20+L` of every corpus file in
// `build/fixedform-corpus`.
func t12W13(t *testing.T, corpus string) {
	type standalone struct {
		schema string
		bin    string
	}
	standalones := []standalone{
		{schema: "FX1", bin: "fx1.bin"},
		{schema: "FX2", bin: "fx2.bin"},
		{schema: "P1", bin: "p1.bin"},
		{schema: "P3", bin: "p3.bin"},
	}
	checked := 0
	for _, s := range standalones {
		u := versionUnit(t, versionSchema(t, s.schema))
		root := versionRoot(t, u)
		t12AssertLayoutAndHashMatchBin(t, u, root, filepath.Join(corpus, s.bin), true)
		checked++
	}
	for _, r := range versionRows {
		if r.unported != "" {
			continue
		}
		uOld := versionUnit(t, versionSchema(t, "VOLD_"+r.row))
		t12AssertLayoutAndHashMatchBin(t, uOld, versionRoot(t, uOld), filepath.Join(corpus, "old_"+r.row+".bin"), checked < 8)
		checked++

		uNew := versionUnit(t, versionSchema(t, "VNEW_"+r.row))
		t12AssertLayoutAndHashMatchBin(t, uNew, versionRoot(t, uNew), filepath.Join(corpus, "new_"+r.row+".bin"), checked < 10)
		checked++
	}
	if checked < 54 {
		t.Fatalf("rust/W13: compared only %d corpus files against the C++ reference", checked)
	}
	out := t12ProbeRun(t, corpus)
	if line := t12Line(out, "W13"); line != "T12 W13 ok" {
		t.Fatalf("rust/W13: runtime layout/hash probe failed: %s", line)
	}
}

func t12AssertLayoutAndHashMatchBin(t *testing.T, u *ir.Unit, root *ir.Struct, binPath string, checkEmittedRust bool) {
	t.Helper()
	data, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("rust/W13: cannot read C++ reference file %s: %v", binPath, err)
	}
	if len(data) < 20 {
		t.Fatalf("rust/W13: %s is shorter than the 20-byte fixed header (%d bytes)", binPath, len(data))
	}
	refHash := binary.LittleEndian.Uint64(data[8:16])
	refL := int(binary.LittleEndian.Uint32(data[16:20]))
	if 20+refL > len(data) {
		t.Fatalf("rust/W13: %s header layout length %d exceeds file length %d", binPath, refL, len(data))
	}
	refLayout := data[20 : 20+refL]

	entry, ok := FixedLineageOf(u, root.Name)
	if !ok {
		t.Fatalf("rust/W13: FixedLineageOf(%s) returned false for %s", root.Name, binPath)
	}
	if entry.Wire != refHash {
		t.Fatalf("rust/W13: %s (%s) hash = 0x%016x, C++ reference = 0x%016x",
			filepath.Base(binPath), root.Name, entry.Wire, refHash)
	}
	if !bytes.Equal(entry.Layout, refLayout) {
		t.Fatalf("rust/W13: %s (%s) layout (%d bytes) != C++ reference (%d bytes)",
			filepath.Base(binPath), root.Name, len(entry.Layout), len(refLayout))
	}

	if checkEmittedRust {
		files, err := GenerateLineage(u, nil)
		if err != nil {
			t.Fatalf("rust/W13: GenerateLineage(%s): %v", filepath.Base(binPath), err)
		}
		snake := ir.RustSnake(root.Name)
		upper := strings.ToUpper(snake)
		src := t08Fixed(t, files)
		wantHash := fmt.Sprintf("pub const %s_FIXED_HASH: u64 = 0x%016x;", upper, refHash)
		if !strings.Contains(src, wantHash) {
			t.Fatalf("rust/W13: *_fixed.rs for %s does not carry %q", filepath.Base(binPath), wantHash)
		}
		emittedBlock := t12ParseEmittedRustBlock(t, src, fmt.Sprintf("pub const %s_FIXED_BLOCK: [u8; %d] = [", upper, len(refLayout)))
		if !bytes.Equal(emittedBlock, refLayout) {
			t.Fatalf("rust/W13: %s_FIXED_BLOCK bytes != C++ reference layout in %s", upper, filepath.Base(binPath))
		}
	}
}

func t12ParseEmittedRustBlock(t *testing.T, src, marker string) []byte {
	t.Helper()
	at := strings.Index(src, marker)
	if at < 0 {
		t.Fatalf("rust/W13: marker %q not found in emitted Rust source", marker)
	}
	rest := src[at+len(marker):]
	end := strings.Index(rest, "];")
	if end < 0 {
		t.Fatalf("rust/W13: closing `];` not found after %q", marker)
	}
	rest = rest[:end]
	var out []byte
	for tok := range strings.SplitSeq(rest, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		v, err := strconv.ParseUint(strings.TrimPrefix(tok, "0x"), 16, 8)
		if err != nil {
			t.Fatalf("rust/W13: invalid byte token %q after %q: %v", tok, marker, err)
		}
		out = append(out, byte(v))
	}
	return out
}

// ---- rust/P2: write-read-write byte-identical (§3.4, §5.2) ------------------

// t12P2 holds rust/P2 to docs/SPEC-TABLES.md §3.4 ("every slack byte in the
// written image is zero") and docs/FIXED-FORM-ALGORITHM.md §5.2: a C++
// reference file read into a `0x5A`-stained destination, saved, read again, and
// saved a second time is byte-for-byte identical to the reference and across
// both saves, on both the identity plan and a compiled lineage plan.
func t12P2(t *testing.T, corpus string) {
	out := t12ProbeRun(t, corpus)
	if line := t12Line(out, "P2"); line != "T12 P2 ok" {
		t.Fatalf("rust/P2: write-read-write was not byte-identical: %s", line)
	}
}

// ---- rust/R27: the manifest is the wire (§5.7, §5.9 #32, #37) ---------------

// t12R27 holds rust/R27 to docs/FIXED-FORM-ALGORITHM.md §5.7 step 1 and §5.9
// #32 / #37: `build/fixedform-corpus/manifest.txt` is the single value oracle
// written by the C++ reference dump, every corpus file in it carries a positive
// record count and named values whose record framing matches the `.bin` on
// disk, every bracketed versioning row's `r0.lead` and `r0.trail` match the
// wire's first and last 4 bytes, and the Rust probe reads back the exact values
// the manifest records.
func t12R27(t *testing.T, corpus string) {
	manifestPath := filepath.Join(corpus, "manifest.txt")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("rust/R27: cannot read %s: %v", manifestPath, err)
	}
	lines := 0
	seenFiles := map[string]string{}
	for line := range strings.SplitSeq(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines++
		var file, values string
		records := -1
		for field := range strings.FieldsSeq(line) {
			k, v, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			switch k {
			case "file":
				file = v
			case "records":
				n, err := strconv.Atoi(v)
				if err != nil {
					t.Fatalf("rust/R27: manifest line %d has bad records=%q: %v", lines, v, err)
				}
				records = n
			case "values":
				values = v
			}
		}
		if file == "" || records < 1 || values == "" {
			t.Fatalf("rust/R27: manifest line %d is incomplete: file=%q records=%d values=%q", lines, file, records, values)
		}
		seenFiles[file] = fmt.Sprintf("records=%d values=%s", records, values)
		binData, err := os.ReadFile(filepath.Join(corpus, file))
		if err != nil {
			t.Fatalf("rust/R27: manifest names %s, which is not readable: %v", file, err)
		}
		if len(binData) < 20 {
			t.Fatalf("rust/R27: %s is under 20 bytes (%d)", file, len(binData))
		}
		L := int(binary.LittleEndian.Uint32(binData[16:20]))
		rem := len(binData) - (20 + L)
		if rem <= 0 || rem%records != 0 {
			t.Fatalf("rust/R27: %s has %d bytes after 20+L=%d, not a positive multiple of records=%d",
				file, rem, 20+L, records)
		}
	}
	if lines < 73 {
		t.Fatalf("rust/R27: manifest.txt has %d rows, want at least 73", lines)
	}
	for _, r := range versionRows {
		if r.unported != "" {
			continue
		}
		for _, prefix := range []string{"old_", "new_"} {
			file := prefix + r.row + ".bin"
			if _, ok := seenFiles[file]; !ok {
				t.Fatalf("rust/R27: manifest.txt is missing %s", file)
			}
			lead, trail, ok := versionManifestBrackets(t, corpus, file)
			if !ok {
				continue
			}
			binData, err := os.ReadFile(filepath.Join(corpus, file))
			if err != nil {
				t.Fatalf("rust/R27: cannot read %s: %v", file, err)
			}
			at := 20 + int(binary.LittleEndian.Uint32(binData[16:20])) + 8
			L := int(binary.LittleEndian.Uint32(binData[16:20]))
			recCount := 1
			for field := range strings.FieldsSeq(seenFiles[file]) {
				if v, ok := strings.CutPrefix(field, "records="); ok {
					recCount, _ = strconv.Atoi(v)
				}
			}
			recBytes := (len(binData) - (20 + L)) / recCount
			bodyBytes := recBytes - 8
			body := binData[at : at+bodyBytes]
			gotLead := binary.LittleEndian.Uint32(body[:4])
			gotTrail := binary.LittleEndian.Uint32(body[len(body)-4:])
			if gotLead != lead || gotTrail != trail {
				t.Fatalf("rust/R27: %s manifest brackets (%d, %d) != wire bytes (%d, %d)",
					file, lead, trail, gotLead, gotTrail)
			}
		}
	}
	// Verify the Rust probe reads back the exact values recorded in manifest.txt
	// for old_field_append.bin, new_field_append.bin, old_int_widen.bin, and
	// new_int_widen.bin.
	pOut := t12ProbeRun(t, corpus)
	if line := t12Line(pOut, "R27"); line != "T12 R27 ok" {
		t.Fatalf("rust/R27: manifest value read-back probe failed: %s", line)
	}
	for _, file := range []string{"old_field_append.bin", "new_field_append.bin", "old_int_widen.bin", "new_int_widen.bin"} {
		want := "T12 R27-FILE " + file + " " + seenFiles[file]
		if !strings.Contains(pOut, want+"\n") {
			t.Fatalf("rust/R27: Rust read-back for %s did not match manifest line %q; probe output:\n%s",
				file, want, pOut)
		}
	}
}

// ---- rust/P3 & rust/P4: the_hostile_sweep and the_negative_controls ---------

// t12P3 holds rust/P3 to docs/FIXED-FORM-ALGORITHM.md §7 item 5 ("every byte,
// one bit at a time, is answered one of three ways and never a fourth: a
// refusal by name, a `malformed` read, or a read that lands values"): it runs
// `test/rust-fixedform` (debug and release, as `make tables-rust-fixedform`
// does) and asserts `the_hostile_sweep`'s verdict line.
func t12P3(t *testing.T, corpus string) {
	out := t12FixedFormRun(t, corpus)
	const wantSweep = "P3 hostile sweep: 2334 mutations over 389 bytes — refused=1445 malformed=35 landed=854, three answers never a fourth"
	if strings.Contains(out, "FAIL:") {
		t.Fatalf("rust/P3: test/rust-fixedform reported a failure:\n%s", out)
	}
	if n := strings.Count(out, wantSweep); n < 2 {
		t.Fatalf("rust/P3: want debug+release P3 sweep verdict %q twice (got %d):\n%s", wantSweep, n, out)
	}
}

// t12P4 holds rust/P4 to docs/FIXED-FORM-ALGORITHM.md §7 item 4 ("the wrong
// plan must go red, and so must each fix's own bug: this build's identity plan
// over another schema's record"): it runs `test/rust-fixedform`'s
// `the_negative_controls` through `make tables-rust-fixedform` and asserts its
// verdict line, and also runs the standalone probe's wrong-plan negative
// control over `new_int_widen.bin`.
func t12P4(t *testing.T, corpus string) {
	out := t12FixedFormRun(t, corpus)
	const wantVerdict = "rust fixed form: the bytes are the C++ reference's — the union corpus with them — and the versioned path is the path"
	if strings.Contains(out, "FAIL:") {
		t.Fatalf("rust/P4: test/rust-fixedform negative controls reported a failure:\n%s", out)
	}
	if n := strings.Count(out, wantVerdict); n < 2 {
		t.Fatalf("rust/P4: want debug+release negative-control verdict %q twice (got %d):\n%s", wantVerdict, n, out)
	}
	pOut := t12ProbeRun(t, corpus)
	if line := t12Line(pOut, "P4"); line != "T12 P4 ok" {
		t.Fatalf("rust/P4: standalone wrong-plan negative control failed: %s", line)
	}
}

// ---- shared runners (t12ProbeRun and t12FixedFormRun) -----------------------

var (
	t12ProbeOnce sync.Once
	t12ProbeOut  string
	t12ProbeErr  error

	t12FixedFormOnce sync.Once
	t12FixedFormOut  string
	t12FixedFormErr  error
)

func t12FixedFormRun(t *testing.T, corpus string) string {
	t.Helper()
	_ = corpus
	t12FixedFormOnce.Do(func() {
		repo, err := filepath.Abs("../../..")
		if err != nil {
			t12FixedFormErr = err
			return
		}
		cmd := exec.Command("make", "-C", repo, "tables-rust-fixedform")
		cmd.Env = os.Environ()
		out, err := cmd.CombinedOutput()
		t12FixedFormOut = string(out)
		t12FixedFormErr = err
	})
	if t12FixedFormErr != nil {
		t.Fatalf("make tables-rust-fixedform failed: %v\n%s", t12FixedFormErr, t12FixedFormOut)
	}
	return t12FixedFormOut
}

func t12ProbeRun(t *testing.T, corpus string) string {
	t.Helper()
	t12ProbeOnce.Do(func() {
		t12ProbeOut, t12ProbeErr = t12BuildAndRunProbe(t, corpus)
	})
	if t12ProbeErr != nil {
		t.Fatalf("t12 rustc probe failed: %v\n%s", t12ProbeErr, t12ProbeOut)
	}
	return t12ProbeOut
}

func t12BuildAndRunProbe(t *testing.T, corpus string) (string, error) {
	t.Helper()
	uOld := versionUnit(t, versionSchema(t, "VOLD_field_append"))
	uNew := versionUnit(t, versionSchema(t, "VNEW_field_append"))
	eOld, ok := FixedLineageOf(uOld, "Lineage")
	if !ok {
		return "", fmt.Errorf("no FixedLineageOf for VOLD_field_append")
	}
	lineage := map[string][]FixedLineageEntry{
		"Lineage": {eOld},
	}
	files, err := GenerateLineage(uNew, lineage)
	if err != nil {
		return "", fmt.Errorf("GenerateLineage(VNEW_field_append): %w", err)
	}

	// Also generate VOLD_int_widen and VNEW_int_widen (with lineage) in
	// submodules so R27 can read both int_widen files and P4 can run the wrong
	// identity plan directly over new_int_widen.bin's record body.
	uWidenOld := versionUnit(t, versionSchema(t, "VOLD_int_widen"))
	widenOldFiles, err := GenerateLineage(uWidenOld, nil)
	if err != nil {
		return "", fmt.Errorf("GenerateLineage(VOLD_int_widen): %w", err)
	}
	eWidenOld, ok := FixedLineageOf(uWidenOld, "IntWiden")
	if !ok {
		return "", fmt.Errorf("no FixedLineageOf for VOLD_int_widen")
	}
	uWidenNew := versionUnit(t, versionSchema(t, "VNEW_int_widen"))
	widenNewFiles, err := GenerateLineage(uWidenNew, map[string][]FixedLineageEntry{
		"IntWiden": {eWidenOld},
	})
	if err != nil {
		return "", fmt.Errorf("GenerateLineage(VNEW_int_widen): %w", err)
	}

	dir, err := os.MkdirTemp("", "schema-rust-t12-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)

	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			return "", err
		}
	}
	submod := `pub mod probe_fixed;
pub mod probe_records;
pub use probe_fixed::*;
pub use probe_records::*;
`
	for subName, subFiles := range map[string]map[string][]byte{
		"widen_old": widenOldFiles,
		"widen_new": widenNewFiles,
	} {
		subDir := filepath.Join(dir, subName)
		if err := os.MkdirAll(subDir, 0o750); err != nil {
			return "", err
		}
		for _, name := range []string{"probe_fixed.rs", "probe_records.rs"} {
			src := strings.Replace(string(subFiles[name]), "use crate::*;", "use crate::*;\nuse super::*;", 1)
			if err := os.WriteFile(filepath.Join(subDir, name), []byte(src), 0o600); err != nil {
				return "", err
			}
		}
		if err := os.WriteFile(filepath.Join(subDir, "mod.rs"), []byte(submod), 0o600); err != nil {
			return "", err
		}
	}

	absCorpus, err := filepath.Abs(corpus)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(t12ProbeRustSource(absCorpus)), 0o600); err != nil {
		return "", err
	}
	bin := filepath.Join(dir, "t12probe")
	build := exec.Command("rustc", "--edition", "2024", "-O", "-o", bin, "main.rs")
	build.Dir = dir
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		return string(out), fmt.Errorf("rustc build: %w", err)
	}
	run := exec.Command(bin)
	run.Dir = dir
	out, err := run.CombinedOutput()
	return string(out), err
}

func t12ProbeRustSource(corpusDir string) string {
	return fmt.Sprintf(`#![allow(dead_code, unused_imports)]
pub mod build_version;
pub mod fixed_runtime;
pub mod probe_fixed;
pub mod probe_records;
pub mod widen_old;
pub mod widen_new;

pub use build_version::*;
pub use fixed_runtime::*;
pub use probe_fixed::*;
pub use probe_records::*;

const CORPUS: &str = %q;

fn say(id: &str, ok: bool, why: &str) {
    if ok {
        println!("T12 {id} ok");
    } else {
        println!("T12 {id} BAD {why}");
    }
}

fn poison_row<T>(row: &mut T) {
    unsafe {
        core::ptr::write_bytes(row as *mut T as *mut u8, 0x5A, core::mem::size_of::<T>());
    }
}

fn is_all_poison<T>(row: &T) -> bool {
    let bytes = unsafe {
        core::slice::from_raw_parts(row as *const T as *const u8, core::mem::size_of::<T>())
    };
    bytes.iter().all(|&b| b == 0x5A)
}

fn check_f4() -> Result<(), String> {
    let good = std::fs::read(format!("{CORPUS}/new_field_append.bin"))
        .map_err(|e| format!("read new_field_append.bin: {e}"))?;
    let l = u32::from_le_bytes(good[16..20].try_into().unwrap()) as usize;
    if l == 0 || 20 + l >= good.len() {
        return Err(format!("unexpected layout length {l} in {} bytes", good.len()));
    }

    // Positive control: the full C++ reference file loads cleanly.
    let mut clean = [LineageRow::default(); 1];
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut report = TableFixedReport::default();
    if lineage_fixed_load(&mut clean, &good, &mut plan, &mut remap, &mut report) != Some(1) {
        return Err("positive control failed to load new_field_append.bin".into());
    }
    if report.refused || report.malformed || report.reason != TableFixedReason::None {
        return Err("positive control set refused/malformed".into());
    }

    // Every prefix of length 20..20+L has 20 + L > cut: §5.3 row 3 demands
    // layout_malformed with malformed == false, layout_hash == 0, all counters
    // zero, and the 0x5A-stained destination untouched.
    for cut in 20..(20 + l) {
        let mut dst = [LineageRow::default(); 1];
        poison_row(&mut dst[0]);
        let mut r = TableFixedReport::default();
        let got = lineage_fixed_load(&mut dst, &good[..cut], &mut plan, &mut remap, &mut r);
        if got.is_some() {
            return Err(format!("cut={cut}: truncated layout loaded {:?}", got));
        }
        if !r.refused || r.malformed || r.reason != TableFixedReason::LayoutMalformed {
            return Err(format!(
                "cut={cut}: want refused LayoutMalformed, got refused={} malformed={} reason={}",
                r.refused, r.malformed, r.reason.name()
            ));
        }
        if r.reason.name() != "layout_malformed" || r.layout_hash != 0 {
            return Err(format!("cut={cut}: reason={} hash=0x{:x}", r.reason.name(), r.layout_hash));
        }
        if r.unknown != 0 || r.kind_mismatch != 0 || r.clamped != 0 || r.widened != 0 {
            return Err(format!("cut={cut}: non-zero counters on layout_malformed"));
        }
        if !is_all_poison(&dst[0]) {
            return Err(format!("cut={cut}: destination modified on layout_malformed"));
        }
    }

    // Also forge L in a full-length buffer to good.len() - 20 + 1 and u32::MAX.
    for bad_l in [(good.len() - 20 + 1) as u32, u32::MAX] {
        let mut forged = good.clone();
        forged[16..20].copy_from_slice(&bad_l.to_le_bytes());
        let mut dst = [LineageRow::default(); 1];
        poison_row(&mut dst[0]);
        let mut r = TableFixedReport::default();
        if lineage_fixed_load(&mut dst, &forged, &mut plan, &mut remap, &mut r).is_some() {
            return Err(format!("bad_l={bad_l}: forged L loaded"));
        }
        if !r.refused || r.malformed || r.reason != TableFixedReason::LayoutMalformed || r.layout_hash != 0 {
            return Err(format!("bad_l={bad_l}: wrong refusal {:?}", r.reason.name()));
        }
        if !is_all_poison(&dst[0]) {
            return Err(format!("bad_l={bad_l}: destination modified"));
        }
    }
    Ok(())
}

fn check_w13() -> Result<(), String> {
    let good = std::fs::read(format!("{CORPUS}/new_field_append.bin"))
        .map_err(|e| format!("read new_field_append.bin: {e}"))?;
    let wire_hash = u64::from_le_bytes(good[8..16].try_into().unwrap());
    let l = u32::from_le_bytes(good[16..20].try_into().unwrap()) as usize;
    let wire_block = &good[20..20 + l];
    if LINEAGE_FIXED_HASH != wire_hash {
        return Err(format!("LINEAGE_FIXED_HASH 0x{:016x} != wire 0x{:016x}", LINEAGE_FIXED_HASH, wire_hash));
    }
    if &LINEAGE_FIXED_BLOCK[..] != wire_block {
        return Err("LINEAGE_FIXED_BLOCK != wire layout block".into());
    }
    Ok(())
}

fn check_p2() -> Result<(), String> {
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];

    // 1. Identity plan against C++ reference golden bytes (new_field_append.bin):
    //    read -> write (first) -> read -> write (second) must equal golden and each other.
    let golden = std::fs::read(format!("{CORPUS}/new_field_append.bin"))
        .map_err(|e| format!("read new_field_append.bin: {e}"))?;
    let mut row1 = [LineageRow::default(); 1];
    poison_row(&mut row1[0]);
    let mut report = TableFixedReport::default();
    if lineage_fixed_load(&mut row1, &golden, &mut plan, &mut remap, &mut report) != Some(1) {
        return Err("failed to load new_field_append.bin".into());
    }
    let mut first = vec![0xFFu8; lineage_fixed_measure(1)];
    if lineage_fixed_save(&row1, &mut first) != Some(golden.len()) {
        return Err("first save failed".into());
    }
    if first != golden {
        return Err("first save != C++ reference golden bytes".into());
    }
    let mut row2 = [LineageRow::default(); 1];
    poison_row(&mut row2[0]);
    let mut report2 = TableFixedReport::default();
    if lineage_fixed_load(&mut row2, &first, &mut plan, &mut remap, &mut report2) != Some(1) {
        return Err("second load failed".into());
    }
    let mut second = vec![0xAAu8; lineage_fixed_measure(1)];
    if lineage_fixed_save(&row2, &mut second) != Some(first.len()) {
        return Err("second save failed".into());
    }
    if second != first {
        return Err("second save != first save on identity plan".into());
    }

    // 2. Compiled lineage plan (reading old_field_append.bin into VNEW LineageRow):
    //    write -> read -> write is byte-identical.
    let old_bytes = std::fs::read(format!("{CORPUS}/old_field_append.bin"))
        .map_err(|e| format!("read old_field_append.bin: {e}"))?;
    let mut upgraded = [LineageRow::default(); 1];
    poison_row(&mut upgraded[0]);
    let mut up_rep = TableFixedReport::default();
    if lineage_fixed_load(&mut upgraded, &old_bytes, &mut plan, &mut remap, &mut up_rep) != Some(1) {
        return Err("compiled lineage load of old_field_append.bin failed".into());
    }
    let mut up_first = vec![0x5Au8; lineage_fixed_measure(1)];
    if lineage_fixed_save(&upgraded, &mut up_first).is_none() {
        return Err("up_first save failed".into());
    }
    let mut upgraded_back = [LineageRow::default(); 1];
    poison_row(&mut upgraded_back[0]);
    let mut up_rep2 = TableFixedReport::default();
    if lineage_fixed_load(&mut upgraded_back, &up_first, &mut plan, &mut remap, &mut up_rep2) != Some(1) {
        return Err("upgraded_back load failed".into());
    }
    let mut up_second = vec![0xA5u8; lineage_fixed_measure(1)];
    if lineage_fixed_save(&upgraded_back, &mut up_second).is_none() {
        return Err("up_second save failed".into());
    }
    if up_first != up_second {
        return Err("write-read-write after compiled lineage read is not byte-identical".into());
    }
    Ok(())
}

fn check_r27() -> Result<(), String> {
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];

    let old_bytes = std::fs::read(format!("{CORPUS}/old_field_append.bin"))
        .map_err(|e| format!("read old_field_append.bin: {e}"))?;
    let mut old_row = [LineageRow::default(); 1];
    poison_row(&mut old_row[0]);
    let mut rep = TableFixedReport::default();
    let n_old = lineage_fixed_load(&mut old_row, &old_bytes, &mut plan, &mut remap, &mut rep)
        .ok_or_else(|| "load old_field_append.bin failed".to_string())?;
    println!(
        "T12 R27-FILE old_field_append.bin records={} values=r0.x={},r0.y={},r0.z={}",
        n_old, old_row[0].x, old_row[0].y, old_row[0].z
    );
    if old_row[0].w != 77 {
        return Err(format!("old_field_append default w={}, want 77", old_row[0].w));
    }

    let new_bytes = std::fs::read(format!("{CORPUS}/new_field_append.bin"))
        .map_err(|e| format!("read new_field_append.bin: {e}"))?;
    let mut new_row = [LineageRow::default(); 1];
    poison_row(&mut new_row[0]);
    let mut rep2 = TableFixedReport::default();
    let n_new = lineage_fixed_load(&mut new_row, &new_bytes, &mut plan, &mut remap, &mut rep2)
        .ok_or_else(|| "load new_field_append.bin failed".to_string())?;
    println!(
        "T12 R27-FILE new_field_append.bin records={} values=r0.x={},r0.y={},r0.z={},r0.w={}",
        n_new, new_row[0].x, new_row[0].y, new_row[0].z, new_row[0].w
    );

    let old_widen = std::fs::read(format!("{CORPUS}/old_int_widen.bin"))
        .map_err(|e| format!("read old_int_widen.bin: {e}"))?;
    let mut w_old_rows = [widen_new::IntWidenRow::default(); 3];
    for r in &mut w_old_rows {
        poison_row(r);
    }
    let mut w_plan = vec![TableFixedEntry::default(); 4096];
    let mut w_remap = vec![0u16; 4096];
    let mut w_rep = TableFixedReport::default();
    let n_wold = widen_new::int_widen_fixed_load(&mut w_old_rows, &old_widen, &mut w_plan, &mut w_remap, &mut w_rep)
        .ok_or_else(|| "load old_int_widen.bin failed".to_string())?;
    println!(
        "T12 R27-FILE old_int_widen.bin records={} values=r0.lead={},r0.v={},r0.trail={},r1.lead={},r1.v={},r1.trail={},r2.lead={},r2.v={},r2.trail={}",
        n_wold,
        w_old_rows[0].lead, w_old_rows[0].v, w_old_rows[0].trail,
        w_old_rows[1].lead, w_old_rows[1].v, w_old_rows[1].trail,
        w_old_rows[2].lead, w_old_rows[2].v, w_old_rows[2].trail
    );

    let new_widen = std::fs::read(format!("{CORPUS}/new_int_widen.bin"))
        .map_err(|e| format!("read new_int_widen.bin: {e}"))?;
    let mut w_new_rows = [widen_new::IntWidenRow::default(); 1];
    poison_row(&mut w_new_rows[0]);
    let mut w_rep2 = TableFixedReport::default();
    let n_wnew = widen_new::int_widen_fixed_load(&mut w_new_rows, &new_widen, &mut w_plan, &mut w_remap, &mut w_rep2)
        .ok_or_else(|| "load new_int_widen.bin failed".to_string())?;
    println!(
        "T12 R27-FILE new_int_widen.bin records={} values=r0.lead={},r0.v={},r0.trail={}",
        n_wnew, w_new_rows[0].lead, w_new_rows[0].v, w_new_rows[0].trail
    );
    Ok(())
}

fn check_p4() -> Result<(), String> {
    // 1. VOLD_int_widen loading new_int_widen.bin refuses layout_newer with
    //    the file's hash and touches nothing.
    let new_widen = std::fs::read(format!("{CORPUS}/new_int_widen.bin"))
        .map_err(|e| format!("read new_int_widen.bin: {e}"))?;
    let new_hash = u64::from_le_bytes(new_widen[8..16].try_into().unwrap());
    let l = u32::from_le_bytes(new_widen[16..20].try_into().unwrap()) as usize;
    let new_body = &new_widen[20 + l + 8..];

    let mut dst = [widen_old::IntWidenRow::default(); 1];
    poison_row(&mut dst[0]);
    let mut plan = vec![TableFixedEntry::default(); 4096];
    let mut remap = vec![0u16; 4096];
    let mut rep = TableFixedReport::default();
    if widen_old::int_widen_fixed_load(&mut dst, &new_widen, &mut plan, &mut remap, &mut rep).is_some() {
        return Err("VOLD_int_widen accepted new_int_widen.bin instead of refusing layout_newer".into());
    }
    if !rep.refused || rep.malformed || rep.reason != TableFixedReason::LayoutNewer || rep.layout_hash != new_hash {
        return Err("VOLD_int_widen did not report LayoutNewer with the file's hash".into());
    }
    if !is_all_poison(&dst[0]) {
        return Err("VOLD_int_widen modified destination on LayoutNewer".into());
    }

    // 2. Running VOLD_int_widen's identity plan directly over new_int_widen.bin's
    //    body lands the wrong bytes into trail (because v widened from int16 to
    //    int32, shifting trail by 2 bytes), proving the wrong plan goes red.
    let mut wrong_img = [0u8; widen_old::INT_WIDEN_FIXED_BODY_BYTES];
    let mut run_rep = TableFixedReport::default();
    table_fixed_run(
        &widen_old::INT_WIDEN_FIXED_PLAN,
        &[],
        &[],
        new_body,
        &mut wrong_img,
        &mut run_rep,
    );
    let mut wrong_row = widen_old::IntWidenRow::default();
    widen_old::int_widen_fixed_scatter(&wrong_img, &mut wrong_row, &mut run_rep);
    if wrong_row.trail == 3149642683 {
        return Err("NEGATIVE CONTROL FAILED: VOLD_int_widen's identity plan reproduced VNEW_int_widen's trail".into());
    }
    Ok(())
}

fn main() {
    match check_f4() {
        Ok(()) => say("F4", true, ""),
        Err(e) => say("F4", false, &e),
    }
    match check_w13() {
        Ok(()) => say("W13", true, ""),
        Err(e) => say("W13", false, &e),
    }
    match check_p2() {
        Ok(()) => say("P2", true, ""),
        Err(e) => say("P2", false, &e),
    }
    match check_r27() {
        Ok(()) => say("R27", true, ""),
        Err(e) => say("R27", false, &e),
    }
    match check_p4() {
        Ok(()) => say("P4", true, ""),
        Err(e) => say("P4", false, &e),
    }
}
`, corpusDir)
}

func t12Line(out, id string) string {
	prefix := "T12 " + id + " "
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return fmt.Sprintf("[no T12 line for %s in probe output: %s]", id, strings.TrimSpace(out))
}
