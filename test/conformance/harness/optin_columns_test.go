package main

import (
	"strings"
	"testing"
)

// TestOptInLinesReadEachKindsOwnColumn (F-244): the filter reads each derived
// line kind's own reference column. A cook-write's third column is its input
// file and a forgery's is its kind; neither drops the row when it spells an
// omitted unit's key. A kind the table does not know is refused, never
// guessed at.
func TestOptInLinesReadEachKindsOwnColumn(t *testing.T) {
	t.Parallel()
	text := "unit demo a\nunit extra b\n" +
		"instance kept demo Root a.bin\n" +
		"cook-write kept extra - -\n" +
		"forgery forged extra Root f.bin 0 null\n" +
		"instance left extra Root b.bin\n" +
		"cook-write left kept.json - -"
	kept, dropped, err := optInLines(text, map[string]bool{"extra": true})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		kind, name string
		want       bool
	}{
		{"cook-write", "kept", true}, {"forgery", "forged", true}, {"instance", "kept", true},
		{"instance", "left", false}, {"cook-write", "left", false}, {"unit", "extra", false},
	} {
		if got := handed(kept, c.kind, c.name); got != c.want {
			t.Errorf("handed %s %s = %v, want %v", c.kind, c.name, got, c.want)
		}
	}
	if dropped["kept"] || dropped["forged"] || !dropped["left"] {
		t.Errorf("dropped = %v, want left alone", dropped)
	}
	if _, _, err := optInLines(text+"\nsurprise row extra", map[string]bool{"extra": true}); err == nil || !strings.Contains(err.Error(), "surprise") {
		t.Errorf("an unknown line kind was not refused by name: %v", err)
	}
}
