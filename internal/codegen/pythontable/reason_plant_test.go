package pythontable

import (
	"strings"
	"testing"
)

// reasonPlantName is the reason every refusal carries once plantReasonRename
// has edited a copy of the emitted runtime.
const reasonPlantName = "renamed_by_the_plant"

// plantReasonRename edits a copy of the emitted runtime so every Refused it
// raises carries reasonPlantName instead of its own reason name: a refusal is
// still a refusal and an accepted read is untouched, so the verdict of every
// read is the one it was. A cross-language test holds the verdict alone
// (docs/rulings.json, refusal-verdict-only): the reason is per-language
// diagnostic detail, so a test run on the planted copy must stay green.
func plantReasonRename(t *testing.T, files map[string][]byte) {
	t.Helper()
	const anchor = "        self.reason = reason\n"
	planted := 0
	for name, data := range files {
		if !strings.HasSuffix(name, "/_schema/__init__.py") {
			continue
		}
		src := string(data)
		if n := strings.Count(src, anchor); n != 1 {
			t.Fatalf("%s carries %q %d times, want 1: the reason plant would be vacuous", name, anchor, n)
		}
		files[name] = []byte(strings.Replace(src, anchor, "        self.reason = \""+reasonPlantName+"\"\n", 1))
		planted++
	}
	if planted != 1 {
		t.Fatalf("the reason plant edited %d runtimes, want 1", planted)
	}
}
