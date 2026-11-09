package squirreltable_test

// vt/text-form and vt/text-form-variable (docs/roadmap-evidence/squirrel.sexp):
// the strict text read — one read: the whole value or a refusal — for the JSON
// text form and for the variable root's &node labels. The cell holds when the
// driver's json surfaces read through the strict FromJson and write the read
// back, and when the runtime's FromJson runs the walk and refuses whatever it
// did not read whole (bodyDamaged, a reason, a clamp, a kind mismatch), the
// variable root reading with a label map so &node defines and references nodes.
// These are source gates over the real artifacts (test/conformance/squirrel/
// main.nut and internal/codegen/squirreltable/runtime/json.nut): they take no
// time, no socket, and no network. Running the interpreter is the functional
// harness.

import (
	"fmt"
	"strings"
	"testing"
)

// vtTextFindings checks the strict text read's rule over the real artifacts.
// It answers every way the rule is not held, empty when it is.
func vtTextFindings(driver, jsonSrc string) []string {
	var out []string
	// json-read / json-write surface: the strict text read of the pinned text,
	// then the save written back; never the walk.
	if body, ok := squirrelSurface(driver, `if (mode == "json-read" || mode == "json-write") {`); ok {
		if !strings.Contains(body, "J.FromJson(") {
			out = append(out, "json-read/json-write: no strict FromJson")
		}
		if strings.Contains(body, "fromJsonWalk(") {
			out = append(out, "json-read/json-write: still reads through fromJsonWalk")
		}
		if strings.Contains(body, "loadWalk") {
			out = append(out, "json-read/json-write: still reads through loadWalk")
		}
		if !strings.Contains(body, "Save") {
			out = append(out, "json-read/json-write: does not write the read back")
		}
	} else {
		out = append(out, "json-read/json-write surface missing")
	}
	// json-hostile surface: the strict verdict alone.
	if body, ok := squirrelSurface(driver, `if (mode == "json-hostile") {`); ok {
		if !strings.Contains(body, "J.FromJson(") {
			out = append(out, "json-hostile: no strict FromJson")
		}
		if strings.Contains(body, "fromJsonWalk(") {
			out = append(out, "json-hostile: still reads through fromJsonWalk")
		}
	} else {
		out = append(out, "json-hostile surface missing")
	}
	// The runtime's strict read: the walk marks what it did not read whole, and
	// FromJson refuses it — the whole value or a refusal. The variable root
	// reads with a label map, which is what makes &node define and reference.
	for _, want := range []string{
		`_M.fromJsonWalk <- function(T, rootName, text, report) {`,
		`if (r.bad || !ok) report.bodyDamaged = true;`,
		`_M.FromJson <- function(T, rootName, text, report) {`,
		`if (reason == null && report.bodyDamaged) reason = "malformed";`,
		`if (reason != null) {`,
		`c.variable ? {} : null`,
	} {
		if !strings.Contains(jsonSrc, want) {
			out = append(out, fmt.Sprintf("json runtime: missing %q", want))
		}
	}
	return out
}

// TestSquirrelVtTextCells holds vt/text-form and vt/text-form-variable to the
// strict text read over the real artifacts: the driver's json surfaces and the
// runtime's FromJson. The cell is done when every finding is absent.
func TestSquirrelVtTextCells(t *testing.T) {
	t.Parallel()
	driver := squirrelDriverSource(t)
	jsonSrc := squirrelJsonRuntimeSource(t)
	if f := vtTextFindings(driver, jsonSrc); len(f) > 0 {
		t.Fatalf("the strict text read's rule is not held: %v", f)
	}
}

// TestSquirrelVtTextCellsNegativeControl requires the strict text read's rule
// to go red when a fault is planted in a copy of a real artifact: the walk put
// back into the driver's json surface, the walk's damage mark removed from the
// runtime, and the variable root's label map dropped.
func TestSquirrelVtTextCellsNegativeControl(t *testing.T) {
	t.Parallel()
	driver := squirrelDriverSource(t)
	jsonSrc := squirrelJsonRuntimeSource(t)
	if f := vtTextFindings(driver, jsonSrc); len(f) > 0 {
		t.Fatalf("unplanted already red: %v", f)
	}

	plants := []struct{ name, src, anchor, repl string }{
		{"json-read walk", driver, "J.FromJson(", "J.fromJsonWalk("},
		{"runtime damage mark", jsonSrc, "if (r.bad || !ok) report.bodyDamaged = true;", "if (false && (r.bad || !ok)) {}"},
		{"variable label map", jsonSrc, "c.variable ? {} : null", "null"},
	}
	for _, pl := range plants {
		if strings.Count(pl.src, pl.anchor) == 0 {
			t.Fatalf("anchor %q not in the real artifact", pl.anchor)
		}
		planted := strings.Replace(pl.src, pl.anchor, pl.repl, 1)
		driverP, jsonP := driver, jsonSrc
		if pl.src == driver {
			driverP = planted
		} else {
			jsonP = planted
		}
		if f := vtTextFindings(driverP, jsonP); len(f) == 0 {
			t.Fatalf("NEGATIVE CONTROL FAILED: planting %s left the strict text read's rule green", pl.name)
		}
	}
}
