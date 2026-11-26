package main

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// TestTheHappyPathPassesOnlyOnItsOkLine: a language's happy path passes
// when make happy-<lang> exits 0 and printed that language's ok line; an
// exit 0 with no ok line of its own (another language's, or the words inside
// another line) ran no round trip, a failing exit and a reported
// skip fail, each by name, and two rows of one language are refused. Each
// stand-in is a shell line in place of make. The plant takes any exit 0 for
// a pass.
func TestTheHappyPathPassesOnlyOnItsOkLine(t *testing.T) {
	t.Parallel()
	inst := testreg.Instance{Full: "c.functional.roundtrip.happy-path.ship-create", Lang: "c"}
	run := func(line string) testreg.Result {
		t.Helper()
		res, err := runHappyWith(context.Background(), []string{"sh", "-c", line}, "c", []testreg.Instance{inst}, io.Discard)
		if err != nil || len(res) != 1 || res[0].Instance != inst.Full {
			t.Fatalf("%q: %v %v", line, res, err)
		}
		return res[0]
	}
	if r := run("echo 'happy c: ok (26 bytes)'"); r.Verdict != testreg.Pass {
		t.Errorf("the ok line and exit 0 failed: %s", r.Reason)
	}
	for line, want := range map[string]string{
		"echo built":                                   `never printed "happy c: ok"`,
		"echo 'happy go: ok (26 bytes)'":               `never printed "happy c: ok"`,
		"echo 'unhappy c: ok'":                         `never printed "happy c: ok"`,
		"echo 'happy c: okay'":                         `never printed "happy c: ok"`,
		"echo 'happy c: ok (26 bytes)'; exit 1":        "exit status 1",
		"echo 'SKIP: no compiler'; echo 'happy c: ok'": "nothing is ever skipped",
	} {
		if r := run(line); r.Verdict != testreg.Fail || !strings.Contains(r.Reason, want) {
			t.Errorf("%q: %s %q, want a failure naming %q", line, r.Verdict, r.Reason, want)
		}
	}
	if _, err := runHappyWith(context.Background(), []string{"true"}, "c", []testreg.Instance{inst, inst}, io.Discard); err == nil || !strings.Contains(err.Error(), "one row per language") {
		t.Errorf("two rows of one language: %v", err)
	}
	if h, ok := harnessFor(testreg.Test{Family: happyFamily}); !ok || h.Name != happyFamily || len(h.Build("c")) != 1 || strings.Join(h.Build("c")[0], " ") != "make bin/schema" {
		t.Errorf("the happy-path family's harness: %v %v", h.Name, ok)
	}
}
