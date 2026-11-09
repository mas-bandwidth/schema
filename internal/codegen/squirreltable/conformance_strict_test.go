package squirreltable_test

// The squirrel conformance driver owes one strict read per case: the whole
// value, or a refusal. An accepted report, framing, or json-hostile read also
// writes the canonical <case>.wire. This test reads test/conformance/squirrel/
// main.nut. It takes no time, no socket, and no network. Running the
// interpreter is the functional harness.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func squirrelDriverSource(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	path := filepath.Join(root, "test", "conformance", "squirrel", "main.nut")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// squirrelSurface is the driver's one mode block, from its opening line
// through the next top-level `if (mode ==`.
func squirrelSurface(src, marker string) (string, bool) {
	i := strings.Index(src, marker)
	if i < 0 {
		return "", false
	}
	rest := src[i+len(marker):]
	j := strings.Index(rest, "\nif (mode ==")
	if j < 0 {
		j = len(rest)
	}
	return src[i : i+len(marker)+j], true
}

func squirrelDriverStrict(src string) error {
	check := func(marker, label string, want, ban []string) error {
		body, ok := squirrelSurface(src, marker)
		if !ok {
			return fmt.Errorf("%s: driver has no surface %s", label, marker)
		}
		for _, s := range want {
			if !strings.Contains(body, s) {
				return fmt.Errorf("%s: missing %q", label, s)
			}
		}
		for _, s := range ban {
			if strings.Contains(body, s) {
				return fmt.Errorf("%s: still contains %q", label, s)
			}
		}
		return nil
	}
	if err := check(`if (mode == "wire")`, "wire",
		[]string{"strictLoad(", "blob_shared"}, []string{"loadWalk"}); err != nil {
		return err
	}
	if err := check(`if (mode == "report" || mode == "framing")`, "report/framing",
		[]string{"strictLoad(", "writeReadBack("}, []string{"loadWalk"}); err != nil {
		return err
	}
	if err := check(`if (mode == "json-hostile")`, "json-hostile",
		[]string{"FromJson(", "writeReadBack("}, []string{"fromJsonWalk"}); err != nil {
		return err
	}
	if err := check(`if (mode == "cook-write")`, "cook-write",
		[]string{"FromJson(", "strictLoad("}, []string{"loadWalk"}); err != nil {
		return err
	}
	if err := check(`if (mode == "json-read" || mode == "json-write")`, "json-read/json-write",
		[]string{"FromJson(", "strictLoad(", "textValueValid("}, []string{"fromJsonWalk", "loadWalk", `T[root + "Load"]`}); err != nil {
		return err
	}
	if err := check(`if (mode == "retain" || mode == "retain-save")`, "retain",
		[]string{`"read\n"`}, []string{`retained + "," + lost`}); err != nil {
		return err
	}
	if !strings.Contains(src, `cname + ".wire"`) {
		return fmt.Errorf("an accepted read writes no <case>.wire")
	}
	return nil
}

func TestSquirrelDriverReadsStrictAndWritesBack(t *testing.T) {
	t.Parallel()
	if err := squirrelDriverStrict(squirrelDriverSource(t)); err != nil {
		t.Fatal(err)
	}
}

// TestSquirrelDriverReadsStrictAndWritesBackNegativeControl plants loadWalk
// back into the wire surface of the real main.nut. The check has to go red
// on that copy. A driver that never calls strictLoad has nothing to plant.
func TestSquirrelDriverReadsStrictAndWritesBackNegativeControl(t *testing.T) {
	t.Parallel()
	src := squirrelDriverSource(t)
	marker := `if (mode == "wire")`
	at := strings.Index(src, marker)
	if at < 0 {
		t.Fatal("wire surface missing from the real driver")
	}
	rel := strings.Index(src[at:], "strictLoad(")
	if rel < 0 {
		t.Fatal("NEGATIVE CONTROL FAILED: the real driver has no strictLoad in the wire surface, so the fault was not planted in the artifact")
	}
	planted := src[:at+rel] + "loadWalk(" + src[at+rel+len("strictLoad("):]
	if planted == src {
		t.Fatal("NEGATIVE CONTROL FAILED: the plant did not change the real driver")
	}
	if err := squirrelDriverStrict(planted); err == nil {
		t.Fatal("NEGATIVE CONTROL FAILED: planting loadWalk in the real wire surface left the check green")
	}
	// The same plant in the json-read surface: fromJsonWalk back in place
	// of the strict FromJson must also go red.
	marker2 := `if (mode == "json-read" || mode == "json-write")`
	at2 := strings.Index(src, marker2)
	if at2 < 0 {
		t.Fatal("json-read surface missing from the real driver")
	}
	rel2 := strings.Index(src[at2:], "FromJson(")
	if rel2 < 0 {
		t.Fatal("NEGATIVE CONTROL FAILED: the real driver has no FromJson in the json surface, so the fault was not planted in the artifact")
	}
	planted2 := src[:at2+rel2] + "fromJsonWalk(" + src[at2+rel2+len("FromJson("):]
	if planted2 == src {
		t.Fatal("NEGATIVE CONTROL FAILED: the json plant did not change the real driver")
	}
	if err := squirrelDriverStrict(planted2); err == nil {
		t.Fatal("NEGATIVE CONTROL FAILED: planting fromJsonWalk in the real json surface left the check green")
	}
}
