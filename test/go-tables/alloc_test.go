// THE ALLOCATION GATE (docs/SPEC-TABLES.md, "What allocates, and what never
// does"). The generated Go table codecs allocate NOTHING: the caller owns the
// value, the buffer and the report, sub-readers are stack values, and Load
// overlays in place after restoring the declared defaults.
//
// C++ holds that claim by construction — there is no allocator to reach for —
// and the C++ gate is a grep over the emitted header. Go has a garbage
// collector, so a grep proves nothing and the claim has to be MEASURED:
// testing.AllocsPerRun is the measurement, and zero is the number.
//
// It is a gate and not a benchmark: a regression here is a design defect (a
// sub-reader that escaped, a closure that captured, a conversion that boxed),
// and it would never show up as a wrong byte.
package schematables

import (
	"os"
	"testing"

	"tabledemo"
)

// the corpus's richest FIXED root: nested tables, counted arrays of tables,
// strings, bytes, a union, a flags mask, a guarded branch and every scalar
// width. If anything on the read path allocates, this instance finds it.
const representative = "../../testdata/wire/tables/root_full.bin"

// dormantWhileThePortWritesThePreviousForm is the gate's OFF SWITCH while this
// port writes the wire's previous form (docs/SPEC-TABLES.md §3, schema#511).
// The goldens under testdata/wire/tables are the id-table form, and every
// measurement below begins by loading one, so what is absent is the corpus
// these tests measure against and not the measurement. Each test names itself
// here rather than the file going dark, so waking the port wakes them one at a
// time.
func dormantWhileThePortWritesThePreviousForm(t testing.TB) {
	t.Helper()
	t.Skip("dormant — the corpus it gates against is absent while this port writes the wire's previous form (docs/SPEC-TABLES.md §3, schema#511)")
}

func wire(t testing.TB, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the corpus is not built: %v (run `make conformance` first)", err)
	}
	return data
}

func TestLoadAllocatesNothing(t *testing.T) {
	dormantWhileThePortWritesThePreviousForm(t)
	bytes := wire(t, representative)
	var value tabledemo.RootConfig
	var report tabledemo.TableReport
	if n := testing.AllocsPerRun(200, func() {
		report = tabledemo.TableReport{}
		if !tabledemo.RootConfigLoad(&value, bytes, &report) {
			t.Fatal("the golden does not load")
		}
	}); n != 0 {
		t.Errorf("RootConfigLoad allocates %v times per run — the read path owns no memory", n)
	}
	if report != (tabledemo.TableReport{}) {
		t.Errorf("the golden read with events: %+v", report)
	}
}

func TestMeasureAllocatesNothing(t *testing.T) {
	dormantWhileThePortWritesThePreviousForm(t)
	bytes := wire(t, representative)
	var value tabledemo.RootConfig
	if !tabledemo.RootConfigLoad(&value, bytes, nil) {
		t.Fatal("the golden does not load")
	}
	if n := testing.AllocsPerRun(200, func() {
		if tabledemo.RootConfigMeasure(&value) < 0 {
			t.Fatal("measure refused the value")
		}
	}); n != 0 {
		t.Errorf("RootConfigMeasure allocates %v times per run", n)
	}
}

func TestSaveAllocatesNothing(t *testing.T) {
	dormantWhileThePortWritesThePreviousForm(t)
	bytes := wire(t, representative)
	var value tabledemo.RootConfig
	if !tabledemo.RootConfigLoad(&value, bytes, nil) {
		t.Fatal("the golden does not load")
	}
	buffer := make([]byte, tabledemo.RootConfigMeasure(&value))
	if n := testing.AllocsPerRun(200, func() {
		if tabledemo.RootConfigSave(&value, buffer) != int64(len(buffer)) {
			t.Fatal("save did not write measure's answer")
		}
	}); n != 0 {
		t.Errorf("RootConfigSave allocates %v times per run", n)
	}
}

