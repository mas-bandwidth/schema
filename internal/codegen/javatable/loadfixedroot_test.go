package javatable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
