package docgate

import (
	"strings"
	"testing"
)

// TestRuntimePinsMatchTheMakefile is the standing gate: every runtime version
// a page tells its reader to clone or require is the Makefile's pin for that
// runtime. A red names the page, the line, and the pin it should be.
func TestRuntimePinsMatchTheMakefile(t *testing.T) {
	root, err := FindRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	pins, err := RuntimePins(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, repo := range []string{"serialize", "serialize.c", "serialize.go"} {
		if pins[repo] == "" {
			t.Errorf("the Makefile's pins %v have none for %s, which the tutorial names", pins, repo)
		}
	}
	problems, err := CheckRuntimePins(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Errorf("%s", p)
	}
}

// TestRuntimePinsGoRed is the gate's own control: each form a page names a
// runtime version in has to surface a drift by name, and the forms that name
// the pin, or name no version, have to pass.
func TestRuntimePinsGoRed(t *testing.T) {
	pins := map[string]string{"serialize": "v2.1.3", "serialize.c": "v2.1.1", "serialize.go": "v2.1.1"}
	page := strings.Join([]string{
		"$ git clone --branch v2.1.3 https://github.com/mas-bandwidth/serialize.git",   // 1 pinned
		"$ git clone --branch v2.0.2 https://github.com/mas-bandwidth/serialize.c.git", // 2 drifted
		"git clone https://github.com/mas-bandwidth/serialize.go.git",                  // 3 no version
		"\tgithub.com/mas-bandwidth/serialize.go/v2 v2.0.0",                            // 4 drifted
		"go: downloading github.com/mas-bandwidth/serialize.go/v2 v2.1.1",              // 5 pinned
		"$ go get github.com/mas-bandwidth/serialize.go/v2@v2.1.0",                     // 6 drifted
		"$ git clone -b v9.9.9 https://github.com/mas-bandwidth/serialize.nut.git",     // 7 unpinned runtime
		"replace github.com/mas-bandwidth/serialize.go/v2 => ../serialize.go",          // 8 no version
		"serialize.c v1.9.2 at `ddea231` is what the run used",                         // 9 a record
	}, "\n")
	problems := pagePinProblems("docs/X.md", page, pins)
	if len(problems) == 0 {
		t.Fatal("a page with drifted runtime versions surfaced no problem")
	}
	got := map[int]string{}
	for _, p := range problems {
		got[p.Line] = p.Ref
	}
	want := map[int]string{
		2: "serialize.c v2.0.2",
		4: "serialize.go v2.0.0",
		6: "serialize.go v2.1.0",
		7: "serialize.nut v9.9.9",
	}
	for line, ref := range want {
		if got[line] != ref {
			t.Errorf("line %d: want a problem naming %q, got %q", line, ref, got[line])
		}
	}
	for line, ref := range got {
		if _, ok := want[line]; !ok {
			t.Errorf("line %d: a problem naming %q, and that line names the pin or no pin", line, ref)
		}
	}
}
