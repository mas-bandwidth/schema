package tabletext_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// longNumberModel is one Root with the same float64 and int32 fields the
// generated walk's TestJsonLongNumberTokenLands reads, so the two engines'
// answers are the same rule (docs/SPEC-TABLES.md §16.2 Numbers).
func longNumberModel(t *testing.T) *tabletext.Model {
	t.Helper()
	const src = "package probe\ntable Root\n{\n    f float64\n    i int32\n    w uint128\n    g fixed(16, 16) | min = -32768, max = 32767\n}\n"
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return tabletext.NewModel(u)
}

// docs/SPEC-TABLES.md §16.2 Numbers: a token that IS a JSON number is read by
// its VALUE, and its length is never a reason to call it malformed. The
// document's own size is the only bound. A long token whose value fits lands
// it; a long token whose value is past the field's range, or fractional in an
// integer field, refuses the whole read as any token with that value would.
func TestTextLongNumberTokenLands(t *testing.T) {
	t.Parallel()
	m := longNumberModel(t)
	st := m.Lookup("Root")
	if st == nil {
		t.Fatal("no table Root")
	}
	zeros := strings.Repeat("0", 600)
	for text, reason := range map[string]string{
		`{"i":1` + zeros + `}`:      "out_of_range",
		`{"w":1.` + zeros + `1}`:    "kind_mismatch",
		`{"g":1` + zeros + `.0}`:    "out_of_range",
		`{"i":-1` + zeros + `.0e0}`: "", // past what a float64 holds: refused, its reason this engine's detail
	} {
		var r tabletext.Report
		if m.Read(m.New(st), []byte(text), &r) || !r.Refused || !strings.HasPrefix(r.Reason, reason) {
			t.Fatalf("%.40s...: want a %s refusal, got %+v", text, reason, r)
		}
	}
	inst := m.New(st)
	text := []byte(`{"f":1.` + zeros + `,"i":1.` + zeros + `,"w":0.` + zeros + `,"g":1.` + zeros + `}`)
	var r tabletext.Report
	if !m.Read(inst, text, &r) || !r.Silent() {
		t.Fatalf("a valid long number token was refused: %+v", r)
	}
	if got := field(t, inst, "f").Cell.F; got != 1.0 {
		t.Fatalf("float64 landed %v, want 1.0", got)
	}
	if got := field(t, inst, "i").Cell.I; got != 1 {
		t.Fatalf("int32 landed %d, want 1", got)
	}
	if got := tabletext.WideValue(&field(t, inst, "w").Cell); got.Sign() != 0 {
		t.Fatalf("uint128 landed %s, want zero", got)
	}
	if got := tabletext.WideValue(&field(t, inst, "g").Cell); got.Int64() != 1<<16 {
		t.Fatalf("fixed(16,16) landed raw %s, want %d", got, 1<<16)
	}
	out, err := m.Write(inst)
	if err != nil {
		t.Fatalf("the writer refused the value it read: %v", err)
	}
	// the file writer adds the canonical single newline the generated ToJson
	// also ends with (§16.1), so the bytes compared are the ones both land
	out = append(out, '\n')
	want := "{\n  \"f\": 1,\n  \"i\": 1,\n  \"w\": 0,\n  \"g\": 1.0\n}\n"
	if string(out) != want {
		t.Fatalf("text %q, want %q", out, want)
	}
}