// AND THE ROUND TRIP, which is what a server actually runs: load the caller's
// bytes, measure, save back. Zero for the three separately and non-zero for
// the sequence would mean something escaped across the call boundary.
func TestRoundTripAllocatesNothing(t *testing.T) {
	dormantWhileThePortWritesThePreviousForm(t)
	bytes := wire(t, representative)
	var value tabledemo.RootConfig
	var report tabledemo.TableReport
	buffer := make([]byte, len(bytes)*2)
	if n := testing.AllocsPerRun(200, func() {
		report = tabledemo.TableReport{}
		if !tabledemo.RootConfigLoad(&value, bytes, &report) {
			t.Fatal("the golden does not load")
		}
		size := tabledemo.RootConfigMeasure(&value)
		if tabledemo.RootConfigSave(&value, buffer[:size]) != size {
			t.Fatal("save did not write measure's answer")
		}
	}); n != 0 {
		t.Errorf("the load/measure/save round trip allocates %v times per run", n)
	}
}

// sink is where the negative control's allocations escape to. Nothing else
// reads it: its only job is to be a place the compiler cannot prove a value
// does not reach, so the allocation is real.
var sink any

// TestAllocationGateCanGoRed is the negative control the gates above need, and
// it is not optional: `testing.AllocsPerRun` reports ZERO for a body whose
// allocation the compiler proved does not escape, so a gate written without one
// can read zero because it is measuring nothing. Two shapes, both of them what
// a regression here would actually look like — a buffer that escaped, and a
// sub-reader that escaped — and both must be seen.
func TestAllocationGateCanGoRed(t *testing.T) {
	if n := testing.AllocsPerRun(200, func() { sink = make([]byte, 64) }); n == 0 {
		t.Error("an escaping allocation measured as zero — the gate above is watching nothing")
	}
	bytes := wire(t, representative)
	if n := testing.AllocsPerRun(200, func() {
		r := tabledemo.TableReader{Buffer: bytes}
		sink = &r
	}); n == 0 {
		t.Error("an escaping sub-reader measured as zero — the gate above is watching nothing")
	}
}

// AND THE TEXT FORM, both ways (docs/SPEC-TABLES.md §16). It is the same claim
// as the wire's and it is easier to break: the walk is generic over the
// descriptors, so a value handed to it through an `any`, a closure that
// captured a slot, or a number formatted through `fmt` rather than `strconv`'s
// Append* would each allocate without moving a byte of output.
func TestToJsonAllocatesNothing(t *testing.T) {
	dormantWhileThePortWritesThePreviousForm(t)
	bytes := wire(t, representative)
	var value tabledemo.RootConfig
	var report tabledemo.TableReport
	if !tabledemo.RootConfigLoad(&value, bytes, &report) {
		t.Fatal("the golden does not load")
	}
	buffer := make([]byte, tabledemo.RootConfigToJsonMeasure(&value))
	if n := testing.AllocsPerRun(200, func() {
		if tabledemo.RootConfigToJsonMeasure(&value) != int64(len(buffer)) {
			t.Fatal("measure is not stable")
		}
		if tabledemo.RootConfigToJson(&value, buffer) != int64(len(buffer)) {
			t.Fatal("write did not fill measure's answer")
		}
	}); n != 0 {
		t.Errorf("the text form's measure and write allocate %v times per run", n)
	}
}

func TestFromJsonAllocatesNothing(t *testing.T) {
	dormantWhileThePortWritesThePreviousForm(t)
	bytes := wire(t, representative)
	var value tabledemo.RootConfig
	var report tabledemo.TableReport
	if !tabledemo.RootConfigLoad(&value, bytes, &report) {
		t.Fatal("the golden does not load")
	}
	text := make([]byte, tabledemo.RootConfigToJsonMeasure(&value))
	tabledemo.RootConfigToJson(&value, text)

	var parsed tabledemo.RootConfig
	if n := testing.AllocsPerRun(200, func() {
		report = tabledemo.TableReport{}
		if !tabledemo.RootConfigFromJson(&parsed, text, &report) {
			t.Fatal("the text this port wrote does not read back")
		}
	}); n != 0 {
		t.Errorf("the text form's read allocates %v times per run", n)
	}
}
