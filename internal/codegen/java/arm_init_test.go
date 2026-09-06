package java

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

// Generation regressions for the two initialization modes. Runtime fixture
// tests separately verify values after repeated decode into poisoned storage.
const armInitSchema = `package armdefaults

enum Mode { One, Two }

type Leaf
{
    active bool = true
    kind Mode = Two
    q fixed(2, 30) = 1.0 | min = -1, max = 1
}

union Nested
{
    leaf Leaf
    empty
}

type Payload
{
    current Leaf
    fixed_values [2]Leaf
    live [1..3]Leaf
    nested Nested
    nested_values [2]Nested
    raw bytes(4)
    enabled bool
    if enabled
    {
        branch Leaf
    }
}

union Choice
{
    selected Payload
    empty
}

type Packet
{
    choice Choice
}
`

func armInitOutput(t *testing.T) string {
	t.Helper()
	f, errs := parser.Parse("Arms.schema", []byte(armInitSchema))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Arms.schema", Name: "Arms.schema", Base: "Arms", Bytes: []byte(armInitSchema), AST: f,
	}})
	if len(cerrs) != 0 {
		t.Fatal(cerrs)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, data := range files {
		all.Write(data)
	}
	return all.String()
}

func armInitBody(t *testing.T, source, declaration string) string {
	t.Helper()
	at := strings.Index(source, declaration)
	if at < 0 {
		t.Fatalf("missing %s", declaration)
	}
	start := strings.IndexByte(source[at:], '{') + at
	if start < at {
		t.Fatalf("missing body for %s", declaration)
	}
	depth := 0
	for end := start; end < len(source); end++ {
		switch source[end] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return source[start : end+1]
			}
		}
	}
	t.Fatalf("unclosed body for %s", declaration)
	return ""
}

func armInitContains(t *testing.T, body string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

func armInitExcludes(t *testing.T, body string, unwanted ...string) {
	t.Helper()
	for _, text := range unwanted {
		if strings.Contains(body, text) {
			t.Errorf("unexpected %q in:\n%s", text, body)
		}
	}
}

func TestUnionSelectionInitializesConstructionDefaults(t *testing.T) {
	source := armInitOutput(t)
	leaf := armInitBody(t, source, "public static void initLeaf(")
	armInitContains(t, leaf, "value.active = true;", "value.kind = Mode.two;", "1073741824")
	zero := armInitBody(t, source, "public static void zeroLeaf(")
	armInitContains(t, zero, "value.active = false;", "value.kind = 0;", "value.q = 0;")

	init := armInitBody(t, source, "public static void initPayload(")
	armInitContains(t, init, "initLeaf(value.current);", "initLeaf(value.fixedValues[i0]);",
		"i0 < 3", "initLeaf(value.live[i0]);", "value.liveCount = 1;",
		"value.nested.type = 0;", "value.nestedValues[i0].type = 0;",
		"java.util.Arrays.fill(value.raw, (byte) 0);", "value.rawLength = 0;", "initLeaf(value.branch);")
	armInitExcludes(t, init, "new ", "initNested(", "value.nested.leaf", "value.liveCount = 0;")
	zero = armInitBody(t, source, "public static void zeroPayload(")
	armInitContains(t, zero, "zeroLeaf(value.current);", "zeroLeaf(value.live[i0]);", "value.liveCount = 0;")
	armInitExcludes(t, zero, "initLeaf(")

	read := armInitBody(t, source, "public static boolean readChoice(")
	armInitContains(t, read, "value.selected.current.active = true;", "value.selected.liveCount = 1;",
		"value.selected.nested.type = 0;")
	armInitExcludes(t, read, "new ", "value.empty")
	armInitContains(t, source, "public final Payload selected = new Payload();")
}
