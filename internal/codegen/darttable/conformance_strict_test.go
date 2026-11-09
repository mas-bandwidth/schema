package darttable_test

// The Dart conformance driver reads strict and writes back: a fixed root is
// read through its one strict read, an accepted report, framing, or json-hostile
// read owes `read\n` plus the complete text value and the canonical write-back
// wire (<case>.wire), a refused read owes only the refusal, and retain answers
// with the known value in the same shape. This test checks test/conformance/
// dart/main.dart, the real driver artifact. It takes no time, no socket, and no
// network; running the Dart interpreter is the functional harness.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func dartRepoFile(t *testing.T, rel ...string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	path := filepath.Join(append([]string{root}, rel...)...)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func dartDriverSource(t *testing.T) string {
	t.Helper()
	return dartRepoFile(t, "test", "conformance", "dart", "main.dart")
}

// dartFunction returns one surface function's span, from its signature to the
// brace that closes its body.
func dartFunction(src, sig string) (string, bool) {
	i := strings.Index(src, sig)
	if i < 0 {
		return "", false
	}
	open := strings.Index(src[i:], "{")
	if open < 0 {
		return "", false
	}
	open += i
	depth := 0
	for k := open; k < len(src); k++ {
		switch src[k] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[i : k+1], true
			}
		}
	}
	return "", false
}

func dartDriverStrict(src string) error {
	check := func(sig string, want, ban []string) error {
		body, ok := dartFunction(src, sig)
		if !ok {
			return fmt.Errorf("%s: driver has no such surface", sig)
		}
		for _, s := range want {
			if !strings.Contains(body, s) {
				return fmt.Errorf("%s: missing %q", sig, s)
			}
		}
		for _, s := range ban {
			if strings.Contains(body, s) {
				return fmt.Errorf("%s: still contains %q", sig, s)
			}
		}
		return nil
	}
	// Every accepted read writes the canonical write-back wire beside the
	// answer: the whole value is the JSON text AND the wire.
	if err := check("int surfaceReport(", []string{".wire'"}, nil); err != nil {
		return err
	}
	if err := check("int surfaceFraming(", []string{".wire'"}, nil); err != nil {
		return err
	}
	if err := check("int surfaceJsonHostile(", []string{".wire'"}, nil); err != nil {
		return err
	}
	// Retain owes `read\n` plus the complete known value, or the refusal
	// alone; it never answers with retention counters.
	if err := check("int surfaceRetain(",
		[]string{`'read\n$`, `'refused\n'`},
		[]string{"'${report.retained}'"}); err != nil {
		return err
	}
	if !strings.Contains(src, ".wire'") {
		return fmt.Errorf("an accepted read writes no <case>.wire")
	}
	return nil
}

func TestDartDriverReadsStrictAndWritesBack(t *testing.T) {
	t.Parallel()
	if err := dartDriverStrict(dartDriverSource(t)); err != nil {
		t.Fatal(err)
	}
}

// TestDartDriverReadsStrictAndWritesBackNegativeControl plants, in a copy of
// the real main.dart, the two faults the check exists to catch: a surface that
// stops writing the write-back wire, and a retain surface that answers with
// counters instead of `read\n` plus the value. Each check has to go red on its
// copy.
func TestDartDriverReadsStrictAndWritesBackNegativeControl(t *testing.T) {
	t.Parallel()
	src := dartDriverSource(t)
	// Plant 1: the wire write-backs disappear from the real driver.
	if !strings.Contains(src, ".wire'") {
		t.Fatal("NEGATIVE CONTROL FAILED: the real driver has no .wire write-back to plant a fault in")
	}
	noWire := strings.ReplaceAll(src, ".wire'", ".w'")
	if noWire == src {
		t.Fatal("NEGATIVE CONTROL FAILED: the .wire plant did not change the real driver")
	}
	if err := dartDriverStrict(noWire); err == nil {
		t.Fatal("NEGATIVE CONTROL FAILED: dropping the write-back wires left the check green")
	}
	// Plant 2: the retain surface answers with counters instead of the known
	// value, in the real driver.
	anchor := `'read\n${tblrt1.nodeToJson(root)}'`
	if !strings.Contains(src, anchor) {
		t.Fatal("NEGATIVE CONTROL FAILED: the real retain surface has no read+value answer to plant a fault in")
	}
	regressed := strings.Replace(src, anchor, `'${report.retained},${report.retainLost},${report.unknown} ${save.retainLost}\n'`, 1)
	if regressed == src {
		t.Fatal("NEGATIVE CONTROL FAILED: the retain plant did not change the real driver")
	}
	if err := dartDriverStrict(regressed); err == nil {
		t.Fatal("NEGATIVE CONTROL FAILED: the retain counters answer left the check green")
	}
}
