package unity

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func unitOf(t *testing.T, name, src string) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse(name+".schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: name + ".schema", Name: name + ".schema", Base: name,
		Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

func generateText(t *testing.T, src string) string {
	t.Helper()
	files, err := Generate(unitOf(t, "Probe", src))
	if err != nil {
		t.Fatal(err)
	}
	return string(files["Probe.cs"])
}

const probeSrc = `package probe

enum Kind { A, B }

type Inner
{
    count int32 = 3 | min = 0, max = 9
}

union Pick
{
    inner Inner
}

type Outer
{
    kind  Kind
    items [1..4]Inner
    name  string(8)
    pick  Pick
}
`

// The Unity dialect: every packet declaration is a [Serializable] struct of
// unmanaged storage, the buffers are generated generic structs, and nothing
// asks a Unity project for unsafe code, reflection or a class per value.
func TestEveryDeclarationIsAnUnmanagedStruct(t *testing.T) {
	text := generateText(t, probeSrc)
	for _, want := range []string{
		"[Serializable]\n    public struct Inner\n",
		"[Serializable]\n    public struct Outer\n",
		"[Serializable]\n    public struct Pick\n",
		"public Buffer4<Inner> Items;",
		"public Buffer8<byte> Name;",
		"public struct Buffer4<T> where T : unmanaged",
		"public static Span<T> AsSpan<T>(ref this Buffer4<T> buffer) where T : unmanaged",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, refused := range []string{"unsafe ", "unsafe{", "fixed (", "class Inner", "class Outer", "System.Reflection", "new Inner[", "[InlineArray"} {
		if strings.Contains(text, refused) {
			t.Errorf("the Unity dialect emitted %q", refused)
		}
	}
	if got := strings.Count(text, "class "); got != 2 { // Buffers and Schema
		t.Errorf("want exactly two classes (Buffers, Schema), got %d", got)
	}
}

// C# 9 has no parameterless struct constructor: the construction form is
// New<T>/Init<T>, emitted exactly for the types that declare a default or a
// born count anywhere in their by-value closure.
func TestConstructionFormIsNewAndInit(t *testing.T) {
	text := generateText(t, probeSrc)
	for _, want := range []string{
		"public static Inner NewInner()",
		"public static void InitInner(ref Inner value)",
		"public static Outer NewOuter()",
		"value.ItemsCount = 1;",
		"Span<Inner> itemsSpan = value.Items.AsSpan();",
		"InitInner(ref itemsSpan[i]);",
		"InitInner(ref value.Inner); // a selection starts from the construction form",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(text, "NewPick") || strings.Contains(text, "InitPick") {
		t.Error("a union is constructed at None: it takes no construction helper")
	}
}

// Generation is deterministic to the byte (SPEC §6.1).
func TestDeterministic(t *testing.T) {
	a, b := generateText(t, probeSrc), generateText(t, probeSrc)
	if a != b {
		t.Fatal("two generations of one unit differ")
	}
}

// A declaration that takes a generated unit-level name is refused by name.
func TestRefusesAGeneratedNameCollision(t *testing.T) {
	for _, src := range []string{
		"package p\n\ntype Buffer4\n{\n    a bool\n}\n\ntype T\n{\n    v [4]uint8\n}\n",
		"package p\n\ntype Buffers\n{\n    a bool\n}\n",
	} {
		_, err := Generate(unitOf(t, "P", src))
		if err == nil || !strings.Contains(err.Error(), "collides with the generated type") {
			t.Errorf("want a collision refusal, got %v", err)
		}
	}
}
