package ci

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const (
	hostileCorpusDir = "internal/ci/hostilecorpus/corpus"
	hostileManifest  = "manifest.tsv"
	hostileProbePath = "schemas/probe.schema"

	hostileAccept   = "accept"
	hostileRefuse   = "refuse"
	hostileNoReason = "-"

	hostileDamaged            = "damaged"
	hostileNewerForm          = "newer_form"
	hostileSecondAnnouncement = "second_announcement"
	hostileVocabularyTooLarge = "vocabulary_too_large"
	hostileCountRefusal       = "count_refusal"
)

const hostileProbeSchema = "package probe\n\ntable Root\n{\n    entries map[int32]int32\n}\n"

type hostileCase struct {
	Name       string
	Carrier    string
	Schema     string
	Table      string
	Input      string
	Provenance string
	Verdict    string
	Reason     string
	Digest     string
}

type hostileReader func(t *testing.T, root, tmp string, models map[string]*tabletext.Model, c hostileCase) (verdict, reason, digest string, err error)

func TestSchemaGoRunsHostileCorpus(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	dir := filepath.Join(root, filepath.FromSlash(hostileCorpusDir))
	if _, err := os.Stat(filepath.Join(dir, hostileManifest)); err != nil {
		t.Fatalf("the shared hostile corpus is not in this checkout: %v\n"+
			"PATHS-PROPOSED: %s (manifest plus inputs/), docs/HOSTILE-CORPUS.md and tools/hostilecorpus are built by schema-shared-hostile-corpus and are not in sprint/schema-go at this head.\n"+
			"Run the corpus card first, or merge its branch, then this gate runs every input.", err, hostileCorpusDir)
	}
	_, cases, err := loadHostileManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("the hostile corpus manifest names no cases")
	}
	seen := map[string]bool{}
	for _, c := range cases {
		if seen[c.Name] {
			t.Fatalf("the hostile corpus names %q twice", c.Name)
		}
		seen[c.Name] = true
	}
	models, tmp, err := loadHostileModels(t, root, cases)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			verdict, reason, digest, err := readHostileCase(t, root, tmp, models, c)
			if err != nil {
				t.Fatal(err)
			}
			if err := compareHostile(c, verdict, reason, digest); err != nil {
				t.Fatal(err)
			}
			if hostileStaleManifest[c.Name] {
				if verdict == c.Verdict && reason == c.Reason && digest == c.Digest {
					t.Logf("%s now agrees with the manifest; hostileStaleManifest can drop it", c.Name)
				} else {
					t.Logf("FIX-CARD: schema-shared-hostile-corpus: %s manifest says %s, sprint/schema-go's reader says %s/%s (string_not_utf8, SPEC-TABLES §16.3)",
						c.Input, c.Verdict, verdict, reason)
				}
			}
		})
	}
}

func TestSchemaGoRunsHostileCorpusPlantedReader(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	dir := filepath.Join(root, filepath.FromSlash(hostileCorpusDir))
	if _, err := os.Stat(filepath.Join(dir, hostileManifest)); err != nil {
		t.Fatalf("the shared hostile corpus is not in this checkout: %v", err)
	}
	_, cases, err := loadHostileManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	models, tmp, err := loadHostileModels(t, root, cases)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyHostileCorpus(t, root, tmp, cases, models, func(t *testing.T, root, tmp string, models map[string]*tabletext.Model, c hostileCase) (string, string, string, error) {
		return readHostileCase(t, root, tmp, models, c)
	}); err != nil {
		t.Fatalf("the unplanted runner is red: %v", err)
	}
	planted := ""
	for _, c := range cases {
		if c.Verdict == hostileAccept {
			planted = c.Name
			break
		}
	}
	if planted == "" {
		t.Fatal("the corpus has no accept case to plant against")
	}
	err = verifyHostileCorpus(t, root, tmp, cases, models, func(t *testing.T, root, tmp string, models map[string]*tabletext.Model, c hostileCase) (string, string, string, error) {
		if c.Name == planted {
			return hostileRefuse, hostileDamaged, hostileNoReason, nil
		}
		return readHostileCase(t, root, tmp, models, c)
	})
	if err == nil {
		t.Fatalf("a planted reader bug on %s did not make the runner red", planted)
	}
	if !strings.Contains(err.Error(), planted) {
		t.Fatalf("the planted-bug failure does not name %s: %v", planted, err)
	}
	t.Logf("planted reader bug on %s is red: %v", planted, err)
}

