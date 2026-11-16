package js

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

// TestPacketWritersCheckStringText pins SPEC §5.1 row 8 on the JavaScript
// packet writers: both tiers check a string's used bytes with the read-side
// validator the module already carries, and refuse an off-contract value
// through the tier's own mechanism before the string bytes ride.
func TestPacketWritersCheckStringText(t *testing.T) {
	t.Parallel()

	src := `package textprobe

type Payload
{
    text string(8)
}
`
	ast, perrs := parser.Parse("Payload.schema", []byte(src))
	if len(perrs) != 0 {
		t.Fatalf("parse: %v", perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Payload.schema", Name: "Payload.schema", Base: "Payload", Bytes: []byte(src), AST: ast,
	}})
	if len(cerrs) != 0 {
		t.Fatalf("check: %v", cerrs)
	}
	out, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}

	for _, tier := range []struct {
		file    string
		fn      string
		refusal string
		before  string
	}{
		{"Payload.js", "WritePayload", "return false;", "serializeBytes("},
		{"PayloadFlat.js", "WritePayloadFlat", "return -1;", "view.setUint32("},
	} {
		module, ok := out[tier.file]
		if !ok {
			t.Fatalf("no %s module: %v", tier.file, keys(out))
		}
		text := string(module)
		if !strings.Contains(text, "function validText(bytes, length) {") {
			t.Errorf("%s must carry the read-side text validator", tier.file)
		}
		write := emittedFunction(t, text, tier.fn)
		guard := "if (!validText(value.text, value.textLength)) {\n    " + tier.refusal + "\n  }"
		if !strings.Contains(write, guard) {
			t.Errorf("%s must refuse ill-formed text through the validator; missing %q", tier.fn, guard)
		}
		if at, call := strings.Index(write, guard), strings.Index(write, tier.before); at < 0 || call < 0 || at > call {
			t.Errorf("%s must check the text before its string bytes ride", tier.fn)
		}
	}
}
