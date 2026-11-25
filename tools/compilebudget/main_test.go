package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mas-bandwidth/schema/v2/internal/testinputs"
)

// recordedBudget and recordedSeconds are the shape the Makefile records beside
// CONFORMANCE_DRIVER_TU_BUDGET: the budget, and each unit's CPU seconds from
// the cold leg measured on captain (2026-11-25, make -j4 on 4 cores). No test
// here times anything: the timings are data.
const recordedBudget = 15.0

var recordedSeconds = map[string]string{
	"driver_maps":        "9.17",
	"driver_examples":    "8.43",
	"driver_backend":     "6.37",
	"driver_lists":       "6.12",
	"driver_tblsurf":     "5.64",
	"driver_cooktorture": "5.39",
	"driver_varevo12":    "5.36",
	"driver_messages":    "4.99",
	"driver_varevo3":     "4.90",
	"driver_pointers":    "4.88",
	"driver_wref":        "4.84",
	"driver_evolution_b": "3.94",
	"driver_evolution_a": "3.57",
	"driver_blocks":      "2.60",
	"driver_codec":       "1.19",
	"main":               "0.74",
}

func recordedFS(units []string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for _, u := range units {
		fsys[u+".seconds"] = &fstest.MapFile{Data: []byte(recordedSeconds[u] + "\n")}
	}
	return fsys
}

// makeVariable reads one `NAME := value` assignment of the repository's
// Makefile, backslash continuations joined.
func makeVariable(t *testing.T, name string) string {
	t.Helper()
	testinputs.Files(t, "Makefile")
	data, err := os.ReadFile(filepath.Join(testinputs.Root(t), "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(data), "\\\n", " ")
	for _, line := range strings.Split(text, "\n") {
		if value, ok := strings.CutPrefix(line, name+" := "); ok {
			return strings.Join(strings.Fields(value), " ")
		}
	}
	t.Fatalf("the Makefile assigns no %s", name)
	return ""
}

// TestTheRecordedShapeIsTheMakefiles holds the shape below to the build: the
// same units the driver compiles, and the same budget the produce job checks.
func TestTheRecordedShapeIsTheMakefiles(t *testing.T) {
	units := strings.Fields(makeVariable(t, "CONFORMANCE_DRIVER_TUS"))
	if len(units) != len(recordedSeconds) {
		t.Errorf("the Makefile names %d units, the recorded shape %d", len(units), len(recordedSeconds))
	}
	for _, u := range units {
		if _, ok := recordedSeconds[u]; !ok {
			t.Errorf("unit %s has no recorded seconds", u)
		}
	}
	budget, err := strconv.ParseFloat(makeVariable(t, "CONFORMANCE_DRIVER_TU_BUDGET"), 64)
	if err != nil || budget != recordedBudget {
		t.Errorf("CONFORMANCE_DRIVER_TU_BUDGET is %v (%v), the recorded budget %v", budget, err, recordedBudget)
	}
}

// TestCheckPassesTheRecordedShape: every unit as measured is within budget.
func TestCheckPassesTheRecordedShape(t *testing.T) {
	units := strings.Fields(makeVariable(t, "CONFORMANCE_DRIVER_TUS"))
	report, err := check(recordedFS(units), "objs", recordedBudget, units)
	if err != nil {
		t.Fatalf("the recorded shape is red: %v\n%s", err, report)
	}
	if !strings.Contains(report, "every one within") || !strings.Contains(report, "driver_maps") {
		t.Errorf("the report does not list the units it held: %s", report)
	}
}

// TestCheckRedsAUnitOverBudget is the plant: one unit past the budget is red,
// and the error names it and its seconds.
func TestCheckRedsAUnitOverBudget(t *testing.T) {
	units := []string{"main", "driver_maps", "driver_codec"}
	fsys := recordedFS(units)
	fsys["driver_maps.seconds"] = &fstest.MapFile{Data: []byte("15.01\n")}
	_, err := check(fsys, "objs", recordedBudget, units)
	if err == nil {
		t.Fatal("a unit at 15.01 s against a 15 s budget passed")
	}
	if !strings.Contains(err.Error(), "unit driver_maps took 15.01 s") {
		t.Errorf("the error does not name the unit and its seconds: %v", err)
	}
	if strings.Contains(err.Error(), "driver_codec") {
		t.Errorf("the error names a unit within budget: %v", err)
	}
}

// TestCheckRedsAMissingTiming: a unit with no timing file is red by name,
// never a pass.
func TestCheckRedsAMissingTiming(t *testing.T) {
	units := []string{"main", "driver_maps"}
	fsys := recordedFS(units)
	delete(fsys, "driver_maps.seconds")
	_, err := check(fsys, "objs", recordedBudget, units)
	if err == nil || !strings.Contains(err.Error(), "objs/driver_maps.seconds: no timing for unit driver_maps") {
		t.Fatalf("a missing timing file is not red by name: %v", err)
	}
}

// TestCheckRedsAnUnreadableTiming: a timing that is not a number, or a unit
// named twice, or no unit or budget at all, is red.
func TestCheckRedsAnUnreadableTiming(t *testing.T) {
	units := []string{"main"}
	fsys := fstest.MapFS{"main.seconds": &fstest.MapFile{Data: []byte("fast\n")}}
	if _, err := check(fsys, "objs", recordedBudget, units); err == nil || !strings.Contains(err.Error(), `"fast" is not a number`) {
		t.Errorf("an unreadable timing is not red: %v", err)
	}
	if _, err := check(recordedFS(units), "objs", recordedBudget, []string{"main", "main"}); err == nil || !strings.Contains(err.Error(), "named twice") {
		t.Errorf("a unit named twice is not red: %v", err)
	}
	if _, err := check(recordedFS(units), "objs", recordedBudget, nil); err == nil {
		t.Error("no unit at all passed")
	}
	if _, err := check(recordedFS(units), "objs", 0, units); err == nil {
		t.Error("a zero budget passed")
	}
}