// hostileStaleManifest names the four inputs whose committed verdict predates
// sprint/schema-go's "both JSON readers refuse ill-formed UTF-8 by name
// (string_not_utf8, SPEC-TABLES §16.3)" change (56a70b5fb). The corpus card
// regenerates the manifest from the engine; this card must not edit the corpus.
// The suppression is narrow: a listed case that still refuses as damaged is
// carried with a FIX-CARD; one that starts agreeing means the manifest was
// regenerated and the entry can go; any other refusal change fails the gate.
var hostileStaleManifest = map[string]bool{
	"conformance-str-illformed-at-bound":  true,
	"fuzz-text-illformed_byte_at_bound":   true,
	"fuzz-text-illformed_bytes_in_string": true,
	"fuzz-text-invalid_utf8_tail":         true,
}

func compareHostile(c hostileCase, verdict, reason, digest string) error {
	if hostileStaleManifest[c.Name] {
		if verdict == c.Verdict && reason == c.Reason && digest == c.Digest {
			return nil
		}
		if verdict == hostileRefuse && reason == hostileDamaged && digest == hostileNoReason {
			return nil
		}
		return fmt.Errorf("%s: the known stale verdict changed: got verdict=%s reason=%s digest=%s, want the suppressed refuse/damaged/-", c.Name, verdict, reason, digest)
	}
	if verdict != c.Verdict || reason != c.Reason || digest != c.Digest {
		return fmt.Errorf("%s: got verdict=%s reason=%s digest=%s, want verdict=%s reason=%s digest=%s",
			c.Name, verdict, reason, digest, c.Verdict, c.Reason, c.Digest)
	}
	return nil
}

func verifyHostileCorpus(t *testing.T, root, tmp string, cases []hostileCase, models map[string]*tabletext.Model, reader hostileReader) error {
	var diffs []string
	for _, c := range cases {
		verdict, reason, digest, err := reader(t, root, tmp, models, c)
		if err != nil {
			diffs = append(diffs, fmt.Sprintf("%s: %v", c.Name, err))
			continue
		}
		if err := compareHostile(c, verdict, reason, digest); err != nil {
			diffs = append(diffs, err.Error())
		}
	}
	if len(diffs) == 0 {
		return nil
	}
	return errors.New(strings.Join(diffs, "\n"))
}

func loadHostileManifest(dir string) (string, []hostileCase, error) {
	data, err := os.ReadFile(filepath.Join(dir, hostileManifest))
	if err != nil {
		return "", nil, err
	}
	text := string(data)
	cases, err := parseHostileManifest(text)
	if err != nil {
		return "", nil, err
	}
	return text, cases, nil
}

func parseHostileManifest(text string) ([]hostileCase, error) {
	var out []hostileCase
	for i, line := range strings.Split(text, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 9 {
			return nil, fmt.Errorf("manifest line %d: %d fields, want 9", i+1, len(f))
		}
		out = append(out, hostileCase{
			Name: f[0], Carrier: f[1], Schema: f[2], Table: f[3], Input: f[4],
			Provenance: f[5], Verdict: f[6], Reason: f[7], Digest: f[8],
		})
	}
	return out, nil
}

func loadHostileModels(t *testing.T, root string, cases []hostileCase) (map[string]*tabletext.Model, string, error) {
	t.Helper()
	tmp := t.TempDir()
	models := map[string]*tabletext.Model{}
	for _, c := range cases {
		if _, ok := models[c.Schema]; ok {
			continue
		}
		m, err := loadHostileModel(root, tmp, c.Schema)
		if err != nil {
			return nil, tmp, fmt.Errorf("%s: load schema %s: %w", c.Name, c.Schema, err)
		}
		models[c.Schema] = m
	}
	return models, tmp, nil
}

func loadHostileModel(root, tmp, schema string) (*tabletext.Model, error) {
	path := filepath.Join(root, filepath.FromSlash(schema))
	if schema == hostileProbePath {
		path = filepath.Join(tmp, "probe.schema")
		if err := os.WriteFile(path, []byte(hostileProbeSchema), 0o644); err != nil {
			return nil, err
		}
	}
	paths, err := compiler.GatherPaths([]string{path})
	if err != nil {
		return nil, err
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		return nil, err
	}
	return tabletext.NewModel(u), nil
}

