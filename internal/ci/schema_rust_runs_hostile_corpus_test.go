package ci

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type rustHostileFinding struct {
	slug    string
	input   string
	wantGot string
	summary string
}

var rustHostileKnownFindings = map[string]rustHostileFinding{
	"conformance-fixed-array-elements": {
		slug:    "rust-hostile-conformance-fixed-array-elements",
		input:   "inputs/conformance-fixed-array-elements.json",
		wantGot: "got unavailable no_text_reader_for_fixed_table -",
		summary: "rusttable emits no text reader for a pure fixed table root (SimState)",
	},
	"conformance-fixed-at-bound": {
		slug:    "rust-hostile-conformance-fixed-at-bound",
		input:   "inputs/conformance-fixed-at-bound.json",
		wantGot: "got unavailable no_text_reader_for_fixed_table -",
		summary: "rusttable emits no text reader for a pure fixed table root (SimState)",
	},
	"conformance-fixed-below-resolution": {
		slug:    "rust-hostile-conformance-fixed-below-resolution",
		input:   "inputs/conformance-fixed-below-resolution.json",
		wantGot: "got unavailable no_text_reader_for_fixed_table -",
		summary: "rusttable emits no text reader for a pure fixed table root (SimState)",
	},
	"conformance-fixed-exact-fraction": {
		slug:    "rust-hostile-conformance-fixed-exact-fraction",
		input:   "inputs/conformance-fixed-exact-fraction.json",
		wantGot: "got unavailable no_text_reader_for_fixed_table -",
		summary: "rusttable emits no text reader for a pure fixed table root (SimState)",
	},
	"conformance-fixed-fraction-too-fine": {
		slug:    "rust-hostile-conformance-fixed-fraction-too-fine",
		input:   "inputs/conformance-fixed-fraction-too-fine.json",
		wantGot: "got unavailable no_text_reader_for_fixed_table -",
		summary: "rusttable emits no text reader for a pure fixed table root (SimState)",
	},
	"conformance-fixed-huge-exponent": {
		slug:    "rust-hostile-conformance-fixed-huge-exponent",
		input:   "inputs/conformance-fixed-huge-exponent.json",
		wantGot: "got unavailable no_text_reader_for_fixed_table -",
		summary: "rusttable emits no text reader for a pure fixed table root (SimState)",
	},
	"conformance-fixed-integral-token": {
		slug:    "rust-hostile-conformance-fixed-integral-token",
		input:   "inputs/conformance-fixed-integral-token.json",
		wantGot: "got unavailable no_text_reader_for_fixed_table -",
		summary: "rusttable emits no text reader for a pure fixed table root (SimState)",
	},
	"conformance-fixed-negative-zero": {
		slug:    "rust-hostile-conformance-fixed-negative-zero",
		input:   "inputs/conformance-fixed-negative-zero.json",
		wantGot: "got unavailable no_text_reader_for_fixed_table -",
		summary: "rusttable emits no text reader for a pure fixed table root (SimState)",
	},
	"conformance-fixed-nested-type": {
		slug:    "rust-hostile-conformance-fixed-nested-type",
		input:   "inputs/conformance-fixed-nested-type.json",
		wantGot: "got unavailable no_text_reader_for_fixed_table -",
		summary: "rusttable emits no text reader for a pure fixed table root (SimState)",
	},
	"conformance-fixed-over-range": {
		slug:    "rust-hostile-conformance-fixed-over-range",
		input:   "inputs/conformance-fixed-over-range.json",
		wantGot: "got unavailable no_text_reader_for_fixed_table -",
		summary: "rusttable emits no text reader for a pure fixed table root (SimState)",
	},
	"conformance-fixed-wrong-shape": {
		slug:    "rust-hostile-conformance-fixed-wrong-shape",
		input:   "inputs/conformance-fixed-wrong-shape.json",
		wantGot: "got unavailable no_text_reader_for_fixed_table -",
		summary: "rusttable emits no text reader for a pure fixed table root (SimState)",
	},
	"conformance-int128-beyond-int64": {
		slug:    "rust-hostile-conformance-int128-beyond-int64",
		input:   "inputs/conformance-int128-beyond-int64.json",
		wantGot: "got unavailable no_text_reader_for_fixed_table -",
		summary: "rusttable emits no text reader for a pure fixed table root (SimState)",
	},
	"conformance-int128-fractional": {
		slug:    "rust-hostile-conformance-int128-fractional",
		input:   "inputs/conformance-int128-fractional.json",
		wantGot: "got unavailable no_text_reader_for_fixed_table -",
		summary: "rusttable emits no text reader for a pure fixed table root (SimState)",
	},
	"conformance-int128-integral-forms": {
		slug:    "rust-hostile-conformance-int128-integral-forms",
		input:   "inputs/conformance-int128-integral-forms.json",
		wantGot: "got unavailable no_text_reader_for_fixed_table -",
		summary: "rusttable emits no text reader for a pure fixed table root (SimState)",
	},
	"conformance-int128-over-range": {
		slug:    "rust-hostile-conformance-int128-over-range",
		input:   "inputs/conformance-int128-over-range.json",
		wantGot: "got unavailable no_text_reader_for_fixed_table -",
		summary: "rusttable emits no text reader for a pure fixed table root (SimState)",
	},
	"conformance-int128-saturated-below": {
		slug:    "rust-hostile-conformance-int128-saturated-below",
		input:   "inputs/conformance-int128-saturated-below.json",
		wantGot: "got unavailable no_text_reader_for_fixed_table -",
		summary: "rusttable emits no text reader for a pure fixed table root (SimState)",
	},
	"conformance-ufixed-negative": {
		slug:    "rust-hostile-conformance-ufixed-negative",
		input:   "inputs/conformance-ufixed-negative.json",
		wantGot: "got unavailable no_text_reader_for_fixed_table -",
		summary: "rusttable emits no text reader for a pure fixed table root (SimState)",
	},
	"conformance-uint128-max": {
		slug:    "rust-hostile-conformance-uint128-max",
		input:   "inputs/conformance-uint128-max.json",
		wantGot: "got unavailable no_text_reader_for_fixed_table -",
		summary: "rusttable emits no text reader for a pure fixed table root (SimState)",
	},
	"conformance-uint128-negative": {
		slug:    "rust-hostile-conformance-uint128-negative",
		input:   "inputs/conformance-uint128-negative.json",
		wantGot: "got unavailable no_text_reader_for_fixed_table -",
		summary: "rusttable emits no text reader for a pure fixed table root (SimState)",
	},
	"conformance-uint128-over": {
		slug:    "rust-hostile-conformance-uint128-over",
		input:   "inputs/conformance-uint128-over.json",
		wantGot: "got unavailable no_text_reader_for_fixed_table -",
		summary: "rusttable emits no text reader for a pure fixed table root (SimState)",
	},
	"message-valid": {
		slug:    "rust-hostile-message-valid",
		input:   "inputs/message-valid.bin",
		wantGot: "got unavailable no_text_conversion_for_message_row -",
		summary: "rusttable emits no bridge from the message-form Row to the canonical text writer",
	},
	"wire-count-refusal": {
		slug:    "rust-hostile-wire-count-refusal",
		input:   "inputs/wire-count-refusal.bin",
		wantGot: "got refuse damaged -",
		summary: "the generated Rust variable reader refuses an oversized collection count without naming count_refusal",
	},
}

