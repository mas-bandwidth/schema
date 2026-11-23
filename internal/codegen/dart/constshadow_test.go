package dart_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
)

// TestFieldDefaultNamingShadowedConstant pins SPEC §4.2 (a field never shadows
// a same-named constant): the unsigned-subtraction pin declares const Half and
// a field half = Half. Dart resolves a bare name in a class initializer to the
// field, so emitting `int half = half;` is a self-reference dart analyze
// rejects. The default must fold to the constant's value instead. Red on the
// generator before the fix: the output carries `int half = half;`.
func TestFieldDefaultNamingShadowedConstant(t *testing.T) {
	t.Parallel()
	u := unitFromSource(t, `package probe

const Start uint32 = 1
const Half = (Start - 2) / 2

type Value
{
    half int64 = Half
}
`)
	files, err := dart.Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var all strings.Builder
	for _, body := range files {
		all.Write(body)
	}
	if strings.Contains(all.String(), "int half = half;") {
		t.Fatalf("a field default names the field itself, not the constant:\n%s", all.String())
	}
	if !strings.Contains(all.String(), "int half = 0;") {
		t.Fatalf("the default did not fold to the constant's value:\n%s", all.String())
	}
}
