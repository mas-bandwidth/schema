package ci

// The SHARED HOSTILE-INPUT CORPUS (schema-shared-hostile-corpus).
//
// One corpus of adversarial inputs for schema, shared by every port. Each
// input's expected verdict — accept with the digest of the decoded value, or
// refuse with the engine's named reason — is taken from the GO ENGINE
// (internal/tablewire, internal/tabletext), never from a generated reader.
//
// THE CORPUS IS A PORTABLE DIRECTORY: internal/ci/hostilecorpus/corpus holds
// one raw file per input under inputs/ and manifest.tsv beside it. The format
// needs no JSON dependency (a line reader plus the port's own decoder). The
// engine logic, generator, drift check and source inventory live in
// internal/ci/hostilecorpus; tools/hostilecorpus is the Go-port runner and the
// worked example every per-port card follows. See docs/HOSTILE-CORPUS.md.
//
// TestSchemaSharedHostileCorpus is the drift gate: it rebuilds every verdict
// from the engine and fails on any difference from the committed manifest.
// TestSchemaSharedHostileCorpusNegativeControl plants a changed verdict and
// requires that same gate to go red, so the gate is proved to have a blade.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/ci/hostilecorpus"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func hostileCorpusDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "internal/ci/hostilecorpus/corpus")
}

// TestHostileRefusalReasonFailsClosed pins the engine-to-reason mapping and
// proves an error that is none of the engine's refusal types fails closed
// (a non-nil error) rather than collapsing into a synthetic engine_error.
func TestHostileRefusalReasonFailsClosed(t *testing.T) {
	t.Parallel()
	msg := &tablewire.MessageRefusal{Reason: tablewire.ReasonVocabularyTooLarge}
	if r, err := hostilecorpus.RefusalReason(msg); err != nil || r != hostilecorpus.ReasonVocabularyTooLarge {
		t.Errorf("MessageRefusal: got (%q, %v), want (vocabulary_too_large, nil)", r, err)
	}
	fr := &tablewire.FormRefusal{Form: 2}
	if r, err := hostilecorpus.RefusalReason(fr); err != nil || r != hostilecorpus.ReasonNewerForm {
		t.Errorf("FormRefusal: got (%q, %v), want (newer_form, nil)", r, err)
	}
	cr := &tablewire.CountRefusal{}
	if r, err := hostilecorpus.RefusalReason(cr); err != nil || r != hostilecorpus.ReasonCountRefusal {
		t.Errorf("CountRefusal: got (%q, %v), want (count_refusal, nil)", r, err)
	}
	if _, err := hostilecorpus.RefusalReason(errors.New("not a refusal")); err == nil {
		t.Error("an unnamed refusal error did not fail closed; it collapsed into a synthetic reason")
	}
}

