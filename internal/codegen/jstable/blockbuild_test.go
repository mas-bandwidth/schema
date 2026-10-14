package jstable

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
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
// negative count, are refused with the extent untouched (§19.1). The cook
// half of the same probe (docs/SPEC-TABLES.md §7.6) is tablecook.Cook's bytes
// in both orders, and a buffer one byte short of the measure is left alone.
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
import { Root, RootCookMeasure, RootCookMeasureReason, RootCookFrom } from "./probeTable.js";
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
const root = new Root();
root.Stamp = 42n;
root.RowsCount = 2;
root.Rows[0].Value = 9n;
root.Rows[0].Flag = true;
root.Rows[1].Value = 3n;
root.Rows[1].Flag = false;
root.OtherCount = 1;
root.Other[0].Value = 11n;
root.Other[0].Flag = true;
const measure = RootCookMeasure(root);
const reason = RootCookMeasureReason(root);
console.log("cook-measure " + measure);
console.log("cook-reason " + reason[0] + " " + reason[1]);
const backing = new Uint8Array(measure);
backing.fill(0xa5);
const short = backing.subarray(0, measure - 1);
const shortOk = RootCookFrom(root, short, 1) === false && backing.every((b) => b === 0xa5);
console.log("cook-short " + (shortOk ? "ok" : "bad"));
function cook(order) {
  const buf = new Uint8Array(measure);
  if (!RootCookFrom(root, buf, order)) { return "refused"; }
  return Buffer.from(buf).toString("hex");
}
console.log("cook-le " + cook(1));
console.log("cook-be " + cook(2));
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
	if len(lines) != 9 {
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
	// §7.6: the same value tabletext reads, cooked by tablecook in both orders.
	// A view one byte short of that measure writes nothing.
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Root"))
	var report tabletext.Report
	text := []byte(`{"stamp":42,"rows":[{"value":9,"flag":true},{"value":3}],"other":[{"value":11,"flag":true}]}`)
	if !m.Read(inst, text, &report) {
		t.Fatalf("oracle text: %+v", report)
	}
	le, err := tablecook.Cook(m, inst, tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	be, err := tablecook.Cook(m, inst, tablecook.Options{Big: true})
	if err != nil {
		t.Fatal(err)
	}
	if lines[4] != fmt.Sprintf("cook-measure %d", len(le)) {
		t.Fatalf("CookMeasure = %q, tablecook answers %d (docs/SPEC-TABLES.md §7.6)", lines[4], len(le))
	}
	if lines[5] != fmt.Sprintf("cook-reason %d null", len(le)) {
		t.Fatalf("CookMeasureReason = %q, want %d and no refusal", lines[5], len(le))
	}
	if lines[6] != "cook-short ok" {
		t.Fatalf("a short buffer was written or CookFrom returned true: %q", lines[6])
	}
	gotLE, err := hex.DecodeString(strings.TrimPrefix(lines[7], "cook-le "))
	if err != nil {
		t.Fatalf("little cook: %v\n%s", err, lines[7])
	}
	gotBE, err := hex.DecodeString(strings.TrimPrefix(lines[8], "cook-be "))
	if err != nil {
		t.Fatalf("big cook: %v\n%s", err, lines[8])
	}
	if !bytes.Equal(gotLE, le) {
		t.Fatalf("little cook differs from tablecook:\n%s", cookDiff(gotLE, le))
	}
	if !bytes.Equal(gotBE, be) {
		t.Fatalf("big cook differs from tablecook:\n%s", cookDiff(gotBE, be))
	}
}

func cookDiff(got, want []byte) string {
	n := len(got)
	if len(want) < n {
		n = len(want)
	}
	for i := 0; i < n; i++ {
		if got[i] != want[i] {
			return fmt.Sprintf("byte %d got %02x want %02x (got %d bytes, want %d)", i, got[i], want[i], len(got), len(want))
		}
	}
	return fmt.Sprintf("length got %d want %d", len(got), len(want))
}
