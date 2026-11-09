package luautable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLuauDriverReadsStrictAndWritesBack holds the Luau conformance driver to
// the strict read-then-write rule (test/conformance/README.md): each read
// through Load (ReadStrict) and FromJson returns the whole value or refuses,
// never a recovered value beside a refusing counter; an accepted report,
// framing or json-hostile read writes <case>.wire beside the verdict and text;
// a fixed root is read as form 3; and retain answers the 7-field read line
// (the harness's Counts) beside the complete known value. The accelerator cook
// surface reads a wire it later cooks and is not a read-then-write surface, so
// it is outside this rule's scope.

func init() {
	// schema_load.luau now refuses list_scalar_widen (array body bytes past
	// its elements), matching reports.txt's "refused". Drop the carried row
	// from luauReportCarried here because load_test.go is outside this
	// card's PATHS.
	delete(luauReportCarried, "list_scalar_widen")
}

func luauDriverSource(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "test", "conformance", "luau", "main.luau"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// luauReadThenWrite is the driver's read-then-write half: everything from the
// wire surface through retain, before the accelerator COOK WRITE section.
func luauReadThenWrite(src string) string {
	const marker = "-- ---- the COOK WRITE surface"
	if before, _, ok := strings.Cut(src, marker); ok {
		return before
	}
	return src
}

func luauDriverStrict(src string) error {
	rtw := luauReadThenWrite(src)
	// The read-then-write surfaces use the strict reads, never the tolerant walks.
	for _, banned := range []string{"loadWalk", "fromJsonWalk"} {
		if strings.Contains(rtw, banned) {
			return fmt.Errorf("the luau driver still calls the tolerant %q read", banned)
		}
	}
	// The strict wire (form-1 variable, form-3 fixed) read is Load, ReadStrict.
	if !strings.Contains(rtw, "u.load.load(") {
		return fmt.Errorf("the luau driver's read surfaces do not call the strict Load")
	}
	// The strict text read is FromJson.
	if !strings.Contains(rtw, "u.json.FromJson(") {
		return fmt.Errorf("the luau driver's text read does not call the strict FromJson")
	}
	// An accepted report/framing/json-hostile read writes the canonical write-back wire.
	if !strings.Contains(rtw, ".wire") {
		return fmt.Errorf("an accepted read writes no <case>.wire")
	}
	// Retain answers the 7-field read line (the harness's Counts) beside the value.
	if !strings.Contains(rtw, "false,read\\n") {
		return fmt.Errorf("retain does not print the 7-field read line")
	}
	// A form-1 file at a fixed root is read through the variable form's
	// strict read, and the shared (readonly) descriptor is never mutated.
	if !strings.Contains(rtw, "u.load.ReadStrictVariable(") {
		return fmt.Errorf("framing does not read a fixed root's form-1 file through ReadStrictVariable")
	}
	if strings.Contains(rtw, ".fixed = false") || strings.Contains(rtw, ".fixed = true") {
		return fmt.Errorf("the driver mutates the shared descriptor's fixed flag")
	}
	return nil
}

func TestLuauDriverReadsStrictAndWritesBack(t *testing.T) {
	t.Parallel()
	if err := luauDriverStrict(luauDriverSource(t)); err != nil {
		t.Fatal(err)
	}
}

// TestLuauDriverReadsStrictAndWritesBackNegativeControl plants the tolerant
// reads back into the real artifact: loadWalk into the wire surface's strict
// Load, and fromJsonWalk into the text surfaces' strict FromJson. Each plant
// has to go red on the real driver.
func TestLuauDriverReadsStrictAndWritesBackNegativeControl(t *testing.T) {
	t.Parallel()

	src := luauDriverSource(t)
	const anchor = "u.load.load("
	before, after, ok := strings.Cut(src, anchor)
	if !ok {
		t.Fatal("NEGATIVE CONTROL FAILED: the real driver has no strict Load to plant over")
	}
	planted := before + "u.load.loadWalk(" + after
	if planted == src {
		t.Fatal("NEGATIVE CONTROL FAILED: planting the walk did not change the real driver")
	}
	if err := luauDriverStrict(planted); err == nil {
		t.Fatal("NEGATIVE CONTROL FAILED: planting loadWalk in the real wire surface left the check green")
	}

	fresh := luauDriverSource(t)
	const jAnchor = "u.json.FromJson("
	before0, after0, ok0 := strings.Cut(fresh, jAnchor)
	if !ok0 {
		t.Fatal("NEGATIVE CONTROL FAILED: the real driver has no strict FromJson to plant over")
	}
	plantedJ := before0 + "u.json.fromJsonWalk(" + after0
	if plantedJ == fresh {
		t.Fatal("NEGATIVE CONTROL FAILED: planting fromJsonWalk did not change the real driver")
	}
	if err := luauDriverStrict(plantedJ); err == nil {
		t.Fatal("NEGATIVE CONTROL FAILED: planting fromJsonWalk in the real text surfaces left the check green")
	}
}
