package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCsConformanceRunsUnderCheckedArithmetic(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	mkPath := filepath.Join(root, "make", "schema-cs-conformance-checked-overflow.mk")
	mkBytes, err := os.ReadFile(mkPath)
	if err != nil {
		t.Fatalf("read checked-overflow make fragment: %v", err)
	}
	mk := string(mkBytes)
	for _, want := range []string{
		"tables-cs-checked-overflow-conformance:",
		"-p:CheckForOverflowUnderflow=true",
		"build/tables-generated-cs/.stamp",
		"build/conformance-harness",
		"build-cs-cook",
		"$(DOTNET) build test/cs-cook/schemacook.csproj",
		"-p:CheckForOverflowUnderflow=true",
		"./build/conformance-harness run --only cs",
		"schemaconformance.dll",
	} {
		if !strings.Contains(mk, want) {
			t.Errorf("%s does not contain %q", mkPath, want)
		}
	}

	// The red half uses the same checked build against a scratch copy of the
	// generated corpus and requires the planted addition to raise overflow.
	if got := strings.Count(mk, "-p:CheckForOverflowUnderflow=true"); got < 4 {
		t.Errorf("checked conformance and both overflow plants need four checked builds; got %d", got)
	}
	for _, want := range []string{
		"tables-cs-checked-overflow-negative-control:",
		"CheckedOverflowPlant.cs",
		"OverflowException",
		"TablesGeneratedDir=",
		"schemacooktest.dll",
	} {
		if !strings.Contains(mk, want) {
			t.Errorf("%s does not pin negative-control token %q", mkPath, want)
		}
	}

	workflowPath := filepath.Join(root, ".github", "workflows", "schema-cs-conformance-checked-overflow.yml")
	workflow, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read checked-overflow workflow: %v", err)
	}
	// The cs-cook project is the port's test tree, not this card's file: the
	// control must copy it to scratch and add the plant there, leaving the
	// tracked project byte-identical to base.
	if strings.Contains(mk, "-p:CheckedOverflowPlant=") {
		t.Errorf("%s must not ask the tracked cs-cook project to compile the plant", mkPath)
	}
	for _, want := range []string{
		"sed -e 's|Include=\"src/\\*.cs\"|",
		"$(abspath $(CS_CHECKED_DIR)/negative/cs-cook/schemacook.csproj)",
		"Include=\"$(abspath test/cs-cook)/src/*.cs\"",
	} {
		if !strings.Contains(mk, want) {
			t.Errorf("%s does not copy the cs-cook project to scratch with the plant: missing %q", mkPath, want)
		}
	}
	text := string(workflow)
	for _, target := range []string{"tables-cs-checked-overflow-conformance", "tables-cs-checked-overflow-negative-control"} {
		if !strings.Contains(text, "make "+target) {
			t.Errorf("checked-overflow workflow does not invoke %s", target)
		}
	}
	if strings.Contains(text, "pull_request:") || strings.Contains(text, "push:") {
		t.Error("checked-overflow dotnet gate is heavy and must remain nightly/manual")
	}
}

func TestCsCheckedOverflowGeneratorCasts(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	// /checked+ turns every implicit narrowing and sign-to-unsigned
	// conversion into an OverflowException. The C# emitter therefore has to
	// spell the cases that are INTENTIONAL truncation or wrap as unchecked.
	cases := []struct {
		file string
		want string
		why  string
	}{
		{"internal/codegen/cstable/codecs.go", `"unchecked((ulong)(long)" + expr`, "signed GetRaw/ClampRaw sign-extends a negative value to ulong"},
		{"internal/codegen/cstable/native.go", "unchecked((byte)(raw >> (i * 8)))", "NativeWriter.Put truncates a declared-width scalar"},
		{"internal/codegen/cstable/regioncook.go", "unchecked((byte)(raw >> (i * 8)))", "the cook writer truncates a declared-width scalar"},
		{"internal/codegen/cstable/region.go", "unchecked((byte)(value >> (i * 8)))", "NativePut truncates a declared-width scalar"},
		{"internal/codegen/cstable/wire.go", "Byte(unchecked((byte)v))", "Writer.Var truncates a varint byte"},
		{"internal/codegen/cstable/wire.go", "w.Fixed(unchecked((ulong)raw)", "a 128-bit wide scalar writes its low and high 64-bit words"},
		{"internal/codegen/cstable/regionwrite.go", "w.Fixed(unchecked((ulong)raw)", "the region writer writes the low 64-bit word of a wide scalar"},
		{"internal/codegen/cstable/json.go", "unchecked(0ul - (ulong)value)", "JSON writes the magnitude of a negative signed value"},
		{"internal/codegen/cstable/json.go", "unchecked((long)f.GetRaw(owner, index))", "JSON reads a sign-extended raw scalar back as signed"},
		{"internal/codegen/cstable/variablejson.go", "unchecked((byte)(accumulator >> held))", "base64 decoding truncates the accumulator to one byte"},
		{"internal/codegen/cstable/retain.go", "RetainPut32(p, unchecked((uint)v))", "RetainPut64 narrows a ulong to its two 32-bit words"},
		{"internal/codegen/cstable/retain.go", "unchecked((byte)(value >> (8 * i)))", "the retention output writes a byte of a wide value"},
		{"internal/codegen/cstable/builderload.go", "unchecked((long)NativeWord(", "a builder directory offset is read back as a signed displacement"},
		{"internal/codegen/cstable/regionmessageload.go", "unchecked((long)NativeWord(", "a message directory offset is read back as a signed displacement"},
		{"internal/codegen/cstable/retain.go", "unchecked((long)NativeWord(", "a retained directory offset is read back as a signed displacement"},
		{"internal/codegen/cstable/regiongraph.go", "(byte*)unchecked((long)Value.Base + Value.At)", "a blob node's base plus signed displacement may wrap"},
		{"internal/codegen/cstable/regiongraph.go", "unchecked((ulong)NativeWord(child.Base + at, 4))", "a blob length is a uint32 whose high bit means too large"},
	}
	for _, c := range cases {
		path := filepath.Join(root, c.file)
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(src), c.want) {
			t.Errorf("%s does not spell %q: %s", c.file, c.want, c.why)
		}
	}
}