// TestSchemaSharedHostileCorpus is the drift gate.
func TestSchemaSharedHostileCorpus(t *testing.T) {
	t.Parallel()
	manifest, _, err := hostilecorpus.LoadManifest(hostileCorpusDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := hostilecorpus.Verify(repoRoot(t), t.TempDir(), hostileCorpusDir(t), manifest); err != nil {
		t.Fatal(err)
	}
}

// TestSchemaSharedHostileCorpusReasons proves the corpus names every refusal
// reason the engine can produce and no reason it cannot, and that every accept
// carries a digest and every refuse names its reason.
func TestSchemaSharedHostileCorpusReasons(t *testing.T) {
	t.Parallel()
	_, cases, err := hostilecorpus.LoadManifest(hostileCorpusDir(t))
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

// TestSchemaSharedHostileCorpusNegativeControl plants a changed verdict in the
// committed manifest (in memory), requires the same drift gate to go red, and
// requires the unplanted manifest to be green. A plant that stops being caught
// is the gate going blind.
func TestSchemaSharedHostileCorpusNegativeControl(t *testing.T) {
	t.Parallel()
	root, tmp := repoRoot(t), t.TempDir()
	dir := hostileCorpusDir(t)
	manifest, _, err := hostilecorpus.LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := hostilecorpus.Verify(root, tmp, dir, manifest); err != nil {
		t.Fatalf("unplanted corpus must be green: %v", err)
	}
	lines := strings.Split(manifest, "\n")
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
	if err := hostilecorpus.Verify(root, tmp, dir, strings.Join(lines, "\n")); err == nil {
		t.Fatal("planted bad verdict was NOT caught: the drift gate has no blade")
	}
}

// TestSchemaSharedHostileCorpusSources holds the source claim two ways. Each
// cited source must still exist in the checkout as a regular file, so
// provenance cannot rot into a claim the repository does not hold; and every
// leaf file in the source trees must be cited, so a new fuzz crash or
// conformance vector cannot silently fall out of the all-seed/vector corpus the
// card claims to carry. The source trees are walked recursively: the
// conformance json-hostile vectors live one file per case subdirectory.
func TestSchemaSharedHostileCorpusSources(t *testing.T) {
	t.Parallel()
	_, cases, err := hostilecorpus.LoadManifest(hostileCorpusDir(t))
	if err != nil {
		t.Fatal(err)
	}
	root := repoRoot(t)
	for _, c := range cases {
		for _, prefix := range hostilecorpus.SourceDirs {
			if i := strings.Index(c.Provenance, prefix); i >= 0 {
				rest := strings.Fields(c.Provenance[i+len(prefix):])[0]
				p := filepath.Join(root, filepath.FromSlash(prefix+rest))
				fi, err := os.Stat(p)
				if err != nil {
					t.Errorf("%s: source %s does not exist: %v", c.Name, p, err)
				} else if !fi.Mode().IsRegular() {
					t.Errorf("%s: source %s is not a regular file", c.Name, p)
				}
			}
		}
	}
	for _, u := range hostilecorpus.Uncovered(root, hostilecorpus.SourceDirs, cases) {
		t.Errorf("source %s is cited by no corpus case: the all-seed/vector claim is not protected", u)
	}
}

// TestSchemaSharedHostileCorpusExcludedSources accounts for the tracked fuzz
// seeds that are not table-wire inputs. They must exist, and they must not be
// cited: the exclusion is a compiler-seed decision recorded in
// hostilecorpus.ExcludedSourceDirs and docs/HOSTILE-CORPUS.md, not a silent
// gap. A seed that moves into the corpus fails here until the exclusion list
// says so.
func TestSchemaSharedHostileCorpusExcludedSources(t *testing.T) {
	t.Parallel()
	_, cases, err := hostilecorpus.LoadManifest(hostileCorpusDir(t))
	if err != nil {
		t.Fatal(err)
	}
	root := repoRoot(t)
	files, err := hostilecorpus.ExcludedFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no excluded compiler fuzz seeds found: the documented exclusion is stale")
	}
	for _, f := range files {
		for _, c := range cases {
			if strings.Contains(c.Provenance, f) {
				t.Errorf("%s is cited by %s but listed as an excluded compiler seed", f, c.Name)
			}
		}
	}
}

// TestSchemaSharedHostileCorpusSourcesNegativeControl plants an uncited
// json-hostile/<case>/<root>.json (a new conformance-vector directory) in a
// scratch tree and requires the completeness walk to report it. A check that
// cannot see a planted, uncited vector is no check.
func TestSchemaSharedHostileCorpusSourcesNegativeControl(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	planted := "json-hostile/new-case/Root.json"
	p := filepath.Join(tmp, filepath.FromSlash(planted))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, u := range hostilecorpus.Uncovered(tmp, []string{"json-hostile/"}, nil) {
		if u == planted {
			found = true
		}
	}
	if !found {
		t.Fatalf("planted uncited vector %s was NOT uncovered: the source check has no blade", planted)
	}
}

// TestSchemaSharedHostileCorpusGenerate is the generator: it rebuilds the
// manifest from the engine. Without HOSTILE_CORPUS_UPDATE it is the same drift
// check; with it, it rewrites the committed manifest.
func TestSchemaSharedHostileCorpusGenerate(t *testing.T) {
	t.Parallel()
	root, tmp, dir := repoRoot(t), t.TempDir(), hostileCorpusDir(t)
	manifest, meta, err := hostilecorpus.LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := hostilecorpus.Generate(root, tmp, dir, meta)
	if err != nil {
		t.Fatal(err)
	}
	want := hostilecorpus.Format(got)
	if os.Getenv("HOSTILE_CORPUS_UPDATE") == "1" {
		if err := hostilecorpus.WriteManifest(dir, want); err != nil {
			t.Fatal(err)
		}
		t.Logf("rewrote the committed hostile corpus manifest from the engine")
		return
	}
	if want != manifest {
		for _, d := range hostilecorpus.Diff(manifest, want) {
			t.Error(d)
		}
		t.Fatal("the committed manifest is stale; run make schema-shared-hostile-corpus-generate")
	}
}
