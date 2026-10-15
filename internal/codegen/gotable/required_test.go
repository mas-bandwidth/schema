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

table Old
{
    health int32
    ammo   int32
}

table Root
{
    health int32 | required
    ammo   int32
}
`

const requiredProbe = `package probe

import (
	"strings"
	"testing"
)

func TestRequiredZeroRidesAndMissingRefuses(t *testing.T) {
	var v Root
	v.Ammo = 5
	buf := make([]byte, 256)
	n := RootSave(&v, buf)
	if n < 0 {
		t.Fatal("RootSave refused a valid value")
	}
	var got Root
	var rep TableReport
	if !RootLoad(&got, buf[:n], &rep) {
		t.Fatalf("RootLoad refused its own writer's record: %+v reason %q", rep, rep.Reason)
	}
	if got.Health != 0 || got.Ammo != 5 {
		t.Fatalf("round trip lost a value: %+v", got)
	}
	// The same value under the OLD layout — health plain, so its zero is
	// elided — is strictly shorter: the required zero RODE (§4.3).
	var old Old
	old.Ammo = 5
	on := OldSave(&old, buf)
	if on < 0 {
		t.Fatal("OldSave refused a valid value")
	}
	if n <= on {
		t.Fatalf("required zero did not ride: root record %d bytes, old-layout record %d", n, on)
	}
	// And that older, shorter record — health absent — is REFUSED by the
	// required reader, by name, with the field named (§4.3).
	var late Root
	var rep2 TableReport
	if RootLoad(&late, buf[:on], &rep2) {
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
	if strings.Contains(src, "value.Health != 0") {
		t.Error("the emitted writer still elides the required field at its default (docs/SPEC-TABLES.md §4.3)")
	}
}
