package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/inputfamilies"
	"github.com/mas-bandwidth/schema/v2/internal/provesran"
)

// TestFastPlanSelectsTheLegsThatReadAPath holds ci-fast.yml's plan to the
// legs' own reads (tools/inputfamilies readers). Each path below was a silent
// gap: a change to it selected no leg row at all, and the verdict read every
// skipped row as green. The readers are derived from make's database over the
// real tree; each path must have one, and the plan, given those readers as
// the workflow computes them, must select a verify row for every one.
//
// A path whose readers are Go tests (test/conformance/textform/, the
// write-refusal-packet rows) is the go test selection's: inputfamilies select
// must name a reading package for it.
//
// Then THE SWEEP: every tracked file under test/conformance/, make/ and
// tools/ that is not Go source or prose must be selected by something, a
// reader leg, an input family or Go package, or the plan's own path rules (a
// go test package, a leg). A new file a leg reads that nothing selects is
// red here.
func TestFastPlanSelectsTheLegsThatReadAPath(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`readers_out=$(go run ./tools/inputfamilies readers -changed "$RUNNER_TEMP/diff-files.txt")`,
		`for l in ${reader_legs:-}; do add_leg "$l"; done`,
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("ci-fast.yml's plan no longer has %q", want)
		}
	}
	readers, err := inputfamilies.TreeReaders(root)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := inputfamilies.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	script := verifySelector(t, root, `printf '%s|%s|%s|%s' "$broad" "$touched_legs" "$verify_legs_json" "$packages"`)
	plan := func(t *testing.T, file string, legs []string) (broad, touched, verify, packages string) {
		t.Helper()
		cmd := exec.Command("bash", "-euo", "pipefail", "-c", script)
		cmd.Env = append(scriptEnv(os.Environ()), "files="+file, "reader_legs="+strings.Join(legs, " "))
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("plan script: %v: %s", err, out)
		}
		parts := strings.SplitN(selectorOutput(out), "|", 4)
		if len(parts) != 4 {
			t.Fatalf("plan script printed %q", out)
		}
		return parts[0], parts[1], parts[2], parts[3]
	}
	goRead := func(f string) bool {
		s := inputfamilies.Select(tree, []string{f}, false, true)
		return len(s.Rows) > 0 || len(s.Packages) > 0
	}

	for _, f := range []string{
		"test/conformance/framing/gate",
		"test/conformance/case-outcomes/check.sh",
		"test/conformance/case-outcomes/manifest.txt",
		"test/conformance/case-outcomes/continues/expected.txt",
		"test/conformance/wire-fuzz-complete",
		"test/conformance/storage-release/Storage.schema",
		"test/conformance/wire-fuzz-owed.txt",
		"test/conformance/wire-fuzz-absent.txt",
		"make/conformance-rows.mk",
		"make/conformance-continues.mk",
		"tools/mustrun/main.go",
		"tools/pgroup/main.go",
		"tools/gotestreport/main.go",
	} {
		t.Run(f, func(t *testing.T) {
			t.Parallel()
			legs := readers.Of(f)
			if len(legs) == 0 {
				t.Fatalf("no leg reads %s: a change to it selects no leg row", f)
			}
			_, touched, verify, _ := plan(t, f, legs)
			for _, l := range legs {
				if !strings.Contains(" "+touched+" ", " "+l+" ") || !strings.Contains(verify, `"`+l+`"`) {
					t.Errorf("%s is read by %s (%v) but the plan selected legs %q, verify_legs %s", f, l, readers.Why(f), touched, verify)
				}
			}
		})
	}
	for _, f := range []string{
		"test/conformance/textform/kinds.value.json",
		"test/conformance/write-refusal-packet/rows.txt",
		"test/conformance/write-refusal-packet/PacketWriteRefusal.schema",
		"test/conformance/packet/testdata/constant-defaults/hexadecimal-addition/Defaults.schema",
	} {
		t.Run(f, func(t *testing.T) {
			t.Parallel()
			if len(readers.Of(f)) == 0 && !goRead(f) {
				t.Fatalf("nothing reads %s: no leg target and no Go package", f)
			}
		})
	}

	// THE SWEEP. internal/ci runs on every pull request (ci-policy), so a file
	// its tests name (tools/vuln/testdata/, read by vuln_test.go) is read.
	var ciSrc strings.Builder
	ciGo, err := provesran.Glob(filepath.Join(root, "internal", "ci", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range ciGo {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		ciSrc.Write(b)
	}
	always := func(f string) bool {
		for d := filepath.ToSlash(filepath.Dir(f)); strings.Contains(d, "/"); d = filepath.ToSlash(filepath.Dir(d)) {
			if strings.Contains(ciSrc.String(), d) {
				return true
			}
		}
		return strings.Contains(ciSrc.String(), f)
	}
	var rest []string
	for _, f := range tree.Files {
		if !(strings.HasPrefix(f, "test/conformance/") || strings.HasPrefix(f, "make/") || strings.HasPrefix(f, "tools/")) {
			continue
		}
		if strings.HasSuffix(f, ".go") || strings.HasSuffix(f, ".md") {
			continue // Go source is the go test selection's; prose runs nowhere
		}
		if len(readers.Of(f)) > 0 || goRead(f) || always(f) {
			continue
		}
		rest = append(rest, f)
	}
	slices.Sort(rest)
	for _, f := range rest {
		t.Run("sweep "+f, func(t *testing.T) {
			t.Parallel()
			if broad, touched, verify, packages := plan(t, f, nil); broad != "yes" && strings.TrimSpace(touched) == "" && verify == "[]" && strings.TrimSpace(packages) == "" {
				t.Errorf("nothing selects %s: no leg's make target reads it, no input family or Go package names it, and no path rule of the plan matches it", f)
			}
		})
	}
}

// TestEveryPullRequestRunsTheAgentsMap holds the vet job, which runs on every
// pull request whatever it touches, to running tools/agentsmap's test. A new
// top-level directory, or a new internal/ or tools/ package, with no row in
// tools/agentsmap/catalog.go is red there; no path rule selected that test,
// so #4914, #4904 and #4919 would each have turned main red.
func TestEveryPullRequestRunsTheAgentsMap(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/workflows/ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	_, vet, ok := strings.Cut(string(data), "\n  vet:\n")
	if !ok {
		t.Fatal("ci-fast.yml has no vet job")
	}
	vet, _, _ = strings.Cut(vet, "\n\n  ")
	head, _, _ := strings.Cut(vet, "steps:")
	if strings.Contains(head, "needs.plan") || strings.Contains(head, "needs: plan") {
		t.Fatalf("the vet job waits on the plan's selection; it must run on every pull request:\n%s", head)
	}
	if !strings.Contains(vet, "run: go test -count=1 -timeout=60s ./tools/agentsmap") {
		t.Fatal("the vet job no longer runs go test ./tools/agentsmap")
	}
}
