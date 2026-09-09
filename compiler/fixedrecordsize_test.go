// §3.4'S TWO SIZE BOUNDS, and the flag that turns the first into a gate
// (docs/SPEC-TABLES.md §3.4). *"Effectively, fixed tables should only be used
// for small things"* — the project owner. A design rule nobody is told about is
// not a rule, so what is measured here is that the compiler SAYS it, with the
// TABLE'S NAME and the SIZE in the message, at each of three bounds that mean
// three different things:
//
//	4096   ADVISORY, always on. The table still carries the fixed form.
//	65536  THE WIRE'S CEILING. The fixed form is not emitted for the table at
//	       all, because a reader holds an untrusted peer's layout to the same
//	       number and would refuse the record; the table keeps form 1.
//	--fixed-record-limit  A PROJECT'S POLICY, off by default. Past it the unit
//	       does not compile.
package compiler

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// fixedSizeUnit writes one table whose record body is a byte buffer of the
// given capacity plus its four-byte length, so the size in the message is a
// number this test computed and not one it read back out of the code it is
// testing.
func fixedSizeUnit(t *testing.T, name string, payload int) (dir string, body int64) {
	t.Helper()
	dir = t.TempDir()
	src := fmt.Sprintf("package probe\n\ntable %s\n{\n    payload bytes(%d)\n}\n", name, payload)
	if err := os.WriteFile(filepath.Join(dir, "Probe.schema"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, int64(payload) + ir.TableFixedCountBytes
}

func loadWithWarnings(t *testing.T, dir string, limit int64) (warns []string, err error) {
	t.Helper()
	paths, perr := GatherPaths([]string{dir})
	if perr != nil {
		t.Fatal(perr)
	}
	c := New()
	c.FixedRecordLimit = limit
	c.OnWarn = func(msg string) { warns = append(warns, msg) }
	_, err = c.Load(paths)
	return warns, err
}

// A SMALL FIXED TABLE SAYS NOTHING. The advisory has to be silent on the shape
// this form is for, or it is noise rather than advice.
func TestFixedRecordUnderTheAdvisoryBoundIsSilent(t *testing.T) {
	dir, _ := fixedSizeUnit(t, "Small", 64)
	warns, err := loadWithWarnings(t, dir, 0)
	if err != nil {
		t.Fatalf("a small fixed table must compile: %v", err)
	}
	if len(warns) != 0 {
		t.Errorf("a 68-byte fixed record must warn about nothing: %v", warns)
	}
}

// PAST 4096: A WARNING NAMING THE TABLE AND THE SIZE, and an exit code that
// does not move. The table still carries the form — this bound is about the
// declaration's shape and nothing on the wire changes at it.
func TestFixedRecordPastTheAdvisoryBoundWarns(t *testing.T) {
	dir, body := fixedSizeUnit(t, "Chunky", ir.TableFixedRecordWarnBytes)
	warns, err := loadWithWarnings(t, dir, 0)
	if err != nil {
		t.Fatalf("the advisory bound must not fail a compile: %v", err)
	}
	if len(warns) != 1 {
		t.Fatalf("exactly one advisory, got %d: %v", len(warns), warns)
	}
	for _, want := range []string{"Chunky", fmt.Sprint(body), fmt.Sprint(ir.TableFixedRecordWarnBytes), "§3.4"} {
		if !strings.Contains(warns[0], want) {
			t.Errorf("the advisory must carry %q — a warning that names neither the table nor the size is one nobody can act on: %s", want, warns[0])
		}
	}
	// AND THE TABLE STILL CARRIES THE FORM: the bound is advice, not a gate.
	if !strings.Contains(generatedFixedForm(t, dir), "ChunkyFixedLayout") {
		t.Error("a table past the ADVISORY bound must still carry the fixed form")
	}
}

// PAST 65536: THE FORM IS NOT EMITTED, and the compiler says so. A reader holds
// an untrusted peer's layout to the same number (layout_record_too_large), so a
// writer past it would produce bytes no conforming reader decodes.
func TestFixedRecordPastTheWireCeilingLosesTheForm(t *testing.T) {
	dir, body := fixedSizeUnit(t, "Huge", ir.TableFixedRecordMaxBytes)
	warns, err := loadWithWarnings(t, dir, 0)
	if err != nil {
		t.Fatalf("the wire ceiling refuses the FORM and never the unit: %v", err)
	}
	if len(warns) != 1 {
		t.Fatalf("exactly one report, got %d: %v", len(warns), warns)
	}
	for _, want := range []string{"Huge", fmt.Sprint(body), fmt.Sprint(ir.TableFixedRecordMaxBytes), "form 1"} {
		if !strings.Contains(warns[0], want) {
			t.Errorf("the ceiling report must carry %q: %s", want, warns[0])
		}
	}
	// NOT IN SILENCE, AND NOT AT ALL: no layout, no writer, no reader.
	if got := generatedFixedForm(t, dir); strings.Contains(got, "HugeFixedLayout") {
		t.Error("a table past the WIRE CEILING must not carry the fixed form: a reader would refuse the record it writes")
	}
}

// --fixed-record-limit IS THE PROJECT'S OWN GATE, and it fails the compile. It
// is off by default, which is why every test above passes 0.
func TestFixedRecordLimitRefusesTheCompile(t *testing.T) {
	dir, body := fixedSizeUnit(t, "Chunky", ir.TableFixedRecordWarnBytes)
	if _, err := loadWithWarnings(t, dir, 0); err != nil {
		t.Fatalf("the gate is OFF by default: %v", err)
	}
	_, err := loadWithWarnings(t, dir, ir.TableFixedRecordWarnBytes)
	if err == nil {
		t.Fatal("--fixed-record-limit must FAIL the compile: a gate that only warns is the advisory again")
	}
	diags, ok := errors.AsType[Diagnostics](err)
	if !ok || len(diags) != 1 {
		t.Fatalf("one diagnostic per table, got %v", err)
	}
	for _, want := range []string{"Chunky", fmt.Sprint(body), "fixed-record-limit", "§3.4"} {
		if !strings.Contains(diags[0].Error(), want) {
			t.Errorf("the refusal must carry %q: %s", want, diags[0])
		}
	}
	// AND A LIMIT THE TABLE IS INSIDE CHANGES NOTHING.
	if _, err := loadWithWarnings(t, dir, ir.TableFixedRecordMaxBytes); err != nil {
		t.Fatalf("a limit the table is inside must not refuse it: %v", err)
	}
}

// generatedFixedForm is the C++ table header the unit emits, which is where the
// fixed form either appears or does not.
func generatedFixedForm(t *testing.T, dir string) string {
	t.Helper()
	paths, err := GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	files, err := New().Generate(u, "cpp", Options{})
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for name, content := range files {
		if strings.HasSuffix(name, "Table.h") {
			all.WriteString(string(content))
		}
	}
	return all.String()
}
