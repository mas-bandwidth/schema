package gotable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

// TestGoEnumVariantTypeWords holds SPEC §4.1's rule that an enum variant may be
// named a reserved word in the Go backend, through a generated unit compiled and
// run in a t.TempDir(): a table holding an enum whose variants are text, int,
// enum, list and bool generates, compiles, round-trips the binary wire, base64
// and the JSON text, and the JSON text form carries the words as written. The
// unit is checked for the Go target only: C# and C++ each refuse a variant they
// cannot spell (`int`), which is SPEC §4.6's per-target reserved-word check and
// is not this card's to relax.
func TestGoEnumVariantTypeWords(t *testing.T) {
	t.Parallel()
	f, perrs := parser.Parse("Probe.schema", []byte(enumTypeWordSchema))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.UnitForTargets([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(enumTypeWordSchema), AST: f,
	}}, []string{"go"})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	runGeneratedUnit(t, u, enumTypeWordProbe, nil)
}

const enumTypeWordSchema = `package probe

enum FieldType {
    text
    int
    enum
    list
    bool
}

table Root {
    kind FieldType
}
`

const enumTypeWordProbe = `package probe
import("testing";"bytes";"encoding/base64")
func TestEnumWords(t *testing.T) {
	for _, tc := range []struct{ kind FieldType; word string }{
		{FieldTypeInt, "int"}, {FieldTypeEnum, "enum"}, {FieldTypeBool, "bool"},
		{FieldTypeText, "text"}, {FieldTypeList, "list"},
	} {
		var v, got Root
		RootReset(&v)
		v.Kind = tc.kind
		n := RootMeasure(&v)
		b := make([]byte, n)
		if RootSave(&v, b) != n {
			t.Fatalf("save %s", tc.word)
		}
		var r TableReport
		if !RootLoadTolerant(&got, b, &r) || r != (TableReport{}) || got.Kind != tc.kind {
			t.Fatalf("wire round trip %s: %+v %+v", tc.word, got, r)
		}
		a := make([]byte, 128)
		na := RootToJson(&v, a)
		if na < 0 || !bytes.Contains(a[:na], []byte("\"kind\":\""+tc.word+"\"")) {
			t.Fatalf("json %s: %s", tc.word, a[:max(na, 0)])
		}
		b64, err := RootSaveText(&v, nil)
		if err != nil || b64 != base64.StdEncoding.EncodeToString(b) {
			t.Fatalf("base64 %s: %v", tc.word, err)
		}
		if back, err := RootLoadText(b64, nil); err != nil || back.Kind != tc.kind {
			t.Fatalf("load text %s: %v", tc.word, err)
		}
	}
}
`
