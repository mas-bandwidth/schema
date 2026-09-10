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
//	       number and would refuse the record; the table keeps form 1. The
//	       `fixed` KEYWORD DOES NOT MAKE THIS A REFUSAL — it declares the
//	       CLASS and this bound is the FORM's; see the last test in the file.
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
	src := fmt.Sprintf("package probe\n\nfixed table %s\n{\n    payload bytes(%d)\n}\n", name, payload)
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

// A DECLARED FIXED TABLE PAST THE CEILING KEEPS FORM 1 AND IS NAMED — it does
// NOT fail the compile (docs/SPEC-TABLES.md §3.4, #823). This is the one place
// the keyword's *"if we add any feature that stops it from being fixed, it is a
// compile error"* does not reach, and the reason is that the keyword declares
// the CLASS and this bound belongs to the FORM:
//
//   - THE CLASS is a by-value storage, a cook, a block form (§19) and no arena
//     edge anywhere in the closure. A 7.5 MB record is all of those things.
//   - THE FORM is form `3` on the tolerant wire, and 65536 is what a PEER's
//     reader holds an untrusted layout to (`layout_record_too_large`).
//
// §12.1's render frame and §2.8's `WideBlob` are DECLARED `fixed table` in this
// tree — they have to be, because that is what gives them their storage and
// their block form — and they were never form-`3` tables at all, form `3` being
// younger than both. Refusing them would be refusing the class over one of its
// wires. What the keyword DOES refuse is a construct that makes the body vary
// in size (§2.2, `ir.FixedClosureBreaks`), which a large record is not.
//
// AND IT IS NOT SILENT, which is the whole of the owner's rule: the table and
// its size are in the message, at compile time, always on.
func TestDeclaredFixedTablePastTheCeilingKeepsFormOneAndIsNamed(t *testing.T) {
	dir, body := fixedSizeUnit(t, "Huge", ir.TableFixedRecordMaxBytes)
	paths, err := GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := New().Load(paths)
	if err != nil {
		t.Fatalf("the ceiling names the table; it never refuses the unit: %v", err)
	}
	st := u.Tables["Huge"]
	if st == nil {
		t.Fatal("the probe declares Huge")
	}
	// THE PROBE SAYS `fixed table`, so the parser has already set the flag —
	// nothing in this file reaches into the IR to fake it any more.
	if !st.FixedDeclared {
		t.Fatal("`fixed table Huge` must arrive DECLARED: the keyword is the class")
	}
	warns, errs := ir.TableFixedRecordBounds(u, 0)
	if len(errs) != 0 {
		t.Fatalf("the ceiling is the FORM's bound and refuses no unit: %v", errs)
	}
	if len(warns) != 1 {
		t.Fatalf("exactly one report, got %d: %v", len(warns), warns)
	}
	for _, want := range []string{"Huge", fmt.Sprint(body), fmt.Sprint(ir.TableFixedRecordMaxBytes), "§3.4", "form 1", "CLASS"} {
		if !strings.Contains(warns[0], want) {
			t.Errorf("the report must carry %q — the class is what the table keeps: %s", want, warns[0])
		}
	}
}

// AND THE CORPUS'S OWN MEGABYTE FIXED TABLES COMPILE. §3.4 names two by name,
// and a spec whose own examples do not build is a spec nobody can follow.
func TestTheCorpusMegabyteFixedTablesCompile(t *testing.T) {
	for _, dir := range []string{"../tables/examples", "../tables/block"} {
		paths, err := GatherPaths([]string{dir})
		if err != nil {
			t.Fatal(err)
		}
		var warns []string
		c := New()
		c.OnWarn = func(msg string) { warns = append(warns, msg) }
		if _, err := c.Load(paths); err != nil {
			t.Fatalf("%s must compile: %v", dir, err)
		}
		// AND THE TABLE PAST THE CEILING IS NAMED, not passed over.
		var named bool
		for _, w := range warns {
			if strings.Contains(w, "fixed-form ceiling") {
				named = true
			}
		}
		if !named {
			t.Errorf("%s: the table past the ceiling must be NAMED — a table nobody warned about is a table nobody fixed: %v", dir, warns)
		}
	}
}

// AND A DECLARED FIXED TABLE INSIDE THE CEILING IS UNTOUCHED: the keyword
// changes what happens at the bound, not what happens under it.
func TestDeclaredFixedTableInsideTheCeilingCompiles(t *testing.T) {
	dir, _ := fixedSizeUnit(t, "Small", 64)
	paths, err := GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if !u.Tables["Small"].FixedDeclared {
		t.Fatal("`fixed table Small` must arrive DECLARED")
	}
	warns, errs := ir.TableFixedRecordBounds(u, 0)
	if len(errs) != 0 || len(warns) != 0 {
		t.Fatalf("a small declared fixed table says nothing: warns=%v errs=%v", warns, errs)
	}
}
