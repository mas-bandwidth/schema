// A declared fixed table's plain Save writes form 3, the bytes the engine's
// pack writes, and its plain Load and ReadStrict read that file back
// (docs/SPEC-TABLES.md "The three wires": "the plain `<T>Save` of a fixed table
// writes form `3`, the bytes `<T>FixedSave` writes, and so does `schema
// pack`"). The root is RangedSigned (tables/examples/Ranges.schema), int8 to
// int64 fields at their storage limits, through the committed Go reader.
package gotable

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	tabledemo "github.com/mas-bandwidth/schema/v2/internal/codegen/gotable/fuzzunit"
	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

func TestGoFixedRootPlainSaveRoundTrips(t *testing.T) {
	t.Parallel()
	value := tabledemo.RangedSigned{I8Span: -5, I8Low: 126, I32Span: 123456, I32High: -2147483647}
	need := tabledemo.RangedSignedMeasure(&value)
	if need < 1 {
		t.Fatalf("Measure refused a value inside every bound: %d", need)
	}
	wire := make([]byte, need)
	if n := tabledemo.RangedSignedSave(&value, wire); n != need {
		t.Fatalf("Save wrote %d bytes, Measure said %d", n, need)
	}
	if wire[0] != tabledemo.TableFixedForm {
		t.Fatalf("plain Save of a fixed table wrote form %d, want form %d", wire[0], tabledemo.TableFixedForm)
	}

	fixed := make([]byte, tabledemo.RangedSignedFixedMeasure(1))
	if n := tabledemo.RangedSignedFixedSave([]tabledemo.RangedSigned{value}, fixed); n != int64(len(fixed)) || !bytes.Equal(fixed, wire) {
		t.Fatalf("plain Save is not the bytes FixedSave writes:\nsave  %x\nfixed %x", wire, fixed)
	}

	source, err := os.ReadFile(filepath.Join("..", "..", "..", "tables", "examples", "Ranges.schema"))
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(unitFrom(t, string(source)))
	dir := t.TempDir()
	text := `{ "i8_span": -5, "i8_low": 126, "i32_span": 123456, "i32_high": -2147483647 }`
	if err := os.WriteFile(filepath.Join(dir, "RangedSigned.json"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	engine, _, report, err := tablepack.Pack(m, "RangedSigned", dir)
	if err != nil || report != (tabletext.Report{}) {
		t.Fatalf("engine pack: %v %+v", err, report)
	}
	if !bytes.Equal(wire, engine) {
		t.Fatalf("plain Save differs from the engine's pack:\nsave   %x\nengine %x", wire, engine)
	}

	var back tabledemo.RangedSigned
	var read tabledemo.TableReport
	if !tabledemo.RangedSignedLoad(&back, wire, &read) || read.Refused() {
		t.Fatalf("Load of the plain Save's file refused: %+v", read)
	}
	if back != value {
		t.Fatalf("Load: got %+v, want %+v", back, value)
	}
	strict, err := tabledemo.RangedSignedReadStrict(wire)
	if err != nil {
		t.Fatalf("ReadStrict of the plain Save's file: %v", err)
	}
	if *strict != value {
		t.Fatalf("ReadStrict: got %+v, want %+v", *strict, value)
	}
}