func TestSchemaRustRunsHostileCorpus(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	corpus := filepath.Join(root, "internal", "ci", "hostilecorpus", "corpus")
	runner := os.Getenv("SCHEMA_RUST_HOSTILE_RUNNER")
	if runner == "" {
		runner = filepath.Join(root, "build", "rust-hostile", "target", "debug", "rust-hostile")
	}
	if _, err := os.Stat(runner); err != nil {
		t.Fatalf("the Rust hostile-corpus runner is not built at %s: %v (run make schema-rust-runs-hostile-corpus)", runner, err)
	}
	unexplained, stale, passed, carried := checkRustHostileCorpus(t, runner, corpus, "")
	for _, f := range carried {
		t.Logf("FIX-CARD: %s: %s %s", f.slug, f.input, f.summary)
	}
	t.Logf("rust-hostile: %d cases: %d pass, %d carried as FIX-CARD, %d unexplained, %d stale",
		passed+len(carried)+len(unexplained), passed, len(carried), len(unexplained), len(stale))
	for _, s := range stale {
		t.Errorf("STALE FIX-CARD: %s", s)
	}
	for _, u := range unexplained {
		t.Errorf("UNEXPLAINED: %s", u)
	}
	if len(unexplained) != 0 || len(stale) != 0 {
		t.Fatalf("%d unexplained and %d stale findings on the committed corpus", len(unexplained), len(stale))
	}
}

