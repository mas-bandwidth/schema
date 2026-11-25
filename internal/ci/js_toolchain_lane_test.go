package ci

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// THE GO-TEST JOB CLONES SERIALIZE.JS. Its rows run the js, jstable and
// compiler tests, and a test that needs node, tsc or the serialize.js runtime
// and does not find it fails naming it (internal/slowtest's Tool and
// Program): no test skips. So the job clones serialize.js beside the
// checkout, and a job that stops cloning it is red here before its rows are.

// goTestJobBlock is the go-test job of a workflow, from its key to the next
// job key.
func goTestJobBlock(wf string) string {
	var b strings.Builder
	in := false
	for raw := range strings.SplitSeq(wf, "\n") {
		if !in {
			in = raw == "  go-test:"
			continue
		}
		trimmed := strings.TrimSpace(raw)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") && (!strings.HasPrefix(raw, "  ") || jobKeyRe.MatchString(raw)) {
			break
		}
		b.WriteString(raw)
		b.WriteByte('\n')
	}
	return b.String()
}

// jsToolchainLaneGaps names what the go-test job of wf does not do.
func jsToolchainLaneGaps(wf string) []string {
	job := goTestJobBlock(wf)
	if job == "" {
		return []string{"no go-test job"}
	}
	var gaps []string
	cloned := false
	for raw := range strings.SplitSeq(job, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "git clone") && strings.Contains(line, "serialize.js.git") && strings.HasSuffix(line, " serialize.js") {
			cloned = true
		}
	}
	if !cloned {
		gaps = append(gaps, "the go-test job does not clone serialize.js beside the checkout")
	}
	return gaps
}

func TestTheGoTestJobClonesSerializeJS(t *testing.T) {
	t.Parallel()
	wfs := workflows(t, repoRoot(t))
	wf, ok := wfs["ci-full.yml"]
	if !ok {
		t.Fatal("no ci-full.yml")
	}
	if gaps := jsToolchainLaneGaps(wf); len(gaps) > 0 {
		t.Errorf("ci-full.yml: %s", strings.Join(gaps, "; "))
	}
}

// The negative control, from committed lanes: testdata/js-toolchain-lanes
// holds a lane that clones the runtime and one that does not. The second must
// be named, and the whole lane must not.
func TestTheGoTestJobClonesSerializeJSNegativeControl(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("testdata", "js-toolchain-lanes")
	want := map[string][]string{
		"provides.yml": nil,
		"no-clone.yml": {"the go-test job does not clone serialize.js beside the checkout"},
	}
	for name, gaps := range want {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if got := jsToolchainLaneGaps(string(data)); !slices.Equal(got, gaps) {
			t.Errorf("%s: got %q, want %q", name, got, gaps)
		}
	}
}
