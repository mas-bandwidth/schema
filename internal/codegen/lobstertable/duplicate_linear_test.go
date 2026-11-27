package lobstertable_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
)

// DUPLICATE DETECTION IS NEAR-LINEAR (LOBSTER-3; the Go reference's map, and
// review-owed/cross-leg/message-form rule 4): an id table's entries, a body's
// field ids, a keyed array's keys, a message's announced references, its
// bodies' field ids and its keyed arrays' keys are each tested against an
// IdSet, never by scanning every word seen before. The structural half reads
// the runtime for a scan of a seen list; the behavioural half holds the set
// to its contract over many words, including words that share low bits.

var duplicateScan = regexp.MustCompile(`for\((seen|seen_refs|keys|ids)\) [a-z_]+:\n\s+if [a-z_.]+ == [a-z_.]+`)

func TestLobsterDuplicateTestsUseTheSet(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"load.lobster", "message.lobster"} {
		src, err := os.ReadFile(filepath.Join(root, "internal", "codegen", "lobstertable", "runtime", name))
		if err != nil {
			t.Fatal(err)
		}
		if m := duplicateScan.FindAllString(string(src), -1); len(m) > 0 {
			t.Errorf("%s tests duplicates by scanning a seen list (%d sites): %q", name, len(m), m)
		}
	}
}

func TestLobsterIdSetHoldsItsContract(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Lobster toolchain")
	g := finiteGenerate(t, "package idset\ntable Probe { a int32 }\n")
	out := finiteRun(t, g, `
let s = schema_rt.IdSet {}
var fresh = 0
var repeat = 0
var i = 0
// words that differ only in their high bits, then ordinary ones, then the
// reserved ids at the top of the range
while i < 20000:
    if schema_rt.idset_add(s, i << 40):
        fresh++
    if schema_rt.idset_add(s, i):
        fresh++
    i++
for([-1, -2, -3]) x:
    if schema_rt.idset_add(s, x):
        fresh++
i = 0
while i < 20000:
    if not schema_rt.idset_add(s, i << 40) and not schema_rt.idset_add(s, i) and schema_rt.idset_has(s, i):
        repeat++
    i++
print("fresh " + string(fresh) + " repeat " + string(repeat) + " size " + string(s.n) + " absent " + (if schema_rt.idset_has(s, 20001): "no" else: "yes"))
`, nil)
	if strings.Join(out, "\n") != "fresh 40002 repeat 20000 size 40002 absent yes" {
		t.Fatalf("the set: %q", out)
	}
}
