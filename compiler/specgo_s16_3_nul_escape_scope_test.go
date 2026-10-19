package compiler

import (
	"os"
	"strings"
	"testing"
)

func TestSpecGoS163NulEscapeScope(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../docs/SPEC-TABLES.md")
	if err != nil {
		t.Fatal(err)
	}
	page := strings.Join(strings.Fields(string(data)), " ")
	tick := string(rune(96))
	wantObject := "a table object's key, whether it names a field or none " + string(rune(0x2014)) + " stores nothing and is not this event"
	wantEscape := "a " + tick + `\u0001` + tick + " escape, or any escape but " + tick + `\u0000` + tick + ", is not this event"
	for _, want := range []string{wantObject, wantEscape} {
		if !strings.Contains(page, want) {
			t.Errorf("docs/SPEC-TABLES.md does not hold %q", want)
		}
	}
	stale := "an object key naming a field " + string(rune(0x2014)) + " stores nothing"
	if strings.Contains(page, stale) {
		t.Errorf("docs/SPEC-TABLES.md still holds stale phrase %q", stale)
	}
}
