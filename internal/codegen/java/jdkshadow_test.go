package java

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// jdkShadowUnit checks one single-file unit from src.
func jdkShadowUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
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
	return u
}

// A schema type may take the simple name of a platform class the generated
// Java calls — Arrays, MethodHandles, VarHandle, ByteOrder, and the java.lang
// names Long, String, Object, Math, System — and becomes a nested class of that
// name. The generated file names every platform class from the package root
// and imports nothing, so the nested class shadows nothing it uses, and it
// compiles. (compiler's TestJavaUnitsNamedLikePlatformTypesCompile compiles a
// type and a sibling file for every platform name the emitters use.)
func TestJavaPlatformNamesSurviveSameNamedSchemaTypes(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"Arrays", "MethodHandles", "VarHandle", "ByteOrder", "Long", "String", "Object", "Math", "System"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := "package jdkshadow\n\ntype " + name + " { v uint8 }\n\ntype Holder\n{\n    x " + name +
				"\n    xs [3]" + name + "\n    raw [4]uint8\n    words [2]uint32\n    f float32\n    d float64\n    s string(8)\n}\n"
			files, err := Generate(jdkShadowUnit(t, src))
			if err != nil {
				t.Fatal(err)
			}
			out := string(files["Probe.java"])
			if strings.Contains(out, "\nimport ") {
				t.Errorf("Probe.java imports a platform class; it names each from the package root")
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
				if got, err := testproc.Command(t, javac, args...).CombinedOutput(); err != nil {
					t.Fatalf("javac: %v\n%s", err, got)
				}
			})
		})
	}
}

// A class named `java` obscures the java package every platform class is
// spelled from (JLS §6.4.2), so no spelling of java.lang.Long or
// java.util.Arrays reaches the platform beside it. Generation refuses the
// name, by name, for a type, a union, an enum, a flags set, a field and a
// union arm.
func TestJavaRefusesAPlatformNameObscuredByAClassNamedJava(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"type java { w uint8 }\ntype Holder { x java\n f float32 }\n",
		"union java { a uint8\n b uint16 }\n",
		"enum java { A, B }\n",
		"flags java { A, B }\n",
		"type Holder { java uint8 }\n",
		"type Holder { Java uint8 }\n",
		"union Choice { java uint8\n b uint16 }\n",
	} {
		_, err := Generate(jdkShadowUnit(t, "package jdkshadow\n\n"+src))
		if err == nil || !strings.Contains(err.Error(), "obscures the java package") {
			t.Errorf("%q: want the named refusal, got %v", src, err)
		}
	}
}
