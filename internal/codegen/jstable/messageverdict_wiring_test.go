package jstable

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestWireFuzzMessageAdaptersAnswerThePublicVerdict (F-112): the JavaScript
// conformance driver's message wire-fuzz adapters (test/conformance/js/
// main.mjs, forms 2 and 5) answer the verdict <Root>LoadMessages gave, through
// messageverdict.mjs, and no counter turns a delivered batch into a refusal.
// They read `(rep.Mismatch | 0) + (rep.Oor | 0) > 0` as refused, so a public
// load of count 1 answered loaded 0, refused 1. The verdict itself is replayed
// over its committed cases by `make js-message-verdict-test`; this test holds
// the driver to it without a toolchain.
func TestWireFuzzMessageAdaptersAnswerThePublicVerdict(t *testing.T) {
	t.Parallel()
	src := driverSource(t)
	if !strings.Contains(src, `import { messageLoadVerdict } from "./messageverdict.mjs";`) {
		t.Error("test/conformance/js/main.mjs does not import messageLoadVerdict from ./messageverdict.mjs")
	}
	lines := strings.Split(src, "\n")
	adapters := 0
	for i, line := range lines {
		if !strings.Contains(line, "const n = loadMessages(") {
			continue
		}
		adapters++
		took := false
		for _, next := range lines[i+1 : min(i+10, len(lines))] {
			if strings.Contains(next, "messageLoadVerdict(n, rep, runtime.TableMessageOpenRefused)") {
				took = true
				break
			}
		}
		if !took {
			t.Errorf("main.mjs:%d: a wire-fuzz message adapter does not answer messageLoadVerdict(n, rep, ...) after its load", i+1)
		}
	}
	if adapters != 2 {
		t.Errorf("main.mjs carries %d wire-fuzz message adapter load(s); the forms 2 and 5 adapters are 2", adapters)
	}
	counter := regexp.MustCompile(`\b(refused|malformed|delivered)\b\s*=.*\brep\.(Mismatch|Oor|Widened|Unknown)\b`)
	for i, line := range lines {
		if counter.MatchString(line) {
			t.Errorf("main.mjs:%d: a verdict reads a counter: %s", i+1, strings.TrimSpace(line))
		}
	}
	// the two recipes that run a copy of the driver copy its verdict module too
	mk, err := os.ReadFile(filepath.Join("..", "..", "..", "make", "js.mk"))
	if err != nil {
		t.Fatal(err)
	}
	copies := strings.Count(string(mk), "test/conformance/js/main.mjs > build/$@/main.mjs") + strings.Count(string(mk), "cp test/conformance/js/main.mjs build/$@/main.mjs")
	if verdicts := strings.Count(string(mk), "cp test/conformance/js/messageverdict.mjs build/$@/messageverdict.mjs"); copies == 0 || verdicts != copies {
		t.Errorf("make/js.mk copies the driver %d time(s) and messageverdict.mjs %d time(s): a copy without it cannot load", copies, verdicts)
	}
}
