// R14, the behavioural half: a union tag grown 1 -> 8 between the writer's
// lock layout and the reader reads the WRITER's tag at the WRITER's width, so
// the pair compiles whole (docs/FIXED-FORM-ALGORITHM.md §5.2 EMIT kind 15, the
// ruling of 2026-11-25; the Rust twin is test/conformance/rust/rows/R14.rs).
//
// THE PRODUCTION PATH ON THIS LEG. The plan compiler is the generated unit's
// tableFixedCompile (emitted from internal/codegen/gotable/fixedruntime.go into
// every unit, here build/tables-generated-go/p3/Tblp3Table.go). It pushes the
// union's None const as TableFixedEntry{Src: their_at, Size: their_tag, Meta:
// my_tag}: Size is the width it READS (the writer's), Meta the width it LANDS
// (mine). tableFixedPush bounds every entry by tableFixedSrcEnd against the
// writer's declared record, and the None const's reach is Src + Size. A reach
// at the reader's width, 0 + 8 against a 5-byte record, would refuse a lawful
// file layout_record_too_large and read the arm's payload into the tag.
//
// The compiler is unexported, so the row hands it the vector from inside the
// unit: it copies the generated p3 unit into a scratch directory under build/,
// adds one in-package test (r14Probe, below), and runs go test there. The
// probe drives the unit's own parse, compile, lineage plans, check pass and
// run loop — the code a load runs — over the same vector the Rust and JS rows
// use. No API is added for it.
//
// THE VECTOR. A layout block is a u32LE entry count, then 17-byte entries: id
// u64LE, kind u8, size u32LE, children u32LE. Kind 13 a table, 15 a union
// (size = tag + widest arm), 8 a u32.
//   THEIRS: [1,13,5,1] [7,15,5,1] [9,8,4,0] — record 5, a 1-byte tag over a u32.
//   MINE(tag): [1,13,tag+4,1] [7,15,tag+4,1] [9,8,4,0] — the tag grown.
//   My storage: the union's tag at image 0, the u32 arm at image `tag`.
//
// BUILD/RUN (from ./repo, after make build/tables-generated-go/.stamp):
//   cd test/conformance/go && go test ./rows/ -run 'TestRowR14GrownTag' -count=1 -v
// Plant: pushing the None const with Size: dstTag (the reader's width) in
// internal/codegen/gotable/fixedruntime.go turns this row red by name.

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const r14Probe = `package tblp3

import (
	"encoding/binary"
	"testing"
)

func r14Block(entries [][4]uint32) []byte {
	b := binary.LittleEndian.AppendUint32(nil, uint32(len(entries)))
	for _, e := range entries {
		b = binary.LittleEndian.AppendUint64(b, uint64(e[0]))
		b = append(b, byte(e[1]))
		b = binary.LittleEndian.AppendUint32(b, e[2])
		b = binary.LittleEndian.AppendUint32(b, e[3])
	}
	return b
}

var r14Theirs = r14Block([][4]uint32{{1, 13, 5, 1}, {7, 15, 5, 1}, {9, 8, 4, 0}})

func r14Mine(tag uint32) []byte {
	return r14Block([][4]uint32{{1, 13, tag + 4, 1}, {7, 15, tag + 4, 1}, {9, 8, 4, 0}})
}

// my storage: the root at 0, the union's tag at image 0, its u32 arm past the tag
func r14Dst(tag uint32) []TableFixedDst {
	return []TableFixedDst{{}, {Dst: 0, Aux: 0}, {Dst: tag}}
}

func r14Compile(t *testing.T, tag uint32) ([]TableFixedEntry, int32) {
	t.Helper()
	theirs, why := tableFixedParseLayout(r14Theirs)
	if why != "" {
		t.Fatalf("R14: THEIRS parses under §1.1, got %s", why)
	}
	if theirs.Count != 3 || tableFixedEntryAt(theirs, 0).Size != 5 {
		t.Fatalf("R14: THEIRS is 3 entries, a record of 5")
	}
	if _, why := tableFixedParseLayout(r14Mine(tag)); why != "" {
		t.Fatalf("R14: MINE(%d) parses under §1.1, got %s", tag, why)
	}
	plan := make([]TableFixedEntry, 256)
	var report TableReport
	made := tableFixedCompile(theirs, r14Mine(tag), r14Dst(tag), plan, &report)
	return plan, made
}

func r14Run(t *testing.T, plan []TableFixedEntry, n int32, record []byte) ([]byte, string) {
	t.Helper()
	img := make([]byte, 12)
	for i := range img {
		img[i] = 0xEE // poisoned: every byte the plan owns must be written
	}
	if why := tableFixedTextRefusal(plan, n, record); why != "" {
		return img, why
	}
	var report TableReport
	return img, tableFixedRun(plan, n, record, img, &report)
}

func TestR14Probe(t *testing.T) {
	// 1. THE CELL: the tag grown 1 -> 8 compiles WHOLE
	plan, made := r14Compile(t, 8)
	if made <= 0 {
		t.Fatalf("R14: the grown tag (1 -> 8) compiles whole: tableFixedCompile answers %d (-3 is layout_record_too_large)", made)
	}
	t.Logf("PASS the grown tag (1 -> 8) compiles whole: %d entries", made)

	// 2. THE None CONST reads the WRITER's 1-byte tag, lands MY 8
	var none *TableFixedEntry
	for i := range made {
		e := &plan[i]
		if e.Op == tableFixedConst && e.Aux == 0 && e.DstSize != 0 {
			none = e
		}
		if end := tableFixedSrcEnd(*e); end > 5 {
			t.Fatalf("R14: entry %d (op %d) reaches %d past the writer's record of 5", i, e.Op, end)
		}
	}
	if none == nil {
		t.Fatal("R14: the union's None const is in the plan")
	}
	if none.Src != 0 || none.Size != 1 || none.Meta != 8 || none.DstSize != 1 || none.Guard != tableFixedNoGuard {
		t.Fatalf("R14: the None const reads the WRITER's 1-byte tag at 0 and lands MY 8: src=%d size=%d meta=%d arms=%d guard=%d",
			none.Src, none.Size, none.Meta, none.DstSize, none.Guard)
	}
	if end := tableFixedSrcEnd(*none); end != 1 {
		t.Fatalf("R14: the None const's reach is 0 + 1 of 5, got %d", end)
	}
	t.Log("PASS the None const: src 0, reads 1 byte (the writer's tag), lands 8, reach 1 of 5")

	// 3. THE READ IT LAYS
	img, why := r14Run(t, plan, made, []byte{1, 0x44, 0x33, 0x22, 0x11})
	if why != "" {
		t.Fatalf("R14: arm 1 reads with no refusal, got %s", why)
	}
	if tag := binary.LittleEndian.Uint64(img[0:]); tag != 1 {
		t.Fatalf("R14: my 8-byte tag lands exactly 1, got %#x", tag)
	}
	if v := binary.LittleEndian.Uint32(img[8:]); v != 0x11223344 {
		t.Fatalf("R14: the u32 lands whole past my tag, got %#x", v)
	}
	t.Log("PASS arm 1: my 8-byte tag is 1, the u32 0x11223344 lands whole")
	img, why = r14Run(t, plan, made, []byte{0, 0, 0, 0, 0})
	if why != "" || binary.LittleEndian.Uint64(img[0:]) != 0 {
		t.Fatalf("R14: tag 0 reads as None in my 8-byte tag: why=%q tag=%#x", why, binary.LittleEndian.Uint64(img[0:]))
	}
	t.Log("PASS tag 0 reads as None")
	if _, why = r14Run(t, plan, made, []byte{2, 0, 0, 0, 0}); why != TableRefuseOutOfRange.Error() {
		t.Fatalf("R14: a forged writer tag 2 refuses out_of_range, got %q", why)
	}
	t.Log("PASS a forged writer tag 2 refuses out_of_range")

	// 4. THE TWINS compile whole
	for _, tag := range []uint32{1, 2} {
		if _, n := r14Compile(t, tag); n <= 0 {
			t.Fatalf("R14: the twin MINE(%d) compiles whole, got %d", tag, n)
		}
	}
	t.Log("PASS the twins MINE(1) and MINE(2) compile whole")

	// 5. THE LINEAGE PATH a load runs lays a ready plan
	for _, tag := range []uint32{8, 2} {
		known := []TableFixedKnownLayout{{Hash: 0x3333333344444444, Layout: r14Theirs, LayoutBytes: len(r14Theirs), Record: 5}}
		lanes := tableFixedLineagePlans(known, r14Mine(tag), r14Dst(tag), 0x1111111122222222)
		if len(lanes) != 1 || lanes[0].Why != "" || lanes[0].Count <= 0 {
			t.Fatalf("R14: the lineage plan for MINE(%d) is ready, got %+v", tag, lanes)
		}
	}
	t.Log("PASS the lineage plans for MINE(8) and MINE(2) are ready")
}
`

