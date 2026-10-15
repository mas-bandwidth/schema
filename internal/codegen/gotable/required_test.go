// The `| required` marker on the Go variable wire (docs/SPEC-TABLES.md §4.3):
// the writer NEVER elides a required field — a zero rides — and a record
// missing one is refused by name, `missing_required` with the field named.
// At BASE a zero field was elided, so absence read the same as zero and a
// variable table could not say a field must be present; this pins both halves
// of the fix in a generated unit compiled and run by the house helper.
package gotable

import (
	"strings"
	"testing"
)

const requiredSchema = `package probe

table Root
{
    health int32 | required
    ammo   int32
}

// The OLD LAYOUT: the same field names — so the same name-hash wire ids —
// with health plain, so its zero is elided exactly as at BASE.
table RootOld
{
    health int32
    ammo   int32
}
`

const requiredProbe = `package probe

import (
	"strings"
	"testing"
)

func saveForm1(t *testing.T, write func(*TableWriter) bool) []byte {
	t.Helper()
	var ids TableIds
	buf := make([]byte, 256)
	w := TableWriter{Buffer: buf, Ids: &ids}
	w.Put8(1)
	if !write(&w) {
		t.Fatal("the body writer refused a valid value")
	}
	w.Trailer()
	if w.Overflow || ids.Overflow {
		t.Fatal("the writer overflowed")
	}
	return buf[:w.Offset]
}

func TestRequiredZeroRidesAndMissingRefuses(t *testing.T) {
	// A required int32 written as ZERO rides the wire and reads back
	// (docs/SPEC-TABLES.md §4.3): the writer never elides it.
	var v Root
	v.Ammo = 5
	data := saveForm1(t, func(w *TableWriter) bool { return RootSaveBody(w, &v) })
	var rep TableReport
	r, verdict := tableOpen(data, &rep)
	if verdict != TableOpenOk {
		t.Fatalf("tableOpen verdict %v reason %q", verdict, rep.Reason)
	}
	var got Root
	if !RootLoadBody(&r, &got) {
		t.Fatalf("the reader refused its own writer's record: reason %q", rep.Reason)
	}
	if got.Health != 0 || got.Ammo != 5 {
		t.Fatalf("round trip lost a value: %+v", got)
	}
	// The same value under the OLD LAYOUT — health plain, so its zero is
	// elided — is strictly shorter: the required zero RODE.
	var old RootOld
	old.Ammo = 5
	oldData := saveForm1(t, func(w *TableWriter) bool { return RootOldSaveBody(w, &old) })
	if len(data) <= len(oldData) {
		t.Fatalf("required zero did not ride: new record %d bytes, old-layout record %d", len(data), len(oldData))
	}
	// And that older record — health absent — is REFUSED by name, the field
	// named (§4.3): absence no longer reads as the default.
	var rep2 TableReport
	r2, verdict2 := tableOpen(oldData, &rep2)
	if verdict2 != TableOpenOk {
		t.Fatalf("tableOpen verdict %v reason %q", verdict2, rep2.Reason)
	}
	var late Root
	if RootLoadBody(&r2, &late) {
		t.Fatal("an old-layout record missing a required field loaded; want the missing_required refusal")
	}
	if !strings.Contains(rep2.Reason, "missing_required") || !strings.Contains(rep2.Reason, "health") {
		t.Fatalf("refusal %q does not name missing_required and the field health", rep2.Reason)
	}
}
`

func TestGoTableRequiredFieldRefusesWhenMissing(t *testing.T) {
	t.Parallel()
	runGenerated(t, requiredSchema, requiredProbe)
}

// The central refusal's Go-side counterpart: the emitted variable writer
// carries no elision compare for the required field and the emitted reader
// carries the missing_required refusal naming it.
func TestGoTableRequiredFieldEmittedShapes(t *testing.T) {
	t.Parallel()
	files := generate(t, requiredSchema)
	var src string
	for name, data := range files {
		if strings.HasSuffix(name, "Table.go") || strings.HasSuffix(name, "Tables.go") {
			src += string(data)
		}
	}
	if src == "" {
		for _, data := range files {
			src += string(data)
		}
	}
	if !strings.Contains(src, "missing_required: health") {
		t.Error("the emitted reader does not refuse a missing required field by name (docs/SPEC-TABLES.md §4.3)")
	}
	// The REQUIRED table's own writer carries no elision compare — while the
	// old-layout twin (RootOld, health plain) still does, which is the pair
	// the round-trip test measures.
	body := funcSource(src, "func RootSaveBody(") + funcSource(src, "func RootSaveBodyFields(")
	if body == "" {
		t.Fatal("neither RootSaveBody nor RootSaveBodyFields is emitted")
	}
	if strings.Contains(body, "Health != 0") {
		t.Error("the emitted writer still elides the required field at its default (docs/SPEC-TABLES.md §4.3)")
	}
	old := funcSource(src, "func RootOldSaveBody(") + funcSource(src, "func RootOldSaveBodyFields(")
	if !strings.Contains(old, "Health != 0") {
		t.Error("the plain old-layout twin lost its elision — the pair no longer pins the marker")
	}
}