func TestSchemaRustRunsHostileCorpusPlanted(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	corpus := filepath.Join(root, "internal", "ci", "hostilecorpus", "corpus")
	runner := os.Getenv("SCHEMA_RUST_HOSTILE_RUNNER")
	if runner == "" {
		runner = filepath.Join(root, "build", "rust-hostile", "target", "debug", "rust-hostile")
	}
	if _, err := os.Stat(runner); err != nil {
		t.Fatalf("the Rust hostile-corpus runner is not built at %s: %v (run make schema-rust-runs-hostile-corpus)", runner, err)
	}

	const plantedName = "wire-valid"

	unexplained, _, _, _ := checkRustHostileCorpus(t, runner, corpus, plantedName)
	var seen bool
	for _, m := range unexplained {
		if strings.HasPrefix(m, plantedName+" ") {
			seen = true
			t.Logf("planted reader bug: %s", m)
		}
	}
	if !seen {
		t.Fatalf("a reader bug on %s left the runner green", plantedName)
	}

	unexplainedOut, staleOut, _, _ := checkRustHostileCorpus(t, runner, corpus, "")
	if len(unexplainedOut) != 0 || len(staleOut) != 0 {
		t.Fatalf("unplanted run had %d unexplained and %d stale findings", len(unexplainedOut), len(staleOut))
	}
}

type hostileCase struct {
	name, carrier, schema, table, input, verdict, reason, digest string
}

func loadHostileManifest(t *testing.T, path string) []hostileCase {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []hostileCase
	for i, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 9 {
			t.Fatalf("manifest line %d: %d fields, want 9", i+1, len(f))
		}
		out = append(out, hostileCase{
			name: f[0], carrier: f[1], schema: f[2], table: f[3], input: f[4],
			verdict: f[6], reason: f[7], digest: f[8],
		})
	}
	return out
}

func checkRustHostileCorpus(t *testing.T, runner, corpus, plant string) (unexplained, stale []string, passed int, carried []rustHostileFinding) {
	t.Helper()
	out := t.TempDir()
	cmd := exec.Command(runner, corpus, out)
	if plant != "" {
		cmd.Env = append(os.Environ(), "SCHEMA_RUST_HOSTILE_PLANT="+plant)
	}
	cmd.Stdout = io.Discard
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("the Rust runner failed: %v\n%s", err, stderr.String())
	}
	manifest := loadHostileManifest(t, filepath.Join(corpus, "manifest.tsv"))
	seenKnown := map[string]bool{}
	for _, c := range manifest {
		body, err := os.ReadFile(filepath.Join(out, c.name+".verdict"))
		if err != nil {
			unexplained = append(unexplained, fmt.Sprintf("%s: no verdict (%v)", c.name, err))
			continue
		}
		parts := strings.SplitN(string(body), "\n", 3)
		if len(parts) < 2 {
			unexplained = append(unexplained, fmt.Sprintf("%s: malformed verdict %q", c.name, string(body)))
			continue
		}
		gotVerdict, gotReason := parts[0], parts[1]
		gotDigest := "-"
		if gotVerdict == "accept" {
			sum := sha256.Sum256([]byte(parts[2]))
			gotDigest = hex.EncodeToString(sum[:])
		}
		gotSummary := fmt.Sprintf("got %s %s %s", gotVerdict, gotReason, gotDigest)
		mismatch := gotVerdict != c.verdict || gotReason != c.reason || gotDigest != c.digest
		if known, ok := rustHostileKnownFindings[c.name]; ok {
			seenKnown[c.name] = true
			if known.input != c.input {
				stale = append(stale, fmt.Sprintf("%s (%s): input %q != manifest %q", c.name, known.slug, known.input, c.input))
				continue
			}
			if !mismatch {
				stale = append(stale, fmt.Sprintf("%s (%s): case now passes; remove from rustHostileKnownFindings", c.name, known.slug))
				continue
			}
			if gotSummary != known.wantGot {
				unexplained = append(unexplained, fmt.Sprintf("%s [%s/%s %s]: FIX-CARD %s expected %q, %s",
					c.name, c.carrier, c.schema, c.table, known.slug, known.wantGot, gotSummary))
				continue
			}
			carried = append(carried, known)
			continue
		}
		if mismatch {
			unexplained = append(unexplained, fmt.Sprintf("%s [%s/%s %s]: want %s %s %s, %s",
				c.name, c.carrier, c.schema, c.table, c.verdict, c.reason, c.digest, gotSummary))
			continue
		}
		passed++
	}
	for name, known := range rustHostileKnownFindings {
		if !seenKnown[name] {
			stale = append(stale, fmt.Sprintf("%s (%s): not in manifest.tsv", name, known.slug))
		}
	}
	return unexplained, stale, passed, carried
}
