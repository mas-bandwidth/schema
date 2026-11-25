package ci

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testinputs"
)

// TestTouchedLegReadsItsFamiliesAgain runs the plan block that gives every
// leg a pull request touched its reader rows of every pull-request family
// (#5005). Its jq once read .leg after piping into $legs, a string, so every
// pull request that touched a leg without selecting all of them failed the
// plan with `Cannot index string with string "leg"` (run 45544253136).
func TestTouchedLegReadsItsFamiliesAgain(t *testing.T) {
	t.Parallel()
	// the bash child reads only the workflow block, given on its command line
	testinputs.Files(t, ".github/workflows/ci-fast.yml")
	wf := readFastWorkflow(t)
	_, tail, ok := strings.Cut(wf, "          # A TOUCHED LEG READS ITS FAMILIES AGAIN")
	if !ok {
		t.Fatal("ci-fast.yml: the touched-leg block is gone")
	}
	block, _, ok := strings.Cut(tail, "          # The Dart conformance accounting")
	if !ok {
		t.Fatal("ci-fast.yml: the touched-leg block has no end marker")
	}
	var script strings.Builder
	// tools/inputfamilies answers every pull-request row: a stub, no build
	script.WriteString(`go() { echo 'input_rows=[{"leg":"rust","family":"packet-end","target":"packet-end-rust"},{"leg":"go","family":"packet-end","target":"packet-end-go"},{"leg":"rust","family":"packet-void","target":"packet-void-rust"}]'; }` + "\n")
	script.WriteString(`input_rows='[{"leg":"rust","family":"packet-end","target":"packet-end-rust"}]'` + "\n")
	script.WriteString("touched_legs=' rust '\nall=no\nRUNNER_TEMP=.\n")
	// the block starts mid-comment, after the marker
	script.WriteString("#")
	for line := range strings.SplitSeq(block, "\n") {
		script.WriteString(strings.TrimPrefix(line, "          "))
		script.WriteByte('\n')
	}
	script.WriteString(`jq -r '[.[] | .target] | sort | join(" ")' <<<"$input_rows"` + "\n")
	cmd := exec.Command("bash", "-euo", "pipefail", "-c", script.String())
	cmd.Dir = t.TempDir()
	cmd.Env = scriptEnv(os.Environ())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the touched-leg block failed: %v\n%s", err, out)
	}
	got := strings.TrimSpace(string(out))
	if want := "packet-end-rust packet-void-rust"; got != want {
		t.Fatalf("the touched leg rust must read every family once and no other leg: got %q, want %q", got, want)
	}
}
