package js

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

// SF-24 (docs/silent-failure-audit-1122.md): the packet Write of a statically
// byte-aligned [N]uint8 handed serializeBytes the view subarray(0, N). A
// storage shorter than N made that view short, and the wire carried a short
// field with no error. The write refuses a storage that is not a Uint8Array of
// at least N bytes (docs/SPEC.md section 5.1: JavaScript refuses in every
// build), and it refuses BEFORE the first byte of the field is written.
//
// Pure test: it reads the emitted Write, no node, no real time.
const bulkBytesShortSchema = `package bulkshort

type T {
    align
    data [4]uint8
}
`

func TestBulkByteArrayWriteRefusesShortStorage(t *testing.T) {
	t.Parallel()
	ast, perrs := parser.Parse("BulkShort.schema", []byte(bulkBytesShortSchema))
	if len(perrs) != 0 {
		t.Fatalf("parse: %v", perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "BulkShort.schema", Name: "BulkShort.schema", Base: "BulkShort",
		Bytes: []byte(bulkBytesShortSchema), AST: ast,
	}})
	if len(cerrs) != 0 {
		t.Fatalf("check: %v", cerrs)
	}
	out, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	body := emittedFunction(t, string(out["BulkShort.js"]), "WriteT")
	copyAt := strings.Index(body, "serializeBytes(value.data.subarray(0, 4))")
	if copyAt < 0 {
		t.Fatalf("WriteT no longer takes the bulk byte path, so this test checks nothing:\n%s", body)
	}
	guard := "if (!(value.data instanceof globalThis.Uint8Array) || value.data.length < 4) {"
	guardAt := strings.Index(body, guard)
	if guardAt < 0 || guardAt > copyAt {
		t.Fatalf("WriteT must refuse a storage that is not a Uint8Array of at least 4 bytes before it copies:\n%s", body)
	}
	if !strings.Contains(body[guardAt:copyAt], "return false;") {
		t.Fatalf("the short-storage guard must refuse with false:\n%s", body)
	}
}
