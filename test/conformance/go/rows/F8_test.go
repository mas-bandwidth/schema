package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestRowF8 is the conformance assertion for cell go/F8 of schema#876 /
// schema#898: "malformed, ragged tail". The law (docs/FIXED-FORM-ALGORITHM.md:894):
//
//	| a ragged tail: rest mod record_bytes != 0 | — | malformed, and -1 |
//
// A fixed-form file whose remaining bytes after the header and layout are not
// an exact multiple of the record size is MALFORMED (not refused). The standard
// names it (docs/SPEC-TABLES.md §3.4): "BYTES LEFT OVER ARE `malformed`, named
// `truncated`", so the report also carries Reason "truncated". The reader sets
// Malformed = true, Reason = "truncated", returns -1, and moves no counters.
// This test forges the second arm of the guard at fixedform.go:798
// (rest % recordBytes != 0) by appending 1..(recordBytes-1) bytes to a valid
// single-record file.
//
// Negative control: the sabotage turns the ragged-tail arm into `false` so
// the condition reads `recordBytes <= 8 || false`; a one-extra-byte file then
// reads clean and the test goes RED.
func TestRowF8(t *testing.T) {
	schema := `package probe
fixed table Point {
	x int32 = 1
	y int32 = 2
}
`
	testSrc := `package probe

import (
	"testing"
)

func TestRaggedTail(t *testing.T) {
	one := Point{X: 4242, Y: -7}
	need := PointFixedMeasure(1)
	buf := make([]byte, need)
	if n := PointFixedSave([]Point{one}, buf); n != need {
		t.Fatalf("save %d", n)
	}
	good := make([]Point, 1)
	var r TableReport
	plan := make([]TableFixedEntry, 64)
	if n := PointFixedLoad(good, buf, plan, &r); n != 1 || r != (TableReport{}) || good[0] != one {
		t.Fatalf("the good file does not read back: n=%d got=%+v report=%+v", n, good[0], r)
	}
	recordBytes := PointFixedRecordBytes
	for extra := 1; extra < recordBytes; extra++ {
		got := []Point{{X: -1, Y: -1}}
		r = TableReport{}
		ragged := append(buf, make([]byte, extra)...)
		if n := PointFixedLoad(got, ragged, plan, &r); n != -1 || !r.Malformed || r.Reason != "truncated" || got[0] != (Point{X: -1, Y: -1}) {
			t.Fatalf("extra=%d: n=%d report=%+v dest=%+v", extra, n, r, got[0])
		}
	}
	// extra == recordBytes is one more whole record, not a ragged tail:
	// it owes batch_too_large (2 records > capacity of 1).
	got := []Point{{X: -1, Y: -1}}
	r = TableReport{}
	whole := append(buf, make([]byte, recordBytes)...)
	if n := PointFixedLoad(got, whole, plan, &r); n != -1 || r.Verdict != TableOpenRefused || r.Reason != "batch_too_large" || r.Malformed {
		t.Fatalf("extra==recordBytes owes batch_too_large, not malformed: n=%d report=%+v", n, r)
	}
}
`
	dir := t.TempDir()

	// Write the schema file.
	schemaPath := filepath.Join(dir, "probe.schema")
	if err := os.WriteFile(schemaPath, []byte(schema), 0o600); err != nil {
		t.Fatal(err)
	}

	// Find bin/schema relative to this test's location.
	// This file is at test/conformance/go/rows/F8_test.go; bin/schema is at repo root.
	repoRoot, err := filepath.Abs(filepath.Join(".", "..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binSchema := filepath.Join(repoRoot, "bin", "schema")

	// Generate Go code.
	genDir := filepath.Join(dir, "gen")
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binSchema, "generate", "--lang", "go", "--out", genDir, schemaPath)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bin/schema generate: %v\n%s", err, out)
	}

	// The generated code lands directly in genDir (one package per schema file).
	pkgDir := genDir

	// Find serialize.go runtime.
	runtimePath := filepath.Join(repoRoot, "serialize.go")

	// Write go.mod.
	goMod := fmt.Sprintf("module probe\n\ngo 1.23\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtimePath)
	if err := os.WriteFile(filepath.Join(pkgDir, "go.mod"), []byte(goMod), 0o600); err != nil {
		t.Fatal(err)
	}

	// Write the test file.
	if err := os.WriteFile(filepath.Join(pkgDir, "ragged_test.go"), []byte(testSrc), 0o600); err != nil {
		t.Fatal(err)
	}

	// Run the test.
	cmd = exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = pkgDir
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test: %v\n%s", err, out)
	}
}

