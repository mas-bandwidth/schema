//go:build schema_leg_python

package compiler

// SPEC.md §4.13, docs/SPEC-TABLES.md §2.11: the Python target carries the
// `timestamp` kind, an int64 of nanoseconds since the epoch, on the form-1
// table wire. This holds the compiler's carry registration and the witness
// unit at testdata/conformance/feature-witnesses/timestamp: the target
// generates it and refuses nothing for the construct.

import (
	"os"
	"strings"
	"testing"
)

func init() {
	nonGoCarriers["python"] = append(nonGoCarriers["python"], carryTimestamp)
}

func TestPythonCarriesTimestamp(t *testing.T) {
	t.Parallel()
	if !carries("python", carryTimestamp) {
		t.Fatal("python does not carry timestamp")
	}
	src, err := os.ReadFile("../testdata/conformance/feature-witnesses/timestamp/Probe.schema")
	if err != nil {
		t.Fatal(err)
	}
	files, err := New().Generate(unitFromSource(t, string(src)), "python", Options{})
	if err != nil || len(files) == 0 {
		t.Fatalf("python must carry the timestamp witness: files=%d err=%v", len(files), err)
	}
	module := string(files["probe/Probe.py"])
	for _, want := range []string{
		"class Root:",
		"kind=36,",
		`codec="int",`,
		"lo=-9223372036854775808,",
		"hi=9223372036854775807,",
	} {
		if !strings.Contains(module, want) {
			t.Errorf("the generated timestamp witness lacks %q:\n%s", want, module)
		}
	}
}
