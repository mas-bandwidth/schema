package gotable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

// TestGoEnumVariantTypeWords holds SPEC §4.1's rule that an enum variant may be
// named a reserved word in the Go backend, through a generated unit compiled and
// run in a t.TempDir(): a table holding an enum whose variants are text, int32,
// map, list and table generates, compiles, round-trips the binary wire, base64
// and the JSON text, and the JSON text form carries the words as written. The
// words are schema keywords that no supported language but Go reserves (Go
// carries them as variants, SPEC §4.6): a word another language reserves
// (`int`, `enum`, `bool`) is refused whatever target is generated.
func TestGoEnumVariantTypeWords(t *testing.T) {
	t.Parallel()
	f, perrs := parser.Parse("Probe.schema", []byte(enumTypeWordSchema))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(enumTypeWordSchema), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	if out, err := runGeneratedUnit(t, u, enumTypeWordProbe, nil); err != nil {
		t.Fatalf("generated unit: %v\n%s", err, out)
	}
}

const enumTypeWordSchema = `package probe

enum FieldType {
    text
    int32
    map
    list
    table
}

fixed table Root {
    kind FieldType
}
`

const enumTypeWordProbe = `package probe
import("testing";"bytes";"encoding/base64")

func rootForm1(value *Root, data []byte, report *TableReport) bool {
	// Load refuses a form-1 file at a fixed-class root. The body still answers the probe.
	var gate TableReport
	if RootLoad(value, data, &gate) || gate.Reason != "previous_form" {
		if report != nil {
			*report = gate
		}
		return false
	}
	out := report
	if out == nil {
		out = &TableReport{}
	} else {
		*out = TableReport{}
	}
	r, verdict := tableOpenChecked(data, out)
	if verdict != TableOpenOk || !RootLoadBody(&r, value) || r.Offset != int64(len(r.Buffer)) || out.Refused() {
		tableRefused(out)
		RootReset(value)
		if report != nil {
			*report = *out
		}
		return false
	}
	return true
}

func rootForm1Text(text string, report *TableReport) (*Root, error) {
	if report != nil {
		*report = TableReport{}
	}
	wire, err := base64.StdEncoding.Strict().DecodeString(text)
	if err != nil {
		if _, plainErr := base64.StdEncoding.DecodeString(text); plainErr == nil {
			return nil, tableRefuseError(TableRefuseBase64NotCanonical)
		}
		return nil, tableRefuseError(TableRefuseBase64Damaged)
	}
	if len(text) != base64.StdEncoding.EncodedLen(len(wire)) {
		return nil, tableRefuseError(TableRefuseBase64NotCanonical)
	}
	if len(wire) == 0 || wire[0] != 1 {
		return nil, tableRefuseError(TableRefuseUnknownForm)
	}
	var v Root
	var read TableReport
	ok := rootForm1(&v, wire, &read)
	if report != nil {
		*report = read
	}
	if !ok {
		return nil, read.Err()
	}
	return &v, nil
}
func TestEnumWords(t *testing.T) {
	for _, tc := range []struct{ kind FieldType; word string }{
		{FieldTypeint32, "int32"}, {FieldTypemap, "map"}, {FieldTypetable, "table"},
		{FieldTypetext, "text"}, {FieldTypelist, "list"},
	} {
		if got := EnumNameFieldType(tc.kind); got != tc.word {
			t.Fatalf("name %s: %q", tc.word, got)
		}
		var v, got Root
		RootReset(&v)
		v.Kind = tc.kind
		n := RootMeasure(&v)
		b := make([]byte, n)
		if RootSave(&v, b) != n {
			t.Fatalf("save %s", tc.word)
		}
		var r TableReport
		if !rootForm1(&got, b, &r) || r != (TableReport{}) || got.Kind != tc.kind {
			t.Fatalf("wire round trip %s: %+v %+v", tc.word, got, r)
		}
		a := make([]byte, 128)
		na := RootToJSON(&v, a)
		if na < 0 || !bytes.Contains(a[:na], []byte("\"kind\": \""+tc.word+"\"")) {
			t.Fatalf("json %s: %d %s", tc.word, na, a)
		}
		var j Root
		if !RootFromJSON(&j, a[:na], &r) || j.Kind != tc.kind {
			t.Fatalf("json round trip %s: %+v %+v", tc.word, j, r)
		}
		b64, err := RootSaveText(&v)
		if err != nil || b64 != base64.StdEncoding.EncodeToString(b) {
			t.Fatalf("base64 %s: %v", tc.word, err)
		}
		back, err := rootForm1Text(b64, &r)
		if err != nil || back == nil || back.Kind != tc.kind {
			t.Fatalf("load text %s: %v", tc.word, err)
		}
	}
}
`
