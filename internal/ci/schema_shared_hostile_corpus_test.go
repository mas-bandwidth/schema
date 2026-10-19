package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/ci/hostilecorpus"
)

// The shared hostile-input corpus (schema-shared-hostile-corpus).
//
// The corpus is a directory of RAW inputs plus one line-oriented manifest under
// internal/ci/hostilecorpus, so every port reads it without a JSON dependency:
//
//	name\tcarrier\tschema\ttable\tinput\tprovenance\tverdict\treason\tdigest
//
// Every expected verdict is the GO ENGINE's (internal/tablewire,
// internal/tabletext), and tools/hostilecorpus rebuilds the manifest from that
// engine. TestSchemaSharedHostileCorpus is the drift gate; the planted-bad
// control proves the gate has a blade.

func TestSchemaSharedHostileCorpus(t *testing.T) {
	t.Parallel()
	if err := hostilecorpus.Verify(repoRoot(t)); err != nil {
		t.Fatal(err)
	}
}

// TestSchemaSharedHostileCorpusReasons proves the corpus names every refusal
// reason the engine can produce, and no reason it cannot. The expected set is
// read off internal/tablewire's own reason constants (plus the file reader's
// damaged and builder CountRefusal verdicts).
func TestSchemaSharedHostileCorpusReasons(t *testing.T) {
	t.Parallel()
	cases, err := hostilecorpus.Load(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, c := range cases {
		if c.Verdict == hostilecorpus.Accept {
			if c.Reason != hostilecorpus.NoReason {
				t.Errorf("%s: accept carries reason %q", c.Name, c.Reason)
			}
			if c.Digest == hostilecorpus.NoReason {
				t.Errorf("%s: accept carries no decoded-value digest", c.Name)
			}
			continue
		}
		if c.Verdict != hostilecorpus.Refuse {
			t.Errorf("%s: verdict %q is neither accept nor refuse", c.Name, c.Verdict)
		}
		if c.Reason == hostilecorpus.NoReason {
			t.Errorf("%s: refuse carries no reason", c.Name)
		}
		if c.Digest != hostilecorpus.NoReason {
			t.Errorf("%s: refuse carries a decoded-value digest", c.Name)
		}
		have[c.Reason] = true
	}
	want := map[string]bool{}
	for _, r := range hostilecorpus.EngineReasons() {
		want[r] = true
		if !have[r] {
			t.Errorf("engine reason %q has no input in the corpus (STEP 2: prove none is missing)", r)
		}
	}
	for r := range have {
		if !want[r] {
			t.Errorf("corpus names reason %q, which is not an engine reason", r)
		}
	}
}

// TestSchemaSharedHostileCorpusNegativeControl plants a bad verdict in a copy
// of the COMMITTED corpus and requires the drift gate to go red; without the
// plant the same gate is green. A plant that stops being caught is the gate
// going blind.
func TestSchemaSharedHostileCorpusNegativeControl(t *testing.T) {
	t.Parallel()
	if err := hostilecorpus.Verify(repoRoot(t)); err != nil {
		t.Fatalf("unplanted corpus must be green: %v", err)
	}
	tmp := t.TempDir()
	src := hostilecorpus.Dir(repoRoot(t))
	dst := hostilecorpus.Dir(tmp)
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dst, hostilecorpus.ManifestName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	planted := false
	for i, line := range lines {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if f[6] == hostilecorpus.Accept {
			f[6], f[7], f[8] = hostilecorpus.Refuse, hostilecorpus.ReasonDamaged, hostilecorpus.NoReason
		} else {
			f[6], f[7], f[8] = hostilecorpus.Accept, hostilecorpus.NoReason, strings.Repeat("0", 64)
		}
		lines[i] = strings.Join(f, "\t")
		planted = true
		break
	}
	if !planted {
		t.Fatal("no manifest row to plant")
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := hostilecorpus.Verify(tmp); err == nil {
		t.Fatal("planted bad verdict was NOT caught: the drift gate has no blade")
	}
}

// TestSchemaSharedHostileCorpusSources requires every gathered input to name a
// source that still exists in the checkout, so provenance cannot rot into a
// claim the repository does not hold.
func TestSchemaSharedHostileCorpusSources(t *testing.T) {
	t.Parallel()
	cases, err := hostilecorpus.Load(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	root := repoRoot(t)
	for _, c := range cases {
		for _, prefix := range []string{
			"internal/tabletext/testdata/fuzz/FuzzReadText/",
			"testdata/conformance/tables/json-hostile/",
		} {
			if i := strings.Index(c.Provenance, prefix); i >= 0 {
				rest := c.Provenance[i+len(prefix):]
				rest = strings.Fields(rest)[0]
				p := filepath.Join(root, filepath.FromSlash(prefix+rest))
				if _, err := os.Stat(p); err != nil {
					t.Errorf("%s: source %s does not exist: %v", c.Name, p, err)
				}
			}
		}
	}
}
