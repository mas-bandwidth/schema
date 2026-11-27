package main

import (
	"strings"
	"testing"
)

// TestReadIntoLawfulSecondIsStatedNeverInferred holds the manifest's
// lawful-second mark to the rows it may stand on: a `fixed` row handed one
// value, and the one word `lawful-second`. The engine-verdict check in
// readIntoLawfulSecond holds the mark to the file itself.
func TestReadIntoLawfulSecondIsStatedNeverInferred(t *testing.T) {
	row := func(form, count string, extra ...string) []string {
		f := []string{"read-into", "ri_case", "unit", "Root", form, "lawful.bin", "second.bin", count}
		return append(f, extra...)
	}
	r, err := parseReadInto(row("fixed", "1", "lawful-second"), "here")
	if err != nil || !r.SecondLawful {
		t.Fatalf("a marked fixed row of one value: SecondLawful %v, err %v", r.SecondLawful, err)
	}
	r, err = parseReadInto(row("fixed", "1"), "here")
	if err != nil || r.SecondLawful {
		t.Fatalf("an unmarked fixed row is lawful-second %v, err %v", r.SecondLawful, err)
	}
	for _, c := range []struct {
		name string
		f    []string
		want string
	}{
		{"count above one", row("fixed", "3", "lawful-second"), "lawful-second marks a fixed row handed one value"},
		{"variable form", row("variable", "1", "lawful-second"), "lawful-second marks a fixed row handed one value"},
		{"message form", row("message", "1", "lawful-second"), "lawful-second marks a fixed row handed one value"},
		{"unknown word", row("fixed", "1", "lawful"), `"lawful" is not a read-into marker`},
		{"ten words", row("fixed", "1", "lawful-second", "x"), "an optional lawful-second"},
	} {
		if _, err := parseReadInto(c.f, "here"); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err %v, want %q", c.name, err, c.want)
		}
	}
}
