package gotable

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestGoFixedBatchHoldsARecord pins the fixed form's at-least-one-record rule
// (docs/FIXED-FORM-ALGORITHM.md §5.3 steps 9 and 10, docs/SPEC-TABLES.md
// §22.6): the committed empty-batch vector is refused by every fixed read
// entry, a file with one record still reads, and a save of zero records is
// refused before it writes a byte. The verdict is the contract; a refusal's
// name is this leg's own diagnostic.
func TestGoFixedBatchHoldsARecord(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "..")
	schema, err := os.ReadFile(filepath.Join(root, "test", "fixed-empty-batch", "Batch.schema"))
	if err != nil {
		t.Fatal(err)
	}
	one, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "fixed-empty-batch", "one-record.bin"))
	if err != nil {
		t.Fatal(err)
	}
	noRecord, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "fixed-empty-batch", "no-record.bin"))
	if err != nil {
		t.Fatal(err)
	}

	source := fmt.Sprintf(`package fixedemptybatch
import ("encoding/hex"; "testing")

const oneRecordHex = %q
const noRecordHex = %q

func file(t *testing.T, text string) []byte {
	t.Helper()
	data, err := hex.DecodeString(text)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestBatchHoldsARecord(t *testing.T) {
	t.Parallel()
	one := file(t, oneRecordHex)
	noRecord := file(t, noRecordHex)

	// A one-record file still reads, through the fixed load and the reads built
	// over it.
	got := make([]Batch, 2)
	plan := make([]TableFixedEntry, BatchFixedPlanMax)
	var report TableReport
	if n := BatchFixedLoad(got, one, plan, &report); n != 1 || report.Err() != nil || got[0].A != 7 || string(got[0].Name[:got[0].NameLength]) != "hello" {
		t.Fatalf("one-record file: n=%%d report=%%+v got=%%+v", n, report, got[0])
	}
	var value Batch
	if !BatchLoad(&value, one, &report) || value.A != 7 || string(value.Name[:value.NameLength]) != "hello" {
		t.Fatalf("one-record Load: value=%%+v report=%%+v", value, report)
	}
	if strict, err := BatchReadStrict(one); err != nil || strict == nil || strict.A != 7 {
		t.Fatalf("one-record ReadStrict: value=%%+v err=%%v", strict, err)
	}

	// A header and a layout followed by no record is refused by every fixed
	// read entry, whatever the destination's capacity.
	for _, capacity := range []int{1, 2, 8} {
		got := make([]Batch, capacity)
		var report TableReport
		if n := BatchFixedLoad(got, noRecord, plan, &report); n != -1 || report.Err() == nil {
			t.Fatalf("empty batch, capacity %%d: n=%%d report=%%+v", capacity, n, report)
		}
	}
	value = Batch{A: 9}
	if BatchLoad(&value, noRecord, &report) || value.A != 0 || report.Err() == nil {
		t.Fatalf("empty batch Load: value=%%+v report=%%+v", value, report)
	}
	if strict, err := BatchReadStrict(noRecord); strict != nil || err == nil {
		t.Fatalf("empty batch ReadStrict: value=%%+v err=%%v", strict, err)
	}

	// A save of zero records is refused before it writes a byte.
	buffer := make([]byte, BatchFixedMeasure(1))
	for i := range buffer {
		buffer[i] = 0xAA
	}
	if n := BatchFixedSave(nil, buffer); n != -1 {
		t.Fatalf("empty save: n=%%d", n)
	}
	for i := range buffer {
		if buffer[i] != 0xAA {
			t.Fatalf("empty save wrote byte %%d = 0x%%02x", i, buffer[i])
		}
	}
	if n, err := BatchFixedSaveReason(nil, buffer); n != -1 || err == nil {
		t.Fatalf("empty save reason: n=%%d err=%%v", n, err)
	}
}
`, hex.EncodeToString(one), hex.EncodeToString(noRecord))

	if out, err := runGeneratedResult(t, string(schema), source); err != nil {
		t.Fatalf("generated fixed empty batch: %v\n%s", err, out)
	}
}
