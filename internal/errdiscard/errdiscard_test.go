package errdiscard

import (
	"fmt"
	"strings"
	"testing"
)

// The negative control: testdata/fixture is a module whose main.go drops an
// error in every way the gate refuses (each line marked REFUSED), and in
// every way it lets through (fine). The gate must name exactly the refused
// lines, keep the two that name a reason, and never read the test file.
func TestNegativeControl(t *testing.T) {
	t.Parallel()
	findings, c, err := Check("testdata/fixture", "./...")
	if err != nil {
		t.Fatal(err)
	}
	if c.Packages != 1 || c.Files != 1 {
		t.Errorf("read %d packages and %d files, want 1 and 1 (main.go; never main_test.go)", c.Packages, c.Files)
	}
	var refused, kept []string
	for _, f := range findings {
		if f.Reason == "" {
			refused = append(refused, fmt.Sprintf("%d %s", f.Line, f.How))
		} else {
			kept = append(kept, fmt.Sprintf("%d %s", f.Line, f.Reason))
		}
	}
	want := []string{
		"14 assigned to _", "15 assigned to _", "16 not looked at", "17 assigned to _",
		"18 assigned to _", "19 dropped by go", "24 dropped by defer", "26 assigned to _",
	}
	if strings.Join(refused, "; ") != strings.Join(want, "; ") {
		t.Errorf("refused:\n got %v\nwant %v", refused, want)
	}
	wantKept := []string{"43 a reason on the line above", "44 a reason on the same line"}
	if strings.Join(kept, "; ") != strings.Join(wantKept, "; ") {
		t.Errorf("kept:\n got %v\nwant %v", kept, wantKept)
	}
	if len(Refused(findings)) != len(want) {
		t.Errorf("Refused returned %d findings, want %d", len(Refused(findings)), len(want))
	}
}

// A pattern that names no package is an error, never a clean run over nothing.
func TestNothingListedIsAnError(t *testing.T) {
	t.Parallel()
	if _, _, err := Check("testdata/fixture", "./nothing/..."); err == nil {
		t.Fatal("a pattern that matched no package passed")
	}
}
