package ci

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// THE GO-TEST JOB PROVIDES NODE AND TSC, AND SAYS SO. Its rows run the js,
// jstable and compiler tests under SCHEMA_SLOW=1, where a test that needs
// node, tsc or the serialize.js runtime and does not find it would skip, and
// a skipped test exits 0. So the job declares what it provides
// (SCHEMA_REQUIRE_NODE=1, SCHEMA_REQUIRE_TSC=1 in its env, which turn that
// skip into a failure naming the tool: internal/slowtest's Program and
// Require), and clones serialize.js beside the checkout. A lane that declares
// a tool and then lost it is red in the test; a lane that stops declaring it
// is red here.

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

// jsToolchainLaneGaps names each promise the go-test job of wf does not make.
func jsToolchainLaneGaps(wf string) []string {
	job := goTestJobBlock(wf)
	if job == "" {
		return []string{"no go-test job"}
	}
	// the job-level env: the lines under `    env:` up to the next key at
	// that indent
	env := map[string]string{}
	inEnv := false
	for raw := range strings.SplitSeq(job, "\n") {
		if raw == "    env:" {
			inEnv = true
			continue
		}
		if !inEnv {
			continue
		}
		if !strings.HasPrefix(raw, "      ") {
			if strings.TrimSpace(raw) == "" || strings.HasPrefix(strings.TrimSpace(raw), "#") {
				continue
			}
			break
		}
		k, v, ok := strings.Cut(strings.TrimSpace(raw), ":")
		if ok {
			env[k] = strings.Trim(strings.TrimSpace(v), `'"`)
		}
	}
	var gaps []string
	for _, k := range []string{"SCHEMA_REQUIRE_NODE", "SCHEMA_REQUIRE_TSC"} {
		if env[k] != "1" {
			gaps = append(gaps, "the go-test job's env does not set "+k+": '1'")
		}
	}
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

func TestTheGoTestJobProvidesNodeAndTsc(t *testing.T) {
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
// holds a lane that declares both tools and clones the runtime, and lanes that
// each drop one promise. Each must be named, and the whole lane must not.
func TestTheGoTestJobProvidesNodeAndTscNegativeControl(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("testdata", "js-toolchain-lanes")
	want := map[string][]string{
		"provides.yml": nil,
		"no-tsc.yml":   {"the go-test job's env does not set SCHEMA_REQUIRE_TSC: '1'"},
		"no-node.yml":  {"the go-test job's env does not set SCHEMA_REQUIRE_NODE: '1'"},
		"step-env.yml": {"the go-test job's env does not set SCHEMA_REQUIRE_NODE: '1'", "the go-test job's env does not set SCHEMA_REQUIRE_TSC: '1'"},
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