func readHostileCase(t *testing.T, root, tmp string, models map[string]*tabletext.Model, c hostileCase) (verdict, reason, digest string, err error) {
	payload, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(hostileCorpusDir), "inputs", filepath.FromSlash(strings.TrimPrefix(c.Input, "inputs/"))))
	if err != nil {
		return "", "", "", fmt.Errorf("input %q: %w", c.Input, err)
	}
	m := models[c.Schema]
	if m == nil {
		return "", "", "", fmt.Errorf("schema %q was not loaded", c.Schema)
	}
	st := m.Lookup(c.Table)
	if st == nil {
		return "", "", "", fmt.Errorf("table %q is not in %s", c.Table, c.Schema)
	}
	fail := func(r string) (string, string, string, error) {
		return hostileRefuse, r, hostileNoReason, nil
	}
	accept := func(inst *tabletext.Instance) (string, string, string, error) {
		text, err := m.Write(inst)
		if err != nil {
			return "", "", "", fmt.Errorf("digest: %w", err)
		}
		sum := sha256.Sum256(text)
		return hostileAccept, hostileNoReason, hex.EncodeToString(sum[:]), nil
	}
	switch c.Carrier {
	case "wire":
		inst := m.New(st)
		var r tabletext.Report
		ok, err := tablewire.Decode(m, inst, payload, &r)
		if err != nil {
			rreason, rerr := hostileRefusalReason(err)
			if rerr != nil {
				return "", "", "", rerr
			}
			return fail(rreason)
		}
		if !ok {
			return fail(hostileDamaged)
		}
		return accept(inst)
	case "text":
		inst := m.New(st)
		var r tabletext.Report
		if !m.Read(inst, payload, &r) {
			return fail(hostileDamaged)
		}
		return accept(inst)
	case "announce":
		v := &tablewire.Vocabulary{}
		var r tabletext.Report
		if err := v.AnnounceRead(payload, &r); err != nil {
			rreason, rerr := hostileRefusalReason(err)
			if rerr != nil {
				return "", "", "", rerr
			}
			return fail(rreason)
		}
		if r.Malformed {
			return fail(hostileDamaged)
		}
		return hostileAccept, hostileNoReason, hostileNoReason, nil
	case "announce_second":
		v := &tablewire.Vocabulary{}
		var r tabletext.Report
		if err := v.AnnounceRead(payload, &r); err != nil {
			return "", "", "", fmt.Errorf("first announcement: %w", err)
		}
		err := v.AnnounceRead(payload, &r)
		if err == nil {
			return "", "", "", errors.New("second announcement was accepted")
		}
		rreason, rerr := hostileRefusalReason(err)
		if rerr != nil {
			return "", "", "", rerr
		}
		return fail(rreason)
	case "announce_bytes":
		v := &tablewire.Vocabulary{MaxBytes: 8}
		var r tabletext.Report
		if err := v.AnnounceRead(payload, &r); err == nil {
			return "", "", "", errors.New("announcement over the byte bound was accepted")
		} else {
			rreason, rerr := hostileRefusalReason(err)
			if rerr != nil {
				return "", "", "", rerr
			}
			return fail(rreason)
		}
	case "announce_entries":
		v := &tablewire.Vocabulary{MaxEntries: 1}
		var r tabletext.Report
		if err := v.AnnounceRead(payload, &r); err == nil {
			return "", "", "", errors.New("announcement over the entry bound was accepted")
		} else {
			rreason, rerr := hostileRefusalReason(err)
			if rerr != nil {
				return "", "", "", rerr
			}
			return fail(rreason)
		}
	case "message", "message_novocab":
		var v *tablewire.Vocabulary
		if c.Carrier == "message" {
			v = &tablewire.Vocabulary{}
			var ar tabletext.Report
			if err := v.AnnounceRead(tablewire.Announce(m.Unit), &ar); err != nil {
				return "", "", "", fmt.Errorf("announce: %w", err)
			}
		}
		inst := m.New(st)
		var r tabletext.Report
		n, ok, err := tablewire.DecodeMessages(m, []*tabletext.Instance{inst}, payload, v, &r)
		if err != nil {
			rreason, rerr := hostileRefusalReason(err)
			if rerr != nil {
				return "", "", "", rerr
			}
			return fail(rreason)
		}
		if !ok || n != 1 {
			return fail(hostileDamaged)
		}
		return accept(inst)
	default:
		return "", "", "", fmt.Errorf("unknown carrier %q", c.Carrier)
	}
}

func hostileRefusalReason(err error) (string, error) {
	var mr *tablewire.MessageRefusal
	if errors.As(err, &mr) {
		return mr.Reason.String(), nil
	}
	var fr *tablewire.FormRefusal
	if errors.As(err, &fr) {
		return hostileNewerForm, nil
	}
	var cr *tablewire.CountRefusal
	if errors.As(err, &cr) {
		return hostileCountRefusal, nil
	}
	return "", fmt.Errorf("engine returned an unnamed refusal: %w", err)
}

func TestSchemaGoRunsHostileCorpusWorkflowIsFleetOnly(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", "schema-go-runs-hostile-corpus.yml"))
	if err != nil {
		t.Fatalf("read workflow: %v", err)
	}
	text := string(data)
	for _, want := range []string{
		"runs-on: [self-hosted, Linux, X64, fleet, schema]",
		"github.event_name != 'pull_request' || github.event.pull_request.head.repo.full_name == github.repository",
		"make schema-go-runs-hostile-corpus",
		"make schema-go-runs-hostile-corpus-negative-control",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("workflow missing %q", want)
		}
	}
	for _, forbidden := range []string{"ubuntu-latest"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("workflow still carries %q", forbidden)
		}
	}
}