// TestRowF8NegativeControl sabotages the ragged-tail check in the generated
// code and proves the assertion bites: the condition `rest % recordBytes != 0`
// is turned into `false`, so a one-extra-byte file reads clean and the test
// must go RED.
func TestRowF8NegativeControl(t *testing.T) {
	schema := `package probe
fixed table Point {
	x int32 = 1
	y int32 = 2
}
`
	testSrc := `package probe

import (
	"testing"
)

func TestRaggedTail(t *testing.T) {
	one := Point{X: 4242, Y: -7}
	need := PointFixedMeasure(1)
	buf := make([]byte, need)
	if n := PointFixedSave([]Point{one}, buf); n != need {
		t.Fatalf("save %d", n)
	}
	good := make([]Point, 1)
	var r TableReport
	plan := make([]TableFixedEntry, 64)
	if n := PointFixedLoad(good, buf, plan, &r); n != 1 || r != (TableReport{}) || good[0] != one {
		t.Fatalf("the good file does not read back: n=%d got=%+v report=%+v", n, good[0], r)
	}
	recordBytes := PointFixedRecordBytes
	for extra := 1; extra < recordBytes; extra++ {
		got := []Point{{X: -1, Y: -1}}
		r = TableReport{}
		ragged := append(buf, make([]byte, extra)...)
		if n := PointFixedLoad(got, ragged, plan, &r); n != -1 || !r.Malformed || r.Reason != "truncated" || got[0] != (Point{X: -1, Y: -1}) {
			t.Fatalf("extra=%d: n=%d report=%+v dest=%+v", extra, n, r, got[0])
		}
	}
}
`
	dir := t.TempDir()

	schemaPath := filepath.Join(dir, "probe.schema")
	if err := os.WriteFile(schemaPath, []byte(schema), 0o600); err != nil {
		t.Fatal(err)
	}

	repoRoot, err := filepath.Abs(filepath.Join(".", "..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binSchema := filepath.Join(repoRoot, "bin", "schema")

	genDir := filepath.Join(dir, "gen")
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binSchema, "generate", "--lang", "go", "--out", genDir, schemaPath)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bin/schema generate: %v\n%s", err, out)
	}

	pkgDir := genDir

	// SABOTAGE: replace `rest % recordBytes != 0` with `false` in the
	// generated table code, so the ragged-tail arm is disabled.
	tableFile := filepath.Join(pkgDir, "probeTable.go")
	data, err := os.ReadFile(tableFile)
	if err != nil {
		t.Fatal(err)
	}
	sabotaged := bytes.ReplaceAll(data, []byte("rest%recordBytes != 0"), []byte("false"))
	if bytes.Equal(data, sabotaged) {
		t.Fatal("sabotage did not apply: the pattern was not found in the generated code")
	}
	if err := os.WriteFile(tableFile, sabotaged, 0o600); err != nil {
		t.Fatal(err)
	}

	runtimePath := filepath.Join(repoRoot, "serialize.go")
	goMod := fmt.Sprintf("module probe\n\ngo 1.23\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtimePath)
	if err := os.WriteFile(filepath.Join(pkgDir, "go.mod"), []byte(goMod), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "ragged_test.go"), []byte(testSrc), 0o600); err != nil {
		t.Fatal(err)
	}

	// Run the test — it MUST FAIL because the sabotage disabled the check.
	cmd = exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = pkgDir
	out, err = cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: the sabotaged code passed\n%s", out)
	}
	// The failure must be about the ragged tail assertion, not a compile error.
	if !bytes.Contains(out, []byte("extra=")) {
		t.Fatalf("NEGATIVE CONTROL FAILED: went red for the wrong reason\n%s", out)
	}
}

