package gotable

// THE TEXT WRITER REFUSES A COUNT OR A LENGTH OUTSIDE ITS STORAGE (docs/SPEC.md
// §5.1): ToJSON and ToJSONMeasure answer -1 for a counted array's count, a
// string's length or a bytes' length that is negative or past its declared
// extent, in every build, rather than clamping it to the extent. Each case
// sits one level down too, inside an element of an array of records.

import (
	"strings"
	"testing"
)

const jsonCountSchema = `package probe
type Pick
{
    name string(4)
    tags [..2]int32
}
fixed table Root
{
    label string(8)
    blob bytes(4)
    picks [..3]Pick
}
`

const jsonCountProbe = `package probe

import "testing"

func TestTextWriterRefusesStorageOverrun(t *testing.T) {
	good := func() *Root {
		v := &Root{LabelLength: 2, BlobLength: 1, PicksCount: 1}
		copy(v.Label[:], "ok")
		v.Picks[0].NameLength = 1
		v.Picks[0].Name[0] = 'a'
		v.Picks[0].TagsCount = 1
		return v
	}
	if n := RootToJSONMeasure(good()); n <= 0 || RootToJSON(good(), make([]byte, n)) != n {
		t.Fatalf("a lawful value: measure %d", n)
	}
	for _, c := range []struct {
		name string
		set  func(v *Root)
	}{
		{"label past", func(v *Root) { v.LabelLength = 9 }},
		{"label negative", func(v *Root) { v.LabelLength = -1 }},
		{"blob past", func(v *Root) { v.BlobLength = 5 }},
		{"picks past", func(v *Root) { v.PicksCount = 4 }},
		{"picks negative", func(v *Root) { v.PicksCount = -1 }},
		{"element name past", func(v *Root) { v.Picks[0].NameLength = 5 }},
		{"element tags past", func(v *Root) { v.Picks[0].TagsCount = 3 }},
	} {
		v := good()
		c.set(v)
		if n := RootToJSONMeasure(v); n != -1 {
			t.Errorf("%s: ToJSONMeasure answered %d, wants the refusal -1", c.name, n)
		}
		if n := RootToJSON(v, make([]byte, 1<<14)); n != -1 {
			t.Errorf("%s: ToJSON answered %d, wants the refusal -1", c.name, n)
		}
	}
}
`

func TestGoTextWriterRefusesStorageOverrun(t *testing.T) {
	t.Parallel()
	runGenerated(t, jsonCountSchema, jsonCountProbe)
}

// TestGoTextWriterStorageNegativeControl removes the bound from the generated
// unit: the writer then answers the text for every case, and the probe must
// go red on the refusal.
func TestGoTextWriterStorageNegativeControl(t *testing.T) {
	t.Parallel()
	out, err := runGeneratedEdited(t, jsonCountSchema, jsonCountProbe, func(files map[string][]byte) {
		patch(t, files, "if f.Counted && !tableJSONCountHolds(base, f) {", "if false && f.Counted && !tableJSONCountHolds(base, f) { /* SABOTAGED */")
	})
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: the text writer's storage bound is gone and the probe stayed GREEN\n%s", out)
	}
	if !strings.Contains(string(out), "wants the refusal -1") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the probe went red, but not on the refusal\n%s", out)
	}
}
