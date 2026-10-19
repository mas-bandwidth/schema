package gotable

import (
	"bytes"
	"regexp"
	"testing"
)

// unreachableGuardNote is the one-line justification the emitters write beside
// the map-entry flow's second p==nil guard (maps.go emitMapRead and
// messagecontainers.go emitMessageReadMap): when order==0, p is prior, and
// order stays -1 until prior!=nil, so the guard never fires; it is kept only
// because NilAway cannot see that relational invariant.
const unreachableGuardNote = "// unreachable: order==0 needs prior!=nil; NilAway cannot see it"

// TestGeneratedGoMapEntryGuardIsJustified pins that every unreachable map-entry
// guard the Go lens keeps for NilAway carries its justification in the
// emitted output, in both the table form and the message form, and that the
// note is never written anywhere but beside that guard.
func TestGeneratedGoMapEntryGuardIsJustified(t *testing.T) {
	t.Parallel()
	u := loadUnitPaths(t, "../../..", []string{"tables/maps"})
	out, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	guard := regexp.MustCompile(`if\s*p\s*==\s*nil\s*\{\s*r\.Report\.Malformed\s*=\s*true[;\s]*(?:tableTextMalformed\([^)]*\)[;\s]*)?(?:break|return false)\s*\}[ \t]*` + regexp.QuoteMeta(unreachableGuardNote))
	notes, guarded, tableForm, messageForm := 0, 0, 0, 0
	for name, data := range out {
		n := bytes.Count(data, []byte(unreachableGuardNote))
		g := len(guard.FindAllIndex(data, -1))
		if n != g {
			t.Errorf("%s: %d justification notes but %d of them sit beside the map-entry guard", name, n, g)
		}
		notes += n
		guarded += g
		if g > 0 && bytes.Contains(data, []byte("tableMessageMapReadKey(")) {
			messageForm++
		}
		if g > 0 && bytes.Contains(data, []byte("tableMapReadKey(")) {
			tableForm++
		}
	}
	if notes == 0 || guarded == 0 {
		t.Fatalf("tables/maps emitted %d justified map-entry guards: the unreachable guard carries no justification", guarded)
	}
	if tableForm == 0 || messageForm == 0 {
		t.Errorf("justified guards in %d table-form and %d message-form files, want both forms", tableForm, messageForm)
	}
}
