package javatable

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestJavaDriverLoadsFixedRootFormThree pins how the Java conformance driver
// picks the reader for a wire instance. A root declared `fixed table` is a
// form 3 file and is read and written by the generated `<Root>Fixed` class; a
// root of any other kind is a form 1 file and is read by `<Root>Variable`.
// The variable reader takes form 1 only, so a driver that built
// `<Root>Variable` for every instance would refuse every fixed root's file.
//
// The test has two halves. The generator half states the discriminator the
// driver relies on: `<Root>Fixed` carries a root `load` exactly for the roots
// declared fixed. The driver half states that every surface reading a wire
// instance asks for the fixed codec before the variable one.
func TestJavaDriverLoadsFixedRootFormThree(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")

	u := unitFromPath(t, filepath.Join(repo, "tables", "examples", "Tables.schema"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	fixedRoots := 0
	otherRoots := 0
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if !st.IsTable || st.IsMapEntry() {
				continue
			}
			fixedFile := files[fixedClass(st.Name)+".java"]
			hasLoad := fixedFile != nil && strings.Contains(string(fixedFile), "public static int load(")
			if st.FixedDeclared {
				fixedRoots++
				if !hasLoad {
					t.Errorf("%s is declared fixed but %s carries no root load", st.Name, fixedClass(st.Name))
				}
			} else {
				otherRoots++
				if hasLoad {
					t.Errorf("%s is not declared fixed but %s carries a root load", st.Name, fixedClass(st.Name))
				}
			}
			if files[st.Name+"Variable.java"] == nil {
				t.Errorf("%s has no %sVariable class", st.Name, st.Name)
			}
		}
	}
	if fixedRoots == 0 {
		t.Fatal("Tables.schema declares no fixed root: the test pins nothing")
	}

	src, err := os.ReadFile(filepath.Join(repo, "test", "conformance", "java", "src", "Driver.java"))
	if err != nil {
		t.Fatal(err)
	}
	driver := string(src)
	if !strings.Contains(driver, `Class.forName(pkg + "." + root + "Fixed")`) {
		t.Errorf("the driver never resolves a root's %s class", "Fixed")
	}
	if !strings.Contains(driver, `Class.forName(pkg + "." + root + "Variable")`) {
		t.Errorf("the driver no longer resolves a variable root's %s class", "Variable")
	}
	for _, surface := range []string{"surfaceWire", "surfaceReport", "surfaceFraming", "surfaceJsonRead", "surfaceJsonWrite", "surfaceCookWrite"} {
		body := driverFunc(t, driver, surface)
		fixedAt := strings.Index(body, "fixedCodec(")
		variableAt := strings.Index(body, "variableCodec(")
		if surface == "surfaceCookWrite" {
			variableAt = strings.Index(body, "cookCodec(")
		}
		if fixedAt < 0 {
			t.Errorf("%s never asks for the fixed codec", surface)
			continue
		}
		if variableAt >= 0 && fixedAt > variableAt {
			t.Errorf("%s asks for the variable codec before the fixed one", surface)
		}
	}
}

// driverFunc answers the body of one top-level static method of the driver.
func driverFunc(t *testing.T, driver, name string) string {
	t.Helper()
	at := strings.Index(driver, "private static int "+name+"(")
	if at < 0 {
		t.Fatalf("the driver has no %s", name)
	}
	rest := driver[at+1:]
	end := strings.Index(rest, "\n    private static ")
	if end < 0 {
		end = len(rest)
	}
	return rest[:end]
}

// TestJavaFixedLayoutIsTheReferenceLayout pins that the layout a generated
// `<Root>Fixed` writes into a file header, and the hash taken over it, are the
// compiler's own (ir.TableFixedWalkRoot) for every fixed root of the units the
// conformance corpus names. A file written by any other leg carries that
// layout, and a reader holding a different one refuses the file.
func TestJavaFixedLayoutIsTheReferenceLayout(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	data, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || f[0] != "unit" {
			continue
		}
		u := unitFromPath(t, filepath.Join(repo, f[2]))
		for _, st := range fixedRoots(u) {
			checked++
			got := fixedLayoutBytes(fixedWalkRoot(st).entries)
			want := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
			if !bytes.Equal(got, want) {
				t.Errorf("%s %s: the Java layout is %d bytes and the compiler's is %d", f[1], st.Name, len(got), len(want))
			}
		}
	}
	if checked == 0 {
		t.Fatal("the manifest names no fixed root")
	}
}

// TestJavaVariableLoadRefusesFixedRoot pins that the variable form's `load` of
// a root declared fixed refuses with previous_form: a fixed table's file is
// form 3, and the form 1 file the variable form reads is the one before it.
// A root of any other kind keeps reading form 1.
func TestJavaVariableLoadRefusesFixedRoot(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	u := unitFromPath(t, filepath.Join(repo, "tables", "examples", "Tables.schema"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	fixed := 0
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if !st.IsTable || st.IsMapEntry() {
				continue
			}
			src := string(files[st.Name+"Variable.java"])
			at := strings.Index(src, "public static Value load(byte[] data, TableVariable.Report report) {")
			if at < 0 {
				t.Errorf("%sVariable has no load", st.Name)
				continue
			}
			body := src[at:]
			if end := strings.Index(body, "\n    }\n"); end >= 0 {
				body = body[:end]
			}
			refuses := strings.Contains(body, `report.reason = "previous_form"`)
			if st.FixedDeclared {
				fixed++
			}
			if refuses != st.FixedDeclared {
				t.Errorf("%s: declared fixed is %v and its variable load refuses form 1 is %v", st.Name, st.FixedDeclared, refuses)
			}
		}
	}
	if fixed == 0 {
		t.Fatal("Tables.schema declares no fixed root: the test pins nothing")
	}
}
