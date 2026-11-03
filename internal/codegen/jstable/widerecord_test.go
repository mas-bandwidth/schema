package jstable

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/memceiling"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// wideRecordDir holds a record past 4 GiB (docs/SPEC-TABLES.md §11): every
// leg generates and compiles it, and holds each stored type size, field
// offset and element size at 64 bits. Huge is three 2 GiB parts and a field
// past 4 GiB.
const wideRecordDir = "../../../testdata/wire/tables/storage-cap/wide"

// wideRecordWire is a Root whose items list holds one empty Huge body: its
// region measure commands one Huge, past 4 GiB.
func wideRecordWire(u *ir.Unit) []byte {
	root := u.Tables["Root"]
	list := []byte{byte(ir.TableKindTable), 1, 1, 0}
	wire := []byte{ir.TableWireForm, 1, byte(ir.TableKindArray)}
	wire = binary.AppendUvarint(wire, uint64(len(list)))
	wire = append(append(wire, list...), 0)
	wire = binary.LittleEndian.AppendUint64(wire, ir.TableFieldWireId(root.Fields[0]))
	return binary.LittleEndian.AppendUint64(wire, 1)
}

// TestWideRecordJavaScript: every module the unit generates imports, and the
// emitted facts of the record past 4 GiB are exact: HugeRow.Size and
// HugeTableType.sz are Huge's size, HugeRow.Tail reads at Huge.tail's offset,
// and the region measure of a Root holding one Huge is the oracle's. No Huge
// is built: Tail reads a small view at a base that puts the offset at 0.
func TestWideRecordJavaScript(t *testing.T) {
	slowtest.Gate(t, "node (it runs the generated module)")
	memceiling.Guard(t, memceiling.Default)
	if out, err := runWideRecordJS(t, nil); err != nil {
		t.Fatalf("wide record probe: %v\n%s", err, out)
	}
}

// TestWideRecordJavaScriptNegativeControl narrows Huge's size to 32 bits in
// the generated cook module and requires the probe to go red.
func TestWideRecordJavaScriptNegativeControl(t *testing.T) {
	slowtest.Gate(t, "node (it runs the generated module)")
	memceiling.Guard(t, memceiling.Default)
	out, err := runWideRecordJS(t, func(name, src string) string {
		if !strings.HasSuffix(name, "Cook.js") || !strings.Contains(src, "const Size = 6442450952;") {
			return src
		}
		return strings.Replace(src, "const Size = 6442450952;", "const Size = 6442450952 >>> 0;", 1)
	})
	if err == nil || !strings.Contains(string(out), "HugeRow.Size") {
		t.Fatalf("a 32-bit Huge size stayed green: %v\n%s", err, out)
	}
}

func runWideRecordJS(t *testing.T, edit func(name, src string) string) ([]byte, error) {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(wideRecordDir, "Wide.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := loadSchemaString(t, "Wide.schema", string(src))
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	huge := ir.RecordLayout(u, u.Tables["Huge"])
	var tail int64 = -1
	for _, f := range huge.Fields {
		if f.Field.Name == "tail" {
			tail = f.Offset
		}
	}
	wire := wideRecordWire(u)
	measure, whole, refused := tablewire.FileRegionMeasure(u, u.Tables["Root"], wire)
	if !whole || refused || huge.Size <= 1<<32 || tail <= 1<<32 || measure <= huge.Size {
		t.Fatalf("the oracle's facts are not past 4 GiB: size %d tail %d measure %d", huge.Size, tail, measure)
	}
	work := t.TempDir()
	var imports strings.Builder
	cook := ""
	for name, b := range files {
		if strings.Contains(name, "/") || !strings.HasSuffix(name, ".js") {
			continue
		}
		text := string(b)
		if edit != nil {
			text = edit(name, text)
		}
		if err := os.WriteFile(filepath.Join(work, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&imports, "import * as m%d from %q;\n", imports.Len(), "./"+name)
		if strings.HasSuffix(name, "Cook.js") && strings.Contains(text, "export const HugeRow") {
			cook = name
		}
	}
	if cook == "" {
		t.Fatal("no generated module defines HugeRow")
	}
	probe := imports.String() + fmt.Sprintf(`import { readFileSync } from "node:fs";
import { HugeRow } from "./%s";
import { HugeTableType, RootLoadMeasureReason } from "./%s";
let failures = 0;
function check(ok, what) { if (!ok) { console.log("FAIL: " + what); failures++; } }
check(HugeRow.Size === %d, "HugeRow.Size " + HugeRow.Size);
check(HugeTableType.sz === %d, "HugeTableType.sz " + HugeTableType.sz);
const view = new DataView(new ArrayBuffer(8));
view.setInt32(0, 0x12345678, true);
check(HugeRow.Tail(view, -%d) === 0x12345678, "HugeRow.Tail does not read at Huge.tail's offset");
const [size, why] = RootLoadMeasureReason(new Uint8Array(readFileSync("wide.bin")));
check(size === %d && why === null, "measure " + size + " " + why);
if (failures) { process.exit(1); }
console.log("ok");
`, cook, runtimeHome(u)+"Variable.js", huge.Size, huge.Size, tail, measure)
	if err := os.WriteFile(filepath.Join(work, "probe.mjs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "wide.bin"), wire, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(jsNode(t), "--max-old-space-size=3072", "probe.mjs")
	cmd.Dir = work
	return cmd.CombinedOutput()
}
