package hostilecorpus

import (
	"path/filepath"
	"testing"
)

const wideSchemaPath = "internal/ci/hostilecorpus/wide/wide.schema"

func wideCorpusDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(moduleRoot(t), "internal", "ci", "hostilecorpus", "wide")
}

// TestWideBoundsCorpusVerify holds the committed wide corpus to the engine:
// every verdict and digest in its manifest is the Go engine's own.
func TestWideBoundsCorpusVerify(t *testing.T) {
	t.Parallel()
	dir := wideCorpusDir(t)
	manifest, _, err := LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(moduleRoot(t), t.TempDir(), dir, manifest); err != nil {
		t.Fatal(err)
	}
}

// TestWideBoundsCorpusCovers requires the wide manifest to carry, for each
// rule, a refuse case and its accept twin on every carrier and root the set
// names.
func TestWideBoundsCorpusCovers(t *testing.T) {
	t.Parallel()
	_, cases, err := LoadManifest(wideCorpusDir(t))
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Case{}
	for _, c := range cases {
		byName[c.Name] = c
	}
	groups := []struct {
		rule    string
		carrier string
		table   string
		refuse  []string
		twin    []string
	}{
		{
			rule:    "bits ceiling, wire variable",
			carrier: "wire",
			table:   "Variable",
			refuse: []string{
				"wide-wire-variable-b48-bit63",
				"wide-wire-variable-b48-past48",
				"wide-wire-variable-b63-bit63",
			},
			twin: []string{
				"wide-wire-variable-b48-max",
				"wide-wire-variable-b63-max",
			},
		},
		{
			rule:    "bits ceiling, wire fixed",
			carrier: "wire",
			table:   "Fixed",
			refuse: []string{
				"wide-wire-fixed-b48-bit63",
				"wide-wire-fixed-b48-past48",
				"wide-wire-fixed-b63-bit63",
			},
			twin: []string{
				"wide-wire-fixed-b48-max",
				"wide-wire-fixed-b63-max",
			},
		},
		{
			rule:    "bits ceiling, text variable",
			carrier: "text",
			table:   "Variable",
			refuse: []string{
				"wide-text-variable-b63-bit63",
			},
			twin: []string{
				"wide-text-variable-b63-max",
			},
		},
		{
			rule:    "64-bit text bounds",
			carrier: "text",
			table:   "Variable",
			refuse: []string{
				"wide-text-variable-u64-bound-past",
				"wide-text-variable-i64-bound-past",
				"wide-text-variable-u64-top-past",
				"wide-text-variable-u64-top-past-rounded",
			},
			twin: []string{
				"wide-text-variable-u64-bound-max",
				"wide-text-variable-i64-bound-min",
				"wide-text-variable-u64-top-max",
			},
		},
	}
	if len(groups) != 4 {
		t.Fatalf("the cover table names %d groups, want 4", len(groups))
	}
	for _, g := range groups {
		if len(g.refuse) == 0 {
			t.Errorf("%s: no refuse case", g.rule)
		}
		if len(g.twin) == 0 {
			t.Errorf("%s: no accept twin", g.rule)
		}
		for _, name := range g.refuse {
			checkWideCase(t, byName, name, g.carrier, g.table, Refuse)
		}
		for _, name := range g.twin {
			checkWideCase(t, byName, name, g.carrier, g.table, Accept)
		}
	}
}

func checkWideCase(t *testing.T, byName map[string]Case, name, carrier, table, verdict string) {
	t.Helper()
	c, ok := byName[name]
	if !ok {
		t.Errorf("wide manifest is missing %s", name)
		return
	}
	if c.Carrier != carrier {
		t.Errorf("%s: carrier %q, want %q", name, c.Carrier, carrier)
	}
	if c.Schema != wideSchemaPath {
		t.Errorf("%s: schema %q, want %q", name, c.Schema, wideSchemaPath)
	}
	if c.Table != table {
		t.Errorf("%s: table %q, want %q", name, c.Table, table)
	}
	if c.Verdict != verdict {
		t.Errorf("%s: verdict %q, want %q", name, c.Verdict, verdict)
	}
	if verdict == Accept {
		if c.Reason != NoReason {
			t.Errorf("%s: accept carries reason %q", name, c.Reason)
		}
		if c.Digest == NoReason {
			t.Errorf("%s: accept carries no decoded-value digest", name)
		}
		return
	}
	if c.Reason == NoReason {
		t.Errorf("%s: refuse carries no reason", name)
	}
	if c.Digest != NoReason {
		t.Errorf("%s: refuse carries a decoded-value digest", name)
	}
}
