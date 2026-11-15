package dart

import (
	"strings"
	"testing"
)

// TestDartPacketWriteValidatesStringContent pins row 8 of the write contract
// (docs/SPEC.md §5.1) on the packet writer: a string's used bytes hold no zero
// byte and are well-formed UTF-8, refused by the debug assert the writer's
// other contracts use.
func TestDartPacketWriteValidatesStringContent(t *testing.T) {
	t.Parallel()
	src := "package writetext\n\ntype Probe\n{\n    text string(8)\n}\n"
	files := dartGenerateProbe(t, src)
	source := string(files["Clamp.dart"])
	for _, want := range []string{
		"assert(Schema.utf8Valid(value.text, value.textLength));",
		"assert(value.text[i0] != 0);",
	} {
		if !strings.Contains(source, want) {
			t.Errorf("the packet writer must validate a string's used bytes; missing %q in:\n%s", want, source)
		}
	}
}
