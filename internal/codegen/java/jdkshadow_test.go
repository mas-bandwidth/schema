package java

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// A schema type may take the simple name of a platform class the generated
// Java calls — Arrays, MethodHandles, VarHandle, ByteOrder — and becomes a
// nested class of that name, which shadows a single-type import inside the
// file. The generated file then reaches each such platform class another way
// (a static import of the method it calls, or the full name), and it
// compiles — also beside a class named `java`, where that is possible; a file
// whose names shadow nothing keeps the imports and the short names.
func TestJavaPlatformNamesSurviveSameNamedSchemaTypes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ label, name, extra string }{
		{"Arrays", "Arrays", ""},
		{"MethodHandles", "MethodHandles", ""},
		{"VarHandle", "VarHandle", ""},
		{"ByteOrder", "ByteOrder", ""},
		// a class named java obscures the java package in the class body,
		// and these two still compile: neither names a package
		{"java and Arrays", "Arrays", "type java { w uint8 }\n"},
		{"java and MethodHandles", "MethodHandles", "type java { w uint8 }\n"},
	} {
		name := tc.name
		t.Run(tc.label, func(t *testing.T) {
			t.Parallel()
			src := "package jdkshadow\n\n" + tc.extra + "type " + name + " { v uint8 }\n\ntype Holder\n{\n    x " + name +
				"\n    xs [3]" + name + "\n    raw [4]uint8\n    words [2]uint32\n    f float32\n}\n"
			f, errs := parser.Parse("Probe.schema", []byte(src))
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			u, cerrs := check.Unit([]check.SourceFile{{
				Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
			}})
			if len(cerrs) != 0 {
				t.Fatal(cerrs)
			}
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			out := string(files["Probe.java"])
			if strings.Contains(out, "import ") && strings.Contains(out, "."+name+";") {
				t.Errorf("Probe.java still imports the %s its nested class shadows", name)
			}
			t.Run("javac", func(t *testing.T) {
				slowtest.Gate(t, "javac (it compiles the generated file)")
				javac, _ := compressedFloatJavaTools(t)
				dir := t.TempDir()
				pkg := filepath.Join(dir, "src", "jdkshadow")
				if err := os.MkdirAll(pkg, 0o755); err != nil {
					t.Fatal(err)
				}
				var paths []string
				for fname, data := range files {
					p := filepath.Join(pkg, fname)
					if err := os.WriteFile(p, data, 0o644); err != nil {
						t.Fatal(err)
					}
					paths = append(paths, p)
				}
				classes := filepath.Join(dir, "classes")
				if err := os.MkdirAll(classes, 0o755); err != nil {
					t.Fatal(err)
				}
				args := append([]string{"--release", "17", "-nowarn", "-d", classes}, paths...)
				if got, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
					t.Fatalf("javac: %v\n%s", err, got)
				}
			})
		})
	}
}

// A file that declares a class named `java` beside one named VarHandle or
// ByteOrder leaves the generated word-access field no spelling of that
// platform class: the simple name is the schema's class and the full name
// starts with the obscured package. Generation refuses it by name.
func TestJavaRefusesAPlatformNameObscuredByAClassNamedJava(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"VarHandle", "ByteOrder"} {
		src := "package jdkshadow\n\ntype java { w uint8 }\ntype " + name + " { v uint8 }\n\ntype Holder\n{\n    x " + name + "\n    f float32\n}\n"
		f, errs := parser.Parse("Probe.schema", []byte(src))
		if len(errs) != 0 {
			t.Fatal(errs)
		}
		u, cerrs := check.Unit([]check.SourceFile{{
			Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
		}})
		if len(cerrs) != 0 {
			t.Fatal(cerrs)
		}
		if _, err := Generate(u); err == nil || !strings.Contains(err.Error(), "obscures the java package") {
			t.Errorf("java beside %s: want the named refusal, got %v", name, err)
		}
	}
}
