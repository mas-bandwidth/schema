package cstable

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Only a fixed table rides the message form (docs/SPEC-TABLES.md §3.3). A
// variable root has no message verb and no refusal named for one; a fixed
// root's batch API and its named message-retention refusal are emitted.
func TestMessageFormFixedTablesOnly(t *testing.T) {
	u := csUnitOf(t, `package probe
type Inner { x int32 }
fixed table Fixedroot { a uint32
inner Inner }
table Varleaf { n int32 }
table Varroot { leaf *Varleaf }
table Varplain { v int32 }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var all strings.Builder
	for _, name := range names {
		all.Write(files[name])
	}
	text := all.String()
	for _, want := range []string{"FixedrootLoadMessages(", "FixedrootSaveMessages(", "FixedrootMeasureMessages(", "FixedrootSaveRetainMessages("} {
		if !strings.Contains(text, want) {
			t.Errorf("the fixed root's message surface lacks %q", want)
		}
	}
	absent := regexp.MustCompile(`\b(Varroot|Varplain|Varleaf)(Load|Save|Measure|LoadRetain|SaveRetain)Messages\b|\b(Varroot|Varplain|Varleaf)LoadMeasure\(TableVocabulary\b|\bLoadMessageRegion\b|\bSaveMessageRegion\b|\bLoadMessageRetainRegion\b|\bMessageLoadMeasure\b`)
	if hits := absent.FindAllString(text, -1); len(hits) > 0 {
		t.Fatalf("a variable table carries message-form code: %q", hits)
	}
}