func TestRowR14GrownTagReadsTheWritersTagWidth(t *testing.T) {
	root := findRepoRoot(t)
	unit := filepath.Join(root, "build", "tables-generated-go", "p3")
	files, err := filepath.Glob(filepath.Join(unit, "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("R14: the generated p3 unit is not built at %s (make build/tables-generated-go/.stamp): %v", unit, err)
	}
	scratch := filepath.Join(root, "build", "conformance-go-r14")
	if err := os.MkdirAll(scratch, 0o755); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(scratch, "p3-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(f)), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// the unit's go.mod, its relative replaces made absolute against the unit
	mod, err := os.ReadFile(filepath.Join(unit, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, line := range strings.Split(string(mod), "\n") {
		if i := strings.Index(line, "=> ../"); i >= 0 {
			line = line[:i+3] + filepath.Join(unit, strings.TrimSpace(line[i+3:]))
		}
		out = append(out, line)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(strings.Join(out, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	if sum, err := os.ReadFile(filepath.Join(unit, "go.sum")); err == nil {
		if err := os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "r14probe_test.go"), []byte(r14Probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-count=1", "-run", "^TestR14Probe$", "-v", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	b, err := cmd.CombinedOutput()
	t.Logf("%s", b)
	if err != nil {
		t.Fatalf("R14: the probe over the generated p3 unit failed: %v", err)
	}
	if !strings.Contains(string(b), "--- PASS: TestR14Probe") {
		t.Fatal("R14: the probe ran no TestR14Probe (0 tests is a failure)")
	}
}
