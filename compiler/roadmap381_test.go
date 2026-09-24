// The roadmap is where the languages after the nine are tracked: its matrix
// carries a column for each of them and issue #381 is the commitment behind
// them. The matrix names them only by abbreviation — `ts`, `gdscript` — and
// the page was trimmed of the paragraph that spelled the eleven out and linked
// the issue, so a reader sees empty columns and no reason for them. This gate
// holds the page to the issue it is the roadmap for: the link, and all eleven
// names, in the issue's own order.
package compiler

import (
	"os"
	"strings"
	"testing"
)

// theElevenMoreLanguages is #381's list, in the issue's own order (schema #381,
// "Eleven more languages").
var theElevenMoreLanguages = []string{
	"Swift", "TypeScript", "Lua", "Clojure", "Python", "Ruby",
	"Kotlin", "GDScript", "Zig", "Odin", "Haxe",
}

// TestRoadmapTracksTheElevenMoreLanguages is the gate: the roadmap names the
// eleven languages after the nine and links the issue that tracks them.
func TestRoadmapTracksTheElevenMoreLanguages(t *testing.T) {
	raw, err := os.ReadFile("../ROADMAP.md")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)
	if !strings.Contains(page, "issues/381") {
		t.Error("ROADMAP.md does not link issue #381, where the eleven languages after the nine are tracked")
	}
	for _, lang := range theElevenMoreLanguages {
		if !strings.Contains(page, lang) {
			t.Errorf("ROADMAP.md does not name %s, one of the eleven languages tracked on #381", lang)
		}
	}
}