// TestRowF8RaggedTailByName pins the fixed load's answer to a ragged trailing
// tail BY NAME for every tail length from 0 to the record size. The standard
// (docs/SPEC-TABLES.md §3.4) is explicit: "BYTES LEFT OVER ARE `malformed`,
// named `truncated`". So a batch whose length is not a whole number of records
// is refused as malformed/truncated (Reason "truncated", Malformed true, -1);
// a batch that holds a whole number of records — tail 0, or a whole extra
// record — reads clean and counts them all.
func TestRowF8RaggedTailByName(t *testing.T) {
	t.Parallel()
	schema := `package probe
fixed table Point {
	x int32 = 1
	y int32 = 2
}
`
	testSrc := `package probe

import (
	"testing"
)

func TestRaggedTailByName(t *testing.T) {
	one := Point{X: 4242, Y: -7}
	recordBytes := PointFixedRecordBytes
	for extra := 0; extra <= recordBytes; extra++ {
		r := TableReport{}
		plan := make([]TableFixedEntry, 64)
		if extra%recordBytes == 0 {
			want := int64(1 + extra/recordBytes)
			file := make([]byte, PointFixedMeasure(want))
			if n := PointFixedSave(make([]Point, want), file); n != PointFixedMeasure(want) {
				t.Fatalf("save %d", n)
			}
			got := make([]Point, want)
			if n := PointFixedLoad(got, file, plan, &r); n != want || r != (TableReport{}) {
				t.Fatalf("extra=%d (whole): n=%d want=%d report=%+v", extra, n, want, r)
			}
		} else {
			buf := make([]byte, PointFixedMeasure(1))
			if n := PointFixedSave([]Point{one}, buf); n != PointFixedMeasure(1) {
				t.Fatalf("save %d", n)
			}
			ragged := append(buf, make([]byte, extra)...)
			got := []Point{{X: -1, Y: -1}}
			if n := PointFixedLoad(got, ragged, plan, &r); n != -1 || !r.Malformed || r.Reason != "truncated" || got[0] != (Point{X: -1, Y: -1}) {
				t.Fatalf("extra=%d: n=%d report=%+v dest=%+v", extra, n, r, got[0])
			}
		}
	}
}
`
	dir := t.TempDir()

	// Write the schema file.
	schemaPath := filepath.Join(dir, "probe.schema")
	if err := os.WriteFile(schemaPath, []byte(schema), 0o600); err != nil {
		t.Fatal(err)
	}

	// Find bin/schema relative to this test's location.
	// This file is at test/conformance/go/rows/F8_test.go; bin/schema is at repo root.
	repoRoot, err := filepath.Abs(filepath.Join(".", "..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binSchema := filepath.Join(repoRoot, "bin", "schema")

	// Generate Go code.
	genDir := filepath.Join(dir, "gen")
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binSchema, "generate", "--lang", "go", "--out", genDir, schemaPath)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bin/schema generate: %v\n%s", err, out)
	}

	// The generated code lands directly in genDir (one package per schema file).
	pkgDir := genDir

	// Find serialize.go runtime.
	runtimePath := filepath.Join(repoRoot, "serialize.go")

	// Write go.mod.
	goMod := fmt.Sprintf("module probe\n\ngo 1.23\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtimePath)
	if err := os.WriteFile(filepath.Join(pkgDir, "go.mod"), []byte(goMod), 0o600); err != nil {
		t.Fatal(err)
	}

	// Write the test file.
	if err := os.WriteFile(filepath.Join(pkgDir, "ragged_test.go"), []byte(testSrc), 0o600); err != nil {
		t.Fatal(err)
	}

	// Run the test.
	cmd = exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = pkgDir
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test: %v\n%s", err, out)
	}
}
