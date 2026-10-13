package jstable

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// blockbuild_test.go pins the BUILD side of the block form in this leg
// (docs/SPEC-TABLES.md §19.1). The oracle is the Go builder's own numbers for
// the same declaration and the same counts, which internal/codegen/gotable's
// blockbuild_test.go pins by RUNNING it: for `Root{Rows:17,Other:2}` the Go
// builder answers Bytes=448 with rows at offset_of 64 and other at 384, both at
// stride 16. A JavaScript Begin that writes anything else — a different pitch,
// a different rounding, one cleared row — is a different block form.

// TestBlockBuildAndCookWriteJavaScript builds one block with the leg's own node
// (jsNode, $NODE) and holds the whole extent byte for byte: §19.1's prologue
// (magic, BuildVersion, byte order), the two triples at the Go builder's
// offsets with the Go builder's numbers, and NOT ONE OTHER BYTE — Begin writes
// neither rows nor padding, which is what makes its bytes the Go builder's.
// The same extent is then opened by this leg's own reader and every accessor
// agrees with the counts written. A count past its declared maximum, and a
// negative count, are refused with the extent untouched (§19.1).
func TestBlockBuildAndCookWriteJavaScript(t *testing.T) {
	t.Parallel()
	node := jsNode(t)
	u := loadUnit(t, "testdata/blockbuild/probe.schema")
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	script := filepath.Join(dir, "probe.mjs")
	probe := `
import { RootBlock } from "./probeBlock.js";
const bytes = new Uint8Array(RootBlock.MaxBytes);
const used = RootBlock.Begin(bytes, { Rows: 17, Other: 2 });
if (!(used > 0)) { console.log("begin refused: " + used); process.exit(1); }
const hex = Buffer.from(bytes.subarray(0, used)).toString("hex");
console.log("used " + used);
console.log("hex " + hex);
const block = RootBlock.Open(bytes.subarray(0, used));
if (block === null) { console.log("this leg's own reader refused what this leg built"); process.exit(1); }
console.log("accessors " + [RootBlock.RowsCount(block), RootBlock.RowsPitch(block), RootBlock.RowsAt(block, 0),
  RootBlock.OtherCount(block), RootBlock.OtherAt(block, 1), block.Used].join(" "));
if (RootBlock.Begin(bytes, { Rows: 66, Other: 2 }) !== -1) { console.log("a count past the declared maximum was accepted"); process.exit(1); }
if (RootBlock.Begin(bytes, { Rows: 17, Other: -1 }) !== -1) { console.log("a negative count was accepted"); process.exit(1); }
if (Buffer.from(bytes.subarray(0, used)).toString("hex") !== hex) { console.log("a refusal changed the extent"); process.exit(1); }
console.log("refusals left the extent alone");
`
	if err := os.WriteFile(script, []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, script)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated JavaScript did not build a block: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 4 {
		t.Fatalf("probe output:\n%s", out)
	}
	// §19.1's used extent, and the Go builder's own answer for these counts.
	if lines[0] != "used 448" {
		t.Fatalf("the used extent is %q, and the Go builder answers 448 for Rows:17,Other:2 "+
			"(internal/codegen/gotable/blockbuild_test.go)", lines[0])
	}
	built, err := hex.DecodeString(strings.TrimPrefix(lines[1], "hex "))
	if err != nil {
		t.Fatalf("hex: %v\n%s", err, out)
	}
	// THE ORACLE'S IMAGE: the prologue, then the two triples where the reader
	// reads them, and zero everywhere else. The triples' own bytes are the Go
	// builder's — offset_of 64 and 384, count 17 and 2, stride 16 — and their
	// positions are found in the built extent rather than restated here, so a
	// builder that moved a triple is caught by the whole-image compare below.
	want := make([]byte, 448)
	binary.LittleEndian.PutUint64(want[0:], 0x4b4c42414d484353) // §19.1's magic
	binary.LittleEndian.PutUint64(want[8:], ir.BuildVersion(u)) // §20
	binary.LittleEndian.PutUint64(want[16:], 1)                 // §20.3: this build's byte order
	triples := map[string]string{
		"rows":  "40000000000000001100000010000000", // offset_of 64, count 17, stride 16
		"other": "80010000000000000200000010000000", // offset_of 384, count 2, stride 16
	}
	have := hex.EncodeToString(built)
	for array, group := range triples {
		at := strings.Index(have, group)
		if at < 0 || at%16 != 0 {
			t.Fatalf("the %s triple is not in the extent as the Go builder writes it (offset_of, count, stride): %s", array, have)
		}
		raw, err := hex.DecodeString(group)
		if err != nil {
			t.Fatal(err)
		}
		copy(want[at/2:], raw)
	}
	if !bytes.Equal(built, want) {
		t.Fatalf("the built extent is not the Go builder's:\n built %s\n want  %s", have, hex.EncodeToString(want))
	}
	// the reader this leg already had opens what this leg just built, and every
	// accessor answers the counts written (§19.2)
	if lines[2] != "accessors 17 16 64 2 400 448" {
		t.Fatalf("the accessors disagree with the counts written: %q", lines[2])
	}
	if lines[3] != "refusals left the extent alone" {
		t.Fatalf("a refused Begin changed the extent: %q", lines[3])
	}
}
